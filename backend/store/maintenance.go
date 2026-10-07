package store

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// DeleteExpiredExplainSQLCache removes cached explanations created before
// olderThan. Expired rows are already invisible (GetExplainSQLCache enforces
// ExplainSQLCacheTTL), so this only reclaims space.
func (s *Store) DeleteExpiredExplainSQLCache(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `DELETE FROM explain_sql_cache WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired explain SQL cache rows")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count deleted explain SQL cache rows")
	}
	return deleted, nil
}

// DeleteExpiredLLMDebugLog removes debug log entries created before olderThan.
// The table stores full request/response bodies, so it is pruned rather than
// kept forever.
func (s *Store) DeleteExpiredLLMDebugLog(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `DELETE FROM llm_debug_log WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired LLM debug log rows")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count deleted LLM debug log rows")
	}
	return deleted, nil
}

// DeleteOpenLineageRunsBefore removes persisted runs whose event time is older
// than cutoff, together with what was derived from them: the aggregated task
// rows (recomputed, or removed once they have no run left) and the meta registry
// rows mirroring runs and tasks. The dataset aggregates the pruned references
// belonged to are recomputed the same way. Runs without an event time are kept —
// they cannot be ordered against the cutoff.
func (s *Store) DeleteOpenLineageRunsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	runGUIDs, taskGUIDs, err := collectExpiredOpenLineageRuns(ctx, tx, cutoff)
	if err != nil {
		return 0, err
	}
	if len(runGUIDs) == 0 {
		return 0, nil
	}
	// The pruned tasks are reconciled first and in task-GUID order, which is the
	// order the ingest path takes its task locks in: a prune and an ingest that
	// share two tasks cannot deadlock on them.
	slices.Sort(taskGUIDs)
	// The dataset aggregates to reconcile are the ones the deleted references
	// belonged to. They are staged in the database rather than read into the
	// server: the prune can remove millions of references, and only the distinct
	// datasets they named are needed.
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE expired_openlineage_dataset (
			namespace TEXT NOT NULL,
			name TEXT NOT NULL
		) ON COMMIT DROP
	`); err != nil {
		return 0, errors.Wrap(err, "failed to stage the pruned openlineage datasets")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO expired_openlineage_dataset (namespace, name)
		SELECT DISTINCT d.namespace, d.name
		FROM openlineage_run_dataset d
		JOIN openlineage_run r ON r.id = d.run_pk
		WHERE r.event_time IS NOT NULL AND r.event_time < $1
	`, cutoff); err != nil {
		return 0, errors.Wrap(err, "failed to stage the pruned openlineage datasets")
	}

	result, err := tx.ExecContext(ctx, `
		DELETE FROM openlineage_run
		WHERE event_time IS NOT NULL AND event_time < $1
	`, cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired openlineage runs")
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count deleted openlineage runs")
	}

	if err := deleteOpenLineageRegistryRows(ctx, s, tx, runGUIDs); err != nil {
		return 0, err
	}
	for _, taskGUID := range taskGUIDs {
		if err := reconcileOpenLineageTask(ctx, s, tx, taskGUID); err != nil {
			return 0, err
		}
	}
	// Datasets after tasks, the other half of the same order the ingest path
	// follows.
	if err := reconcileOpenLineageDatasets(ctx, tx); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit transaction")
	}
	return deleted, nil
}

