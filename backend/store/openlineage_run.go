package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// defaultOpenLineageListLimit bounds a list query that does not ask for a size of
// its own. An unbounded list would let one request read the whole table.
const defaultOpenLineageListLimit = 5000

// openLineagePageClause renders the LIMIT/OFFSET clause of an OpenLineage list
// query and returns the arguments to bind after startIndex. A caller that does
// not ask for a size gets the default cap, and a negative value is clamped
// because PostgreSQL rejects a negative LIMIT.
func openLineagePageClause(limit, offset *int, startIndex int) (string, []any) {
	effective := defaultOpenLineageListLimit
	if limit != nil {
		effective = *limit
	}
	pageArgs := []any{max(effective, 0)}
	clause := fmt.Sprintf(" LIMIT $%d", startIndex+1)
	if offset != nil {
		pageArgs = append(pageArgs, max(*offset, 0))
		clause += fmt.Sprintf(" OFFSET $%d", startIndex+len(pageArgs))
	}
	return clause, pageArgs
}

// openLineageEventComplete is the OpenLineage run state whose row is terminal. A
// finished run carries the payload its lineage was derived from, so a later - or
// redelivered - START or FAIL must not rewrite that row: the finish is the last
// word on what the run did.
const openLineageEventComplete = "COMPLETE"

