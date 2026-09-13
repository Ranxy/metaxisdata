package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// OpenLineageTaskMessage is the aggregated task/job-level view derived from persisted runs.
type OpenLineageTaskMessage struct {
	ID                 int64
	GUID               string
	JobNamespace       string
	JobName            string
	JobType            string
	Integration        string
	ProcessingType     string
	ParentJobNamespace string
	ParentJobName      string
	RootJobNamespace   string
	RootJobName        string
	LatestRunGUID      string
	LatestRunID        string
	LatestRawPayload   []byte
	LatestEventTime    *time.Time
	LatestEventType    string
	LatestProducer     string
	LatestSource       string
	RunCount           int32
	LineageRunCount    int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FindOpenLineageTaskMessage is the query filter for aggregated OpenLineage tasks/jobs.
type FindOpenLineageTaskMessage struct {
	GUID         *string
	JobNamespace *string
	JobName      *string
	JobType      *string
	LineageOnly  *bool
	Limit        *int
	Offset       *int
}

// openLineageTaskState is a task's stored aggregate, read while its row is
// locked for one ingested run.
type openLineageTaskState struct {
	ID                 int64
	GUID               string
	JobNamespace       string
	JobName            string
	JobType            string
	Integration        string
	ProcessingType     string
	ParentJobNamespace string
	ParentJobName      string
	RootJobNamespace   string
	RootJobName        string
	LatestRunGUID      string
	LatestRunID        string
	LatestEventTime    *time.Time
	LatestProducer     string
	LatestSource       string
	RunCount           int32
	LineageRunCount    int32
}

// lockOpenLineageTask takes the task's row lock and returns its stored aggregate.
// The lock is what makes the incremental counters exact: it serializes the
// writers of one task, so two deliveries of the same run cannot both count it as
// new. A task without runs is created here and filled in by applyOpenLineageRun.
func lockOpenLineageTask(ctx context.Context, tx *sql.Tx, run *OpenLineageRunMessage) (*openLineageTaskState, error) {
	var state openLineageTaskState
	var latestEventTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openlineage_task (guid, job_namespace, job_name, job_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (job_namespace, job_name, job_type) DO UPDATE SET
			updated_at = openlineage_task.updated_at
		RETURNING
			id,
			guid,
			job_namespace,
			job_name,
			job_type,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			root_job_namespace,
			root_job_name,
			latest_run_guid,
			latest_run_id,
			latest_event_time,
			latest_producer,
			latest_source,
			run_count,
			lineage_run_count
	`, run.TaskGUID, run.JobNamespace, run.JobName, run.JobType).Scan(
		&state.ID,
		&state.GUID,
		&state.JobNamespace,
		&state.JobName,
		&state.JobType,
		&state.Integration,
		&state.ProcessingType,
		&state.ParentJobNamespace,
		&state.ParentJobName,
		&state.RootJobNamespace,
		&state.RootJobName,
		&state.LatestRunGUID,
		&state.LatestRunID,
		&latestEventTime,
		&state.LatestProducer,
		&state.LatestSource,
		&state.RunCount,
		&state.LineageRunCount,
	); err != nil {
		return nil, errors.Wrap(err, "failed to lock openlineage task")
	}
	if latestEventTime.Valid {
		t := latestEventTime.Time
		state.LatestEventTime = &t
	}
	return &state, nil
}

// taskCountDelta reports how one ingested run changes its task's counters. A
// redelivered run counts once, and its lineage flag only moves when it changed.
func taskCountDelta(existed, previousHasLineage, hasLineage bool) (runs, lineage int32) {
	if !existed {
		runs = 1
	}
	if hasLineage {
		lineage++
	}
	if existed && previousHasLineage {
		lineage--
	}
	return runs, lineage
}

// runIsLatest reports whether a run replaces the task's stored latest run. A run
// without an event time only wins while the task has no timed run, mirroring the
// "event_time DESC NULLS LAST" ordering the aggregate used to apply.
func runIsLatest(storedLatestEventTime, runEventTime *time.Time) bool {
	if storedLatestEventTime == nil {
		return true
	}
	if runEventTime == nil {
		return false
	}
	return !runEventTime.Before(*storedLatestEventTime)
}

// applyOpenLineageRun folds one persisted run into the task aggregate. The
// counters move by the delta rather than being recomputed from every run of the
// task, which is what made ingestion quadratic in the task's history.
func (*Store) applyOpenLineageRun(ctx context.Context, tx *sql.Tx, task *openLineageTaskState, run *OpenLineageRunMessage, existed, previousHasLineage bool) (*OpenLineageTaskMessage, error) {
	deltaRuns, deltaLineage := taskCountDelta(existed, previousHasLineage, run.HasLineage)
	task.RunCount += deltaRuns
	task.LineageRunCount += deltaLineage
	if runIsLatest(task.LatestEventTime, run.EventTime) {
		task.Integration = run.Integration
		task.ProcessingType = run.ProcessingType
		task.ParentJobNamespace = run.ParentJobNamespace
		task.ParentJobName = run.ParentJobName
		task.RootJobNamespace = run.RootJobNamespace
		task.RootJobName = run.RootJobName
		task.LatestRunGUID = run.GUID
		task.LatestRunID = run.RunID
		task.LatestEventTime = run.EventTime
		task.LatestProducer = run.Producer
		task.LatestSource = run.Source
	}

	var latestEventTime any
	if task.LatestEventTime != nil {
		latestEventTime = *task.LatestEventTime
	}

	var updated OpenLineageTaskMessage
	var updatedEventTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		UPDATE openlineage_task SET
			integration = $1,
			processing_type = $2,
			parent_job_namespace = $3,
			parent_job_name = $4,
			root_job_namespace = $5,
			root_job_name = $6,
			latest_run_guid = $7,
			latest_run_id = $8,
			latest_event_time = $9,
			latest_producer = $10,
			latest_source = $11,
			run_count = $12,
			lineage_run_count = $13,
			updated_at = NOW()
		WHERE id = $14
		RETURNING
			id,
			guid,
			job_namespace,
			job_name,
			job_type,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			root_job_namespace,
			root_job_name,
			latest_run_guid,
			latest_run_id,
			latest_event_time,
			latest_producer,
			latest_source,
			run_count,
			lineage_run_count,
			created_at,
			updated_at
	`, task.Integration, task.ProcessingType, task.ParentJobNamespace, task.ParentJobName,
		task.RootJobNamespace, task.RootJobName, task.LatestRunGUID, task.LatestRunID,
		latestEventTime, task.LatestProducer, task.LatestSource, task.RunCount,
		task.LineageRunCount, task.ID).Scan(
		&updated.ID,
		&updated.GUID,
		&updated.JobNamespace,
		&updated.JobName,
		&updated.JobType,
		&updated.Integration,
		&updated.ProcessingType,
		&updated.ParentJobNamespace,
		&updated.ParentJobName,
		&updated.RootJobNamespace,
		&updated.RootJobName,
		&updated.LatestRunGUID,
		&updated.LatestRunID,
		&updatedEventTime,
		&updated.LatestProducer,
		&updated.LatestSource,
		&updated.RunCount,
		&updated.LineageRunCount,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to update openlineage task")
	}
	if updatedEventTime.Valid {
		t := updatedEventTime.Time
		updated.LatestEventTime = &t
	}

	return &updated, nil
}

// rebuildOpenLineageTask recomputes a task's aggregate from its remaining runs.
// Ingestion maintains the counters incrementally; retention cleanup, which can
// remove a whole batch of runs at once, rebuilds the aggregate instead.
func (s *Store) rebuildOpenLineageTask(ctx context.Context, tx *sql.Tx, taskGUID string) error {
	agg, err := buildOpenLineageTaskAggregate(ctx, tx, taskGUID)
	if err != nil {
		return err
	}

	var latestEventTime any
	if agg.LatestEventTime != nil {
		latestEventTime = *agg.LatestEventTime
	}

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openlineage_task (
			guid,
			job_namespace,
			job_name,
			job_type,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			root_job_namespace,
			root_job_name,
			latest_run_guid,
			latest_run_id,
			latest_event_time,
			latest_producer,
			latest_source,
			run_count,
			lineage_run_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (job_namespace, job_name, job_type) DO UPDATE SET
			guid = EXCLUDED.guid,
			integration = EXCLUDED.integration,
			processing_type = EXCLUDED.processing_type,
			parent_job_namespace = EXCLUDED.parent_job_namespace,
			parent_job_name = EXCLUDED.parent_job_name,
			root_job_namespace = EXCLUDED.root_job_namespace,
			root_job_name = EXCLUDED.root_job_name,
			latest_run_guid = EXCLUDED.latest_run_guid,
			latest_run_id = EXCLUDED.latest_run_id,
			latest_event_time = EXCLUDED.latest_event_time,
			latest_producer = EXCLUDED.latest_producer,
			latest_source = EXCLUDED.latest_source,
			run_count = EXCLUDED.run_count,
			lineage_run_count = EXCLUDED.lineage_run_count,
			updated_at = NOW()
		RETURNING id, created_at, updated_at
	`, agg.GUID, agg.JobNamespace, agg.JobName, agg.JobType, agg.Integration, agg.ProcessingType, agg.ParentJobNamespace, agg.ParentJobName, agg.RootJobNamespace, agg.RootJobName, agg.LatestRunGUID, agg.LatestRunID, latestEventTime, agg.LatestProducer, agg.LatestSource, agg.RunCount, agg.LineageRunCount).Scan(&agg.ID, &agg.CreatedAt, &agg.UpdatedAt); err != nil {
		return errors.Wrap(err, "failed to upsert openlineage task")
	}

	return s.upsertOpenLineageTaskMetaRegistry(ctx, tx, agg)
}

func buildOpenLineageTaskAggregate(ctx context.Context, tx *sql.Tx, taskGUID string) (*OpenLineageTaskMessage, error) {
	var agg OpenLineageTaskMessage
	var latestEventTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT
			task_guid,
			job_namespace,
			job_name,
			job_type,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			root_job_namespace,
			root_job_name,
			guid,
			run_id,
			event_time,
			producer,
			source,
			event_type,
			COUNT(*) OVER () AS run_count,
			COUNT(*) FILTER (WHERE has_lineage) OVER () AS lineage_run_count
		FROM openlineage_run
		WHERE task_guid = $1
		ORDER BY event_time DESC NULLS LAST, updated_at DESC, id DESC
		LIMIT 1
	`, taskGUID).Scan(
		&agg.GUID,
		&agg.JobNamespace,
		&agg.JobName,
		&agg.JobType,
		&agg.Integration,
		&agg.ProcessingType,
		&agg.ParentJobNamespace,
		&agg.ParentJobName,
		&agg.RootJobNamespace,
		&agg.RootJobName,
		&agg.LatestRunGUID,
		&agg.LatestRunID,
		&latestEventTime,
		&agg.LatestProducer,
		&agg.LatestSource,
		&agg.LatestEventType,
		&agg.RunCount,
		&agg.LineageRunCount,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("openlineage task %q has no runs", taskGUID)
		}
		return nil, errors.Wrap(err, "failed to build openlineage task aggregate")
	}
	if latestEventTime.Valid {
		t := latestEventTime.Time
		agg.LatestEventTime = &t
	}
	return &agg, nil
}