// reconcileOpenLineageDatasets regenerates the aggregates of the datasets the
// prune touched: the ones whose last reference went away are removed, and the
// ones that remain have their counters, newest event time and member rows
// recomputed from the references still stored. Ingestion maintains the aggregate
// incrementally; a bulk delete, which cannot report which references it took,
// rebuilds it instead.
//
// The affected rows are locked before anything about them is read, in the same
// (namespace, name) order the ingest path locks them in and under the same
// hierarchy (tasks before datasets). Recomputing from a snapshot taken after those
// locks is what keeps the rebuild from overwriting an ingest that committed while
// it was waiting: a writer that arrives later waits on the same lock and applies its
// own deltas on top of the rebuilt value.
func reconcileOpenLineageDatasets(ctx context.Context, tx *sql.Tx) error {
	// A dataset the rebuild covers may have no aggregate row yet; it needs one
	// before the lock below can cover it.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO openlineage_dataset (namespace, name)
		SELECT namespace, name FROM expired_openlineage_dataset
		ON CONFLICT (namespace, name) DO NOTHING`); err != nil {
		return errors.Wrap(err, "failed to prepare the openlineage dataset aggregates")
	}
	if _, err := tx.ExecContext(ctx, `
		SELECT ds.namespace, ds.name
		FROM openlineage_dataset ds
		JOIN expired_openlineage_dataset expired ON expired.namespace = ds.namespace AND expired.name = ds.name
		ORDER BY ds.namespace COLLATE "C", ds.name COLLATE "C"
		FOR UPDATE OF ds`); err != nil {
		return errors.Wrap(err, "failed to lock the openlineage dataset aggregates")
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM openlineage_dataset ds
		USING expired_openlineage_dataset expired
		WHERE ds.namespace = expired.namespace AND ds.name = expired.name
			AND NOT EXISTS (
				SELECT 1 FROM openlineage_run_dataset d
				WHERE d.namespace = expired.namespace AND d.name = expired.name
			)`); err != nil {
		return errors.Wrap(err, "failed to delete the empty openlineage datasets")
	}

	// The aggregate is upserted rather than updated so a dataset that has
	// references but no aggregate row yet is rebuilt instead of left out.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO openlineage_dataset (
			namespace, name, last_seen, ref_count, source_job_count, target_job_count, column_lineage_ref_count
		)
		SELECT
			d.namespace,
			d.name,
			MAX(d.event_time),
			COUNT(*),
			COUNT(DISTINCT d.task_guid) FILTER (WHERE d.direction = '`+OpenLineageDatasetDirectionInput+`'),
			COUNT(DISTINCT d.task_guid) FILTER (WHERE d.direction = '`+OpenLineageDatasetDirectionOutput+`'),
			COUNT(*) FILTER (WHERE d.has_column_lineage)
		FROM openlineage_run_dataset d
		JOIN expired_openlineage_dataset expired ON expired.namespace = d.namespace AND expired.name = d.name
		GROUP BY 1, 2
		ON CONFLICT (namespace, name) DO UPDATE SET
			last_seen = EXCLUDED.last_seen,
			ref_count = EXCLUDED.ref_count,
			source_job_count = EXCLUDED.source_job_count,
			target_job_count = EXCLUDED.target_job_count,
			column_lineage_ref_count = EXCLUDED.column_lineage_ref_count,
			updated_at = NOW()`); err != nil {
		return errors.Wrap(err, "failed to rebuild the openlineage dataset aggregates")
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM openlineage_dataset_member member
		USING expired_openlineage_dataset expired
		WHERE member.namespace = expired.namespace AND member.name = expired.name`); err != nil {
		return errors.Wrap(err, "failed to clear the rebuilt openlineage dataset members")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO openlineage_dataset_member (namespace, name, kind, value, ref_count)
		SELECT namespace, name, kind, value, COUNT(*)
		FROM (
			SELECT d.namespace, d.name, '`+openLineageDatasetMemberInputTask+`' AS kind, d.task_guid AS value
			FROM openlineage_run_dataset d
			JOIN expired_openlineage_dataset expired ON expired.namespace = d.namespace AND expired.name = d.name
			WHERE d.direction = '`+OpenLineageDatasetDirectionInput+`'
			UNION ALL
			SELECT d.namespace, d.name, '`+openLineageDatasetMemberOutputTask+`', d.task_guid
			FROM openlineage_run_dataset d
			JOIN expired_openlineage_dataset expired ON expired.namespace = d.namespace AND expired.name = d.name
			WHERE d.direction = '`+OpenLineageDatasetDirectionOutput+`'
			UNION ALL
			SELECT d.namespace, d.name, '`+openLineageDatasetMemberIntegration+`', d.integration
			FROM openlineage_run_dataset d
			JOIN expired_openlineage_dataset expired ON expired.namespace = d.namespace AND expired.name = d.name
			WHERE d.integration <> ''
			UNION ALL
			SELECT d.namespace, d.name, '`+openLineageDatasetMemberSource+`', d.source
			FROM openlineage_run_dataset d
			JOIN expired_openlineage_dataset expired ON expired.namespace = d.namespace AND expired.name = d.name
			WHERE d.source <> ''
		) members
		GROUP BY 1, 2, 3, 4`); err != nil {
		return errors.Wrap(err, "failed to rebuild the openlineage dataset members")
	}
	return nil
}

// collectExpiredOpenLineageRuns returns the GUIDs of the runs to delete and the
// distinct task GUIDs they belong to.
func collectExpiredOpenLineageRuns(ctx context.Context, tx *sql.Tx, cutoff time.Time) ([]string, []string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT guid, task_guid
		FROM openlineage_run
		WHERE event_time IS NOT NULL AND event_time < $1
	`, cutoff)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to list expired openlineage runs")
	}
	defer rows.Close()

	var runGUIDs []string
	taskSet := map[string]struct{}{}
	for rows.Next() {
		var guid, taskGUID string
		if err := rows.Scan(&guid, &taskGUID); err != nil {
			return nil, nil, errors.Wrap(err, "failed to scan expired openlineage run")
		}
		runGUIDs = append(runGUIDs, guid)
		taskSet[taskGUID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, errors.Wrap(err, "failed to iterate expired openlineage runs")
	}

	taskGUIDs := make([]string, 0, len(taskSet))
	for taskGUID := range taskSet {
		taskGUIDs = append(taskGUIDs, taskGUID)
	}
	return runGUIDs, taskGUIDs, nil
}