// OpenLineageRunMessage is the store representation of a persisted OpenLineage
// run. One row holds one run's latest known state, so it is COMPLETE for a run
// that finished and START or FAIL for one that is still going or never got
// there.
type OpenLineageRunMessage struct {
	ID                 int64
	GUID               string
	TaskGUID           string
	RunID              string
	JobNamespace       string
	JobName            string
	JobType            string
	EventType          string
	EventTime          *time.Time
	Producer           string
	Integration        string
	ProcessingType     string
	ParentJobNamespace string
	ParentJobName      string
	ParentRunID        string
	RootJobNamespace   string
	RootJobName        string
	RootRunID          string
	Source             string
	InputCount         int32
	OutputCount        int32
	HasLineage         bool
	RawPayload         []byte
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// FindOpenLineageRunMessage is the query filter for persisted OpenLineage runs.
type FindOpenLineageRunMessage struct {
	GUID         *string
	TaskGUID     *string
	RunID        *string
	JobNamespace *string
	JobName      *string
	JobType      *string
	EventType    *string
	HasLineage   *bool
	// Search matches the lowercased substring anywhere in the fields the run
	// list shows, mirroring the free-text box the page used to filter with in
	// the browser.
	Search *string
	Limit  *int
	Offset *int
}

// UpsertOpenLineageRun persists one OpenLineage run and mirrors it into
// meta_registry_resource.
func (s *Store) UpsertOpenLineageRun(ctx context.Context, run *OpenLineageRunMessage) (*OpenLineageRunMessage, error) {
	persisted, err := s.UpsertOpenLineageRuns(ctx, []*OpenLineageRunMessage{run})
	if err != nil {
		return nil, err
	}
	return persisted[0], nil
}

// UpsertOpenLineageRuns persists several runs in a single transaction, so
// ingesting a batch costs one transaction instead of one per event. A delivery
// that a finished run refuses is reported as that finished run.
func (s *Store) UpsertOpenLineageRuns(ctx context.Context, runs []*OpenLineageRunMessage) ([]*OpenLineageRunMessage, error) {
	if len(runs) == 0 {
		return nil, nil
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	// Locking the task of every run in the same order keeps two concurrent
	// batches from deadlocking on each other's task rows. The returned slice
	// stays in the caller's order, which callers rely on to match a persisted run
	// with its event.
	persisted := make([]*OpenLineageRunMessage, len(runs))
	// Runs a finished row refused. Their stored row is left untouched, so it is
	// read back once the transaction is committed.
	var refused []int
	for _, index := range taskLockOrder(runs) {
		run := runs[index]
		// The task lock has to be taken before the previous run is read, so the
		// counters below stay exact under concurrent deliveries.
		task, err := lockOpenLineageTask(ctx, tx, run)
		if err != nil {
			return nil, err
		}
		previous, err := previousRunState(ctx, tx, run)
		if err != nil {
			return nil, err
		}
		if previous.finished() && run.EventType != openLineageEventComplete {
			refused = append(refused, index)
			continue
		}

		runPersisted, err := upsertOpenLineageRunImpl(ctx, tx, run)
		if err != nil {
			return nil, err
		}

		updatedTask, err := s.applyOpenLineageRun(ctx, tx, task, runPersisted, previous.Existed, previous.HasLineage)
		if err != nil {
			return nil, err
		}

		if err := s.upsertOpenLineageRunMetaRegistry(ctx, tx, runPersisted); err != nil {
			return nil, err
		}
		if err := s.upsertOpenLineageTaskMetaRegistry(ctx, tx, updatedTask); err != nil {
			return nil, err
		}
		persisted[index] = runPersisted
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	for _, index := range refused {
		run := runs[index]
		stored, err := s.GetOpenLineageRun(ctx, &FindOpenLineageRunMessage{
			JobNamespace: &run.JobNamespace,
			JobName:      &run.JobName,
			JobType:      &run.JobType,
			RunID:        &run.RunID,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to read the run a finished run refused")
		}
		if stored == nil {
			// Retention pruned the row between the commit and this read, so the
			// delivery that lost is what stands for the identity again.
			stored = run
		}
		persisted[index] = stored
	}

	return persisted, nil
}

// taskLockOrder returns the indexes of runs ordered by task GUID. Every writer
// locks its tasks in this order, so two batches cannot deadlock on each other's
// task rows. The stable sort keeps runs of one task in their original order.
func taskLockOrder(runs []*OpenLineageRunMessage) []int {
	order := make([]int, len(runs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return strings.Compare(runs[a].TaskGUID, runs[b].TaskGUID)
	})
	return order
}

// previousRun is the stored state of the run a delivery replaces.
type previousRun struct {
	Existed    bool
	HasLineage bool
	EventType  string
}

// finished reports whether the stored run already reached its final state. A
// finished run is terminal: no later delivery may rewrite its row, because the
// row holds the payload its lineage came from.
func (p previousRun) finished() bool {
	return p.Existed && p.EventType == openLineageEventComplete
}

// previousRunState reports the lineage flag and state of the run this one
// replaces, and whether such a run already existed. It must be called while the
// task row is locked.
func previousRunState(ctx context.Context, tx *sql.Tx, run *OpenLineageRunMessage) (previousRun, error) {
	var previous previousRun
	if err := tx.QueryRowContext(ctx, `
		SELECT has_lineage, event_type
		FROM openlineage_run
		WHERE job_namespace = $1 AND job_name = $2 AND job_type = $3 AND run_id = $4
	`, run.JobNamespace, run.JobName, run.JobType, run.RunID).Scan(&previous.HasLineage, &previous.EventType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return previousRun{}, nil
		}
		return previousRun{}, errors.Wrap(err, "failed to read the previous openlineage run")
	}
	previous.Existed = true
	return previous, nil
}

func upsertOpenLineageRunImpl(ctx context.Context, tx *sql.Tx, run *OpenLineageRunMessage) (*OpenLineageRunMessage, error) {
	var eventTime any
	if run.EventTime != nil {
		eventTime = *run.EventTime
	}

	var persisted OpenLineageRunMessage
	var rawPayload []byte
	var persistedEventTime sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openlineage_run (
			guid,
			task_guid,
			run_id,
			job_namespace,
			job_name,
			job_type,
			event_type,
			event_time,
			producer,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			parent_run_id,
			root_job_namespace,
			root_job_name,
			root_run_id,
			source,
			input_count,
			output_count,
			has_lineage,
			raw_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		ON CONFLICT (job_namespace, job_name, job_type, run_id) DO UPDATE SET
			guid = EXCLUDED.guid,
			task_guid = EXCLUDED.task_guid,
			event_type = EXCLUDED.event_type,
			event_time = EXCLUDED.event_time,
			producer = EXCLUDED.producer,
			integration = EXCLUDED.integration,
			processing_type = EXCLUDED.processing_type,
			parent_job_namespace = EXCLUDED.parent_job_namespace,
			parent_job_name = EXCLUDED.parent_job_name,
			parent_run_id = EXCLUDED.parent_run_id,
			root_job_namespace = EXCLUDED.root_job_namespace,
			root_job_name = EXCLUDED.root_job_name,
			root_run_id = EXCLUDED.root_run_id,
			source = EXCLUDED.source,
			input_count = EXCLUDED.input_count,
			output_count = EXCLUDED.output_count,
			has_lineage = EXCLUDED.has_lineage,
			raw_payload = EXCLUDED.raw_payload,
			updated_at = NOW()
		RETURNING
			id,
			guid,
			task_guid,
			run_id,
			job_namespace,
			job_name,
			job_type,
			event_type,
			event_time,
			producer,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			parent_run_id,
			root_job_namespace,
			root_job_name,
			root_run_id,
			source,
			input_count,
			output_count,
			has_lineage,
			raw_payload,
			created_at,
			updated_at
	`, run.GUID, run.TaskGUID, run.RunID, run.JobNamespace, run.JobName, run.JobType, run.EventType, eventTime, run.Producer, run.Integration, run.ProcessingType, run.ParentJobNamespace, run.ParentJobName, run.ParentRunID, run.RootJobNamespace, run.RootJobName, run.RootRunID, run.Source, run.InputCount, run.OutputCount, run.HasLineage, run.RawPayload).Scan(
		&persisted.ID,
		&persisted.GUID,
		&persisted.TaskGUID,
		&persisted.RunID,
		&persisted.JobNamespace,
		&persisted.JobName,
		&persisted.JobType,
		&persisted.EventType,
		&persistedEventTime,
		&persisted.Producer,
		&persisted.Integration,
		&persisted.ProcessingType,
		&persisted.ParentJobNamespace,
		&persisted.ParentJobName,
		&persisted.ParentRunID,
		&persisted.RootJobNamespace,
		&persisted.RootJobName,
		&persisted.RootRunID,
		&persisted.Source,
		&persisted.InputCount,
		&persisted.OutputCount,
		&persisted.HasLineage,
		&rawPayload,
		&persisted.CreatedAt,
		&persisted.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to upsert openlineage run")
	}
	if persistedEventTime.Valid {
		t := persistedEventTime.Time
		persisted.EventTime = &t
	}
	persisted.RawPayload = rawPayload
	return &persisted, nil
}

func (s *Store) upsertOpenLineageRunMetaRegistry(ctx context.Context, tx *sql.Tx, run *OpenLineageRunMessage) error {
	storedMetadata := buildOpenLineageRunStoredMetadata(run)
	metadataBytes, metaHash, err := CalcStoreMetaHash(storedMetadata)
	if err != nil {
		return errors.Wrap(err, "failed to calculate openlineage run metadata hash")
	}

	_, err = s.BatchCreateMetaRegistryResource(ctx, tx, []*CreateMetaRegistryResourceMessage{
		{
			MetaRegistryResource: MetaRegistryResource{
				GUID:       run.GUID,
				ObjectType: storepb.MetaType_OPENLINEAGE,
				Metadata:   storedMetadata,
				MetaHash:   metaHash,
			},
			MetadataBytes: metadataBytes,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to mirror openlineage run into meta registry")
	}

	return nil
}

// GetOpenLineageRun returns one persisted run or nil if none exists.
func (s *Store) GetOpenLineageRun(ctx context.Context, find *FindOpenLineageRunMessage) (*OpenLineageRunMessage, error) {
	list, err := s.ListOpenLineageRun(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Conflict, "found %d OpenLineage runs with filter %+v, expect 1", len(list), find)
	}
	return list[0], nil
}

// ListOpenLineageRun lists persisted OpenLineage runs matching the filter.
func (s *Store) ListOpenLineageRun(ctx context.Context, find *FindOpenLineageRunMessage) ([]*OpenLineageRunMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.GUID; v != nil {
		where, args = append(where, fmt.Sprintf("guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.TaskGUID; v != nil {
		where, args = append(where, fmt.Sprintf("task_guid = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.RunID; v != nil {
		where, args = append(where, fmt.Sprintf("run_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobNamespace; v != nil {
		where, args = append(where, fmt.Sprintf("job_namespace = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobName; v != nil {
		where, args = append(where, fmt.Sprintf("job_name = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.JobType; v != nil {
		where, args = append(where, fmt.Sprintf("job_type = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.EventType; v != nil {
		where, args = append(where, fmt.Sprintf("event_type = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.HasLineage; v != nil {
		if *v {
			where = append(where, "has_lineage = TRUE")
		} else {
			where = append(where, "has_lineage = FALSE")
		}
	}
	// POSITION rather than ILIKE so a search string is a literal substring: the
	// browser filter this replaces treated % and _ as ordinary characters.
	if v := find.Search; v != nil {
		where = append(where, fmt.Sprintf(
			`POSITION(lower($%d) IN lower(concat_ws(' ', job_name, job_namespace, run_id, event_type, producer, source))) > 0`,
			len(args)+1,
		))
		args = append(args, *v)
	}

	query := `
		SELECT
			id,
			guid,
			task_guid,
			run_id,
			job_namespace,
			job_name,
			job_type,
			event_type,
			event_time,
			producer,
			integration,
			processing_type,
			parent_job_namespace,
			parent_job_name,
			parent_run_id,
			root_job_namespace,
			root_job_name,
			root_run_id,
			source,
			input_count,
			output_count,
			has_lineage,
			raw_payload,
			created_at,
			updated_at
		FROM openlineage_run
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY event_time DESC NULLS LAST, id DESC`
	pageClause, pageArgs := openLineagePageClause(find.Limit, find.Offset, len(args))
	query += pageClause
	args = append(args, pageArgs...)

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query openlineage runs")
	}
	defer rows.Close()

	var result []*OpenLineageRunMessage
	for rows.Next() {
		var msg OpenLineageRunMessage
		var eventTime sql.NullTime
		var rawPayload []byte
		if err := rows.Scan(
			&msg.ID,
			&msg.GUID,
			&msg.TaskGUID,
			&msg.RunID,
			&msg.JobNamespace,
			&msg.JobName,
			&msg.JobType,
			&msg.EventType,
			&eventTime,
			&msg.Producer,
			&msg.Integration,
			&msg.ProcessingType,
			&msg.ParentJobNamespace,
			&msg.ParentJobName,
			&msg.ParentRunID,
			&msg.RootJobNamespace,
			&msg.RootJobName,
			&msg.RootRunID,
			&msg.Source,
			&msg.InputCount,
			&msg.OutputCount,
			&msg.HasLineage,
			&rawPayload,
			&msg.CreatedAt,
			&msg.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan openlineage run")
		}
		if eventTime.Valid {
			t := eventTime.Time
			msg.EventTime = &t
		}
		msg.RawPayload = rawPayload
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

func buildOpenLineageRunStoredMetadata(run *OpenLineageRunMessage) *storepb.StoredMetadata {
	summary := &storepb.OpenLineageRunSummary{
		Guid:               run.GUID,
		TaskGuid:           run.TaskGUID,
		RunId:              run.RunID,
		JobNamespace:       run.JobNamespace,
		JobName:            run.JobName,
		JobType:            run.JobType,
		EventType:          run.EventType,
		Producer:           run.Producer,
		Source:             run.Source,
		Integration:        run.Integration,
		ProcessingType:     run.ProcessingType,
		ParentJobNamespace: run.ParentJobNamespace,
		ParentJobName:      run.ParentJobName,
		ParentRunId:        run.ParentRunID,
		RootJobNamespace:   run.RootJobNamespace,
		RootJobName:        run.RootJobName,
		RootRunId:          run.RootRunID,
		InputCount:         run.InputCount,
		OutputCount:        run.OutputCount,
		HasLineage:         run.HasLineage,
		CreatedAt:          timestamppb.New(run.CreatedAt),
		UpdatedAt:          timestamppb.New(run.UpdatedAt),
	}
	if run.EventTime != nil {
		summary.EventTime = timestamppb.New(*run.EventTime)
	}

	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_OpenlineageRunSummary{
			OpenlineageRunSummary: summary,
		},
	}
}