func (s *Store) upsertOpenLineageTaskMetaRegistry(ctx context.Context, tx *sql.Tx, task *OpenLineageTaskMessage) error {
	storedMetadata := buildOpenLineageTaskStoredMetadata(task)
	metadataBytes, metaHash, err := CalcStoreMetaHash(storedMetadata)
	if err != nil {
		return errors.Wrap(err, "failed to calculate openlineage task metadata hash")
	}

	_, err = s.BatchCreateMetaRegistryResource(ctx, tx, []*CreateMetaRegistryResourceMessage{{
		MetaRegistryResource: MetaRegistryResource{
			GUID:       task.GUID,
			ObjectType: storepb.MetaType_OPENLINEAGE,
			Metadata:   storedMetadata,
			MetaHash:   metaHash,
		},
		MetadataBytes: metadataBytes,
	}})
	if err != nil {
		return errors.Wrap(err, "failed to mirror openlineage task into meta registry")
	}

	return nil
}

// GetOpenLineageTask returns one aggregated task or nil if none exists.
func (s *Store) GetOpenLineageTask(ctx context.Context, find *FindOpenLineageTaskMessage) (*OpenLineageTaskMessage, error) {
	list, err := s.ListOpenLineageTask(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Conflict, "found %d OpenLineage tasks with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// ListOpenLineageTask lists aggregated OpenLineage tasks/jobs matching the filter.
func (s *Store) ListOpenLineageTask(ctx context.Context, find *FindOpenLineageTaskMessage) ([]*OpenLineageTaskMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.GUID; v != nil {
		where, args = append(where, fmt.Sprintf("task.guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobNamespace; v != nil {
		where, args = append(where, fmt.Sprintf("task.job_namespace = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobName; v != nil {
		where, args = append(where, fmt.Sprintf("task.job_name = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobType; v != nil {
		where, args = append(where, fmt.Sprintf("task.job_type = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.LineageOnly; v != nil && *v {
		where = append(where, "task.lineage_run_count > 0")
	}

	query := `
		SELECT
			task.id,
			task.guid,
			task.job_namespace,
			task.job_name,
			task.job_type,
			task.integration,
			task.processing_type,
			task.parent_job_namespace,
			task.parent_job_name,
			task.root_job_namespace,
			task.root_job_name,
			task.latest_run_guid,
			task.latest_run_id,
			latest_run.raw_payload,
			task.latest_event_time,
			task.latest_producer,
			task.latest_source,
			task.run_count,
			task.lineage_run_count,
			task.created_at,
			task.updated_at,
			latest_run.event_type
		FROM openlineage_task task
		LEFT JOIN openlineage_run AS latest_run ON latest_run.guid = task.latest_run_guid
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY task.latest_event_time DESC NULLS LAST, task.id DESC`
	pageClause, pageArgs := openLineagePageClause(find.Limit, find.Offset, len(args))
	query += pageClause
	args = append(args, pageArgs...)

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query openlineage tasks")
	}
	defer rows.Close()

	var result []*OpenLineageTaskMessage
	for rows.Next() {
		var msg OpenLineageTaskMessage
		var latestEventTime sql.NullTime
		var latestRawPayload []byte
		if err := rows.Scan(
			&msg.ID,
			&msg.GUID,
			&msg.JobNamespace,
			&msg.JobName,
			&msg.JobType,
			&msg.Integration,
			&msg.ProcessingType,
			&msg.ParentJobNamespace,
			&msg.ParentJobName,
			&msg.RootJobNamespace,
			&msg.RootJobName,
			&msg.LatestRunGUID,
			&msg.LatestRunID,
			&latestRawPayload,
			&latestEventTime,
			&msg.LatestProducer,
			&msg.LatestSource,
			&msg.RunCount,
			&msg.LineageRunCount,
			&msg.CreatedAt,
			&msg.UpdatedAt,
			&msg.LatestEventType,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan openlineage task")
		}
		if latestEventTime.Valid {
			t := latestEventTime.Time
			msg.LatestEventTime = &t
		}
		msg.LatestRawPayload = latestRawPayload
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

func buildOpenLineageTaskStoredMetadata(task *OpenLineageTaskMessage) *storepb.StoredMetadata {
	summary := &storepb.OpenLineageTaskSummary{
		Guid:               task.GUID,
		JobNamespace:       task.JobNamespace,
		JobName:            task.JobName,
		JobType:            task.JobType,
		Integration:        task.Integration,
		ProcessingType:     task.ProcessingType,
		ParentJobNamespace: task.ParentJobNamespace,
		ParentJobName:      task.ParentJobName,
		RootJobNamespace:   task.RootJobNamespace,
		RootJobName:        task.RootJobName,
		LatestRunGuid:      task.LatestRunGUID,
		LatestRunId:        task.LatestRunID,
		LatestProducer:     task.LatestProducer,
		LatestSource:       task.LatestSource,
		LatestEventType:    task.LatestEventType,
		RunCount:           task.RunCount,
		LineageRunCount:    task.LineageRunCount,
		CreatedAt:          timestamppb.New(task.CreatedAt),
		UpdatedAt:          timestamppb.New(task.UpdatedAt),
	}
	if task.LatestEventTime != nil {
		summary.LatestEventTime = timestamppb.New(*task.LatestEventTime)
	}

	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_OpenlineageTaskSummary{
			OpenlineageTaskSummary: summary,
		},
	}
}