// reconcileOpenLineageTask regenerates the aggregate for a task whose runs
// changed, or removes the task and its registry row when no run is left.
func reconcileOpenLineageTask(ctx context.Context, s *Store, tx *sql.Tx, taskGUID string) error {
	var remaining int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM openlineage_run WHERE task_guid = $1`, taskGUID,
	).Scan(&remaining); err != nil {
		return errors.Wrap(err, "failed to count remaining openlineage runs")
	}

	if remaining > 0 {
		// rebuildOpenLineageTask recomputes the aggregate from the remaining runs.
		return s.rebuildOpenLineageTask(ctx, tx, taskGUID)
	}

	var taskGUIDValue string
	if err := tx.QueryRowContext(ctx,
		`DELETE FROM openlineage_task WHERE guid = $1 RETURNING guid`, taskGUID,
	).Scan(&taskGUIDValue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The task row is already gone.
			return nil
		}
		return errors.Wrap(err, "failed to delete empty openlineage task")
	}
	return deleteOpenLineageRegistryRows(ctx, s, tx, []string{taskGUIDValue})
}

// deleteOpenLineageRegistryRows removes the meta registry rows mirroring the
// given OpenLineage GUIDs (runs and tasks both use MetaType_OPENLINEAGE).
func deleteOpenLineageRegistryRows(ctx context.Context, s *Store, tx *sql.Tx, guids []string) error {
	if len(guids) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, guid, object_type
		FROM meta_registry_resource
		WHERE object_type = $1 AND guid = ANY($2)
	`, storepb.MetaType_OPENLINEAGE, pq.Array(guids))
	if err != nil {
		return errors.Wrap(err, "failed to list openlineage meta registry rows")
	}
	defer rows.Close()

	var list []*MetaRegistryResource
	for rows.Next() {
		var res MetaRegistryResource
		if err := rows.Scan(&res.ID, &res.GUID, &res.ObjectType); err != nil {
			return errors.Wrap(err, "failed to scan openlineage meta registry row")
		}
		list = append(list, &res)
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to iterate openlineage meta registry rows")
	}

	return s.BatchDeleteMetaRegistry(ctx, tx, list)
}
