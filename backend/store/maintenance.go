package store

import (
	"context"
	"database/sql"
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
// rows mirroring runs and tasks. Runs without an event time are kept — they
// cannot be ordered against the cutoff.
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

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit transaction")
	}
	return deleted, nil
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
		// upsertOpenLineageTask rebuilds the aggregate from the remaining runs.
		return s.upsertOpenLineageTask(ctx, tx, taskGUID)
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
