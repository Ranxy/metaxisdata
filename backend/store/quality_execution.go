package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityExecutionColumns is the projection every quality_execution query
// returns, in the order scanQualityExecution reads it.
const qualityExecutionColumns = `id, run_id, rule_id, rule_revision, rule_engine_version, target_guid, target_type, target_path, instance_resource_id, trigger, attempt, initiator_id, state, status, observed_value, observed_kind, thresholds, failed_rows, total_rows, sampled, sample_rows, truncated, sample, executed_sql, error_code, error_message, duration_ms, started_at, finished_at`

// QualityExecutionMessage is one rule checked once: the durable, user-facing
// unit of the data quality engine. Its public resource name is built from ID,
// so the row id is the resource identity.
//
// An execution is planned PENDING with the target snapshot, then marked RUNNING
// by the runner, which finally writes the verdict and the evidence with
// FinishQualityExecution. RunID names the internal dispatch batch it was planned
// for; that batch is never exposed through the API.
type QualityExecutionMessage struct {
	ID int64
	// RunID is the internal dispatch batch this execution belongs to. It admits
	// one connection lease per instance and cascades retention; it is not part of
	// the public resource.
	RunID             int64
	RuleID            int64
	RuleRevision      int32
	RuleEngineVersion string
	// TargetGUID, TargetType and TargetPath are the target snapshot taken when the
	// execution was planned. They are not rewritten by a rule edit, so historical
	// evidence keeps rendering the target it judged.
	TargetGUID         string
	TargetType         storepb.MetaType
	TargetPath         *storepb.QualityTargetPath
	InstanceResourceID string
	// State is the execution lifecycle. Status is the quality outcome, which is
	// UNSPECIFIED until the execution reaches COMPLETED.
	State   storepb.QualityExecutionState
	Trigger storepb.QualityExecutionTrigger
	// Attempt is the scheduled attempt number in the target's current due cycle:
	// 1 for the first attempt, 2+ for a retry after an abandoned attempt. Manual
	// executions always use 1.
	Attempt int32
	// InitiatorID is the principal that asked for a manual execution; nil for a
	// scheduled one.
	InitiatorID *int32
	// Status is the quality verdict, set by FinishQualityExecution.
	Status storepb.QualityStatus
	// ObservedValue is nil when the check could not produce a value (ERROR or
	// NOT_EVALUATED); zero is a legitimate observed value.
	ObservedValue *float64
	ObservedKind  storepb.QualityObservedKind
	Thresholds    *storepb.QualityThresholds
	FailedRows    int64
	TotalRows     int64
	Sampled       bool
	SampleRows    int64
	Truncated     bool
	Sample        *storepb.QualityFailureSamples
	ExecutedSQL   string
	ErrorCode     string
	ErrorMessage  string
	DurationMS    int32
	// StartedAt is when the execution was planned (queue time). MarkQualityExecutionRunning
	// rewrites it to the moment the runner picked it up, so a queued execution is
	// still reachable by the stale sweep.
	StartedAt time.Time
	// FinishedAt is the zero time until the execution reaches COMPLETED or
	// ABANDONED.
	FinishedAt time.Time
}

// FindQualityExecutionMessage is the query filter for quality executions.
// Filters compose with AND; an empty filter lists every execution. OrderAsc
// selects ascending started_at for callers that read history forwards.
type FindQualityExecutionMessage struct {
	RunID *int64
	// RuleID and TargetGUID select one rule or one target.
	RuleID     *int64
	TargetGUID *string
	// States filters the lifecycle; Statuses filters the quality outcome.
	States   []storepb.QualityExecutionState
	Statuses []storepb.QualityStatus
	Trigger  *storepb.QualityExecutionTrigger
	// InstanceResourceID and InitiatorID narrow a manual or per-instance view.
	InstanceResourceID *string
	InitiatorID        *int32
	// StartAt and EndAt bound started_at as the half-open interval
	// [StartAt, EndAt), so a boundary execution is never returned twice.
	StartAt  *time.Time
	EndAt    *time.Time
	Limit    *int
	Offset   *int
	OrderAsc bool
}

// buildQualityExecutionWhere renders the WHERE predicate and positional
// arguments for an execution filter. It is a pure function so the predicate is
// unit-testable without a database; ordering and paging stay in the callers.
func buildQualityExecutionWhere(find *FindQualityExecutionMessage) (string, []any) {
	if find == nil {
		find = &FindQualityExecutionMessage{}
	}
	conditions := []string{}
	args := []any{}
	placeholder := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if find.RunID != nil {
		conditions = append(conditions, "run_id = "+placeholder(*find.RunID))
	}
	if find.RuleID != nil {
		conditions = append(conditions, "rule_id = "+placeholder(*find.RuleID))
	}
	if len(find.States) > 0 {
		// The column is INT2 while lib/pq sends int4 arrays for []int32, so the
		// cast is explicit and lets PostgreSQL narrow the array.
		states := make([]int32, len(find.States))
		for i, state := range find.States {
			states[i] = int32(state)
		}
		conditions = append(conditions, "state = ANY("+placeholder(pq.Array(states))+"::int[])")
	}
	if len(find.Statuses) > 0 {
		statuses := make([]int32, len(find.Statuses))
		for i, status := range find.Statuses {
			statuses[i] = int32(status)
		}
		conditions = append(conditions, "status = ANY("+placeholder(pq.Array(statuses))+"::int[])")
	}
	if find.Trigger != nil {
		conditions = append(conditions, "trigger = "+placeholder(int32(*find.Trigger))+"::int")
	}
	if find.TargetGUID != nil {
		conditions = append(conditions, "target_guid = "+placeholder(*find.TargetGUID))
	}
	if find.InstanceResourceID != nil {
		conditions = append(conditions, "instance_resource_id = "+placeholder(*find.InstanceResourceID))
	}
	if find.InitiatorID != nil {
		conditions = append(conditions, "initiator_id = "+placeholder(*find.InitiatorID))
	}
	if find.StartAt != nil {
		conditions = append(conditions, "started_at >= "+placeholder(*find.StartAt))
	}
	if find.EndAt != nil {
		conditions = append(conditions, "started_at < "+placeholder(*find.EndAt))
	}

	where := strings.Join(conditions, " AND ")
	if where == "" {
		where = "TRUE"
	}
	return where, args
}

// qualityExecutionOrderClause renders the ORDER BY of an execution list. The id
// breaks ties on started_at so a page boundary is deterministic; ascending order
// is for reading a rule's history forwards.
func qualityExecutionOrderClause(orderAsc bool) string {
	if orderAsc {
		return " ORDER BY started_at ASC, id ASC"
	}
	return " ORDER BY started_at DESC, id DESC"
}

// scanQualityExecution reads one quality_execution row. The JSONB snapshots are
// unmarshalled with the protojson wrapper so a row written by an older engine
// version stays readable, and the nullable observed value, initiator and
// finished_at columns keep their "absent" representation instead of a zero value.
func scanQualityExecution(scanner interface{ Scan(...any) error }) (*QualityExecutionMessage, error) {
	var (
		msg           QualityExecutionMessage
		targetPathRaw []byte
		observedValue sql.NullFloat64
		thresholdsRaw []byte
		sampleRaw     []byte
		initiator     sql.NullInt32
		finished      sql.NullTime
	)
	err := scanner.Scan(
		&msg.ID,
		&msg.RunID,
		&msg.RuleID,
		&msg.RuleRevision,
		&msg.RuleEngineVersion,
		&msg.TargetGUID,
		&msg.TargetType,
		&targetPathRaw,
		&msg.InstanceResourceID,
		&msg.Trigger,
		&msg.Attempt,
		&initiator,
		&msg.State,
		&msg.Status,
		&observedValue,
		&msg.ObservedKind,
		&thresholdsRaw,
		&msg.FailedRows,
		&msg.TotalRows,
		&msg.Sampled,
		&msg.SampleRows,
		&msg.Truncated,
		&sampleRaw,
		&msg.ExecutedSQL,
		&msg.ErrorCode,
		&msg.ErrorMessage,
		&msg.DurationMS,
		&msg.StartedAt,
		&finished,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan quality execution")
	}

	msg.TargetPath = &storepb.QualityTargetPath{}
	if err := unmarshalQualityProto(targetPathRaw, msg.TargetPath); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality execution target path")
	}
	if observedValue.Valid {
		msg.ObservedValue = &observedValue.Float64
	}
	msg.Thresholds = &storepb.QualityThresholds{}
	if err := unmarshalQualityProto(thresholdsRaw, msg.Thresholds); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality execution thresholds")
	}
	msg.Sample = &storepb.QualityFailureSamples{}
	if err := unmarshalQualityProto(sampleRaw, msg.Sample); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality execution failure samples")
	}
	if initiator.Valid {
		msg.InitiatorID = &initiator.Int32
	}
	if finished.Valid {
		msg.FinishedAt = finished.Time
	}
	return &msg, nil
}

// qualityExecutionPlanStatement inserts one planned execution and returns its
// row id, so the caller can enqueue and report exactly the rows it planned. The
// row is always PENDING with the target snapshot the caller resolved from the
// rule's head revision; the plan leaves the verdict columns at their defaults.
const qualityExecutionPlanStatement = `
	INSERT INTO quality_execution (
		run_id, rule_id, rule_revision, rule_engine_version, target_guid, target_type,
		target_path, instance_resource_id, trigger, attempt, initiator_id, state, started_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
	)
	RETURNING id
`

// PlanQualityExecutions inserts the executions of one dispatch batch as PENDING
// rows and fills each message's ID with the inserted row id. It is called once
// per batch, before the batch is enqueued: a planned execution is the durable
// promise that the rule will be checked, so a crash between planning and
// execution shows up as an abandoned row rather than a silently missing check,
// and the returned ids are what the queue and the API carry.
//
// The inserts run in one transaction so a partially planned batch is never left
// behind, and the whole batch either exists or does not. An empty batch is a
// no-op and never touches the database.
func (s *Store) PlanQualityExecutions(ctx context.Context, executions []*QualityExecutionMessage) error {
	if len(executions) == 0 {
		return nil
	}
	// Validate the whole batch before opening a transaction, so a bad request
	// never reaches the driver and never leaves a partially planned batch behind.
	for i, execution := range executions {
		if execution == nil {
			return common.Errorf(common.Invalid, "quality execution %d is nil", i)
		}
		if execution.RunID <= 0 {
			return common.Errorf(common.Invalid, "quality execution %d has no dispatch batch", i)
		}
		if execution.RuleID <= 0 {
			return common.Errorf(common.Invalid, "quality execution %d has no rule", i)
		}
		if execution.Trigger == storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_UNSPECIFIED {
			return common.Errorf(common.Invalid, "quality execution %d has no trigger", i)
		}
	}
	now := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin quality execution plan transaction")
	}
	defer tx.Rollback()

	for i, execution := range executions {
		targetPathRaw, err := marshalQualityProto(execution.TargetPath)
		if err != nil {
			return errors.Wrap(err, "failed to marshal quality execution target path")
		}
		startedAt := execution.StartedAt
		if startedAt.IsZero() {
			startedAt = now
		}
		initiator := sql.NullInt32{}
		if execution.InitiatorID != nil {
			initiator = sql.NullInt32{Int32: *execution.InitiatorID, Valid: true}
		}
		state := storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING
		attempt := execution.Attempt
		if attempt <= 0 {
			attempt = 1
		}
		if err := tx.QueryRowContext(ctx, qualityExecutionPlanStatement,
			execution.RunID,
			execution.RuleID,
			execution.RuleRevision,
			execution.RuleEngineVersion,
			execution.TargetGUID,
			int32(execution.TargetType),
			targetPathRaw,
			execution.InstanceResourceID,
			int32(execution.Trigger),
			attempt,
			initiator,
			int32(state),
			startedAt,
		).Scan(&execution.ID); err != nil {
			return errors.Wrapf(err, "failed to plan quality execution %d", i)
		}
		// Reflect the row's defaults on the caller's message: a caller that
		// reports the planned executions must not render an UNSPECIFIED state or
		// the zero time the database replaced.
		execution.State = state
		execution.Attempt = attempt
		execution.StartedAt = startedAt
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit quality execution plan transaction")
	}
	return nil
}

// qualityExecutionMarkRunningStatement starts one planned execution. The
// PENDING guard is in the statement, not only in the caller, so a retried
// dispatch cannot restart an execution another worker already owns.
const qualityExecutionMarkRunningStatement = `
	UPDATE quality_execution
	SET state = 2, started_at = $2
	WHERE id = $1 AND state = 1
	RETURNING id
`

// MarkQualityExecutionRunning moves a planned execution to RUNNING and records
// the moment the runner actually picked it up. The rewrite of started_at matters
// for the stale-execution sweep: an execution that waited in the queue must not
// look like it had been running since it was planned.
//
// The transition is idempotent for an execution that is already RUNNING, so a
// retried dispatch is harmless; a terminal execution cannot be revived and
// returns common.Conflict.
func (s *Store) MarkQualityExecutionRunning(ctx context.Context, id int64, at time.Time) error {
	if id <= 0 {
		return common.Errorf(common.Invalid, "quality execution id is required")
	}
	if at.IsZero() {
		return common.Errorf(common.Invalid, "the running start time is required")
	}

	var startedID int64
	err := s.GetDB().QueryRowContext(ctx, qualityExecutionMarkRunningStatement, id, at).Scan(&startedID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return errors.Wrap(err, "failed to mark quality execution running")
	}

	var current storepb.QualityExecutionState
	if err := s.GetDB().QueryRowContext(ctx, `SELECT state FROM quality_execution WHERE id = $1`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality execution %d not found", id)
		}
		return errors.Wrap(err, "failed to read quality execution state")
	}
	switch current {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING:
		// A retried dispatch of an execution another worker already owns is a no-op.
		return nil
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		return common.Errorf(common.Conflict, "quality execution %d is already %s", id, current)
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING:
		return common.Errorf(common.Conflict, "quality execution %d changed concurrently", id)
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED:
		return common.Errorf(common.Internal, "quality execution %d has no lifecycle state", id)
	default:
		return common.Errorf(common.Internal, "quality execution %d has unknown state %d", id, current)
	}
}

// qualityExecutionFinishStatement writes the terminal state of one execution.
// The revision, engine version and target snapshot are merged with
// COALESCE/NULLIF rather than overwritten: a failure before the runner resolved
// them carries zero values, and the planned row's snapshot is the correct
// evidence in that case. The target_path JSONB default "{}" is treated the same
// way as an absent value.
const qualityExecutionFinishStatement = `
	UPDATE quality_execution
	SET rule_revision = COALESCE(NULLIF($2::int, 0), rule_revision),
		rule_engine_version = COALESCE(NULLIF($3::text, ''), rule_engine_version),
		target_guid = COALESCE(NULLIF($4::text, ''), target_guid),
		target_type = COALESCE(NULLIF($5::int2, 0::int2), target_type),
		target_path = COALESCE(NULLIF($6::jsonb, '{}'::jsonb), target_path),
		instance_resource_id = COALESCE(NULLIF($7::text, ''), instance_resource_id),
		status = $8::int2,
		observed_value = $9::double precision,
		observed_kind = $10::int2,
		thresholds = $11::jsonb,
		failed_rows = $12::bigint,
		total_rows = $13::bigint,
		sampled = $14::boolean,
		sample_rows = $15::bigint,
		truncated = $16::boolean,
		sample = $17::jsonb,
		executed_sql = $18::text,
		error_code = $19::text,
		error_message = $20::text,
		duration_ms = $21::int4,
		state = $22::int2,
		finished_at = $23::timestamptz
	WHERE id = $1 AND state IN (1, 2)
	RETURNING id
`

// FinishQualityExecution writes an execution's verdict, evidence and terminal
// state. Only a PENDING or RUNNING execution can finish; finishing one twice is
// a Conflict rather than a rewrite, because the verdict it reported is already
// immutable.
//
// A completed execution must carry a real quality status: UNSPECIFIED is the
// planned row's "no verdict yet" value, not a result.
func (s *Store) FinishQualityExecution(ctx context.Context, execution *QualityExecutionMessage) error {
	if execution == nil {
		return common.Errorf(common.Invalid, "quality execution is required")
	}
	if execution.ID <= 0 {
		return common.Errorf(common.Invalid, "quality execution id is required")
	}
	if execution.Status == storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		return common.Errorf(common.Invalid, "a completed quality execution must carry its quality status")
	}

	targetPathRaw, err := marshalQualityProto(execution.TargetPath)
	if err != nil {
		return errors.Wrap(err, "failed to marshal quality execution target path")
	}
	thresholdsRaw, err := marshalQualityProto(execution.Thresholds)
	if err != nil {
		return errors.Wrap(err, "failed to marshal quality execution thresholds")
	}
	sampleRaw, err := marshalQualityProto(execution.Sample)
	if err != nil {
		return errors.Wrap(err, "failed to marshal quality execution failure samples")
	}
	observedValue := sql.NullFloat64{}
	if execution.ObservedValue != nil {
		observedValue = sql.NullFloat64{Float64: *execution.ObservedValue, Valid: true}
	}
	// The planned row already carries the initiator and the dispatch trigger;
	// finishing never rewrites either of them.
	finishedAt := execution.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}

	var finishedID int64
	err = s.GetDB().QueryRowContext(ctx, qualityExecutionFinishStatement,
		execution.ID,
		execution.RuleRevision,
		execution.RuleEngineVersion,
		execution.TargetGUID,
		int32(execution.TargetType),
		targetPathRaw,
		execution.InstanceResourceID,
		int32(execution.Status),
		observedValue,
		int32(execution.ObservedKind),
		thresholdsRaw,
		execution.FailedRows,
		execution.TotalRows,
		execution.Sampled,
		execution.SampleRows,
		execution.Truncated,
		sampleRaw,
		execution.ExecutedSQL,
		execution.ErrorCode,
		execution.ErrorMessage,
		execution.DurationMS,
		int32(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED),
		finishedAt,
	).Scan(&finishedID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return errors.Wrap(err, "failed to finish quality execution")
	}

	var current storepb.QualityExecutionState
	if err := s.GetDB().QueryRowContext(ctx, `SELECT state FROM quality_execution WHERE id = $1`, execution.ID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality execution %d not found", execution.ID)
		}
		return errors.Wrap(err, "failed to read quality execution state")
	}
	switch current {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		return common.Errorf(common.Conflict, "quality execution %d is already %s", execution.ID, current)
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING:
		return common.Errorf(common.Conflict, "quality execution %d changed concurrently", execution.ID)
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED:
		return common.Errorf(common.Internal, "quality execution %d has no lifecycle state", execution.ID)
	default:
		return common.Errorf(common.Internal, "quality execution %d has unknown state %d", execution.ID, current)
	}
}

// qualityExecutionAbandonBatchStatement closes the executions of one dispatch
// batch that never reached a terminal state. The state list is the guard: a
// completed execution keeps its verdict.
const qualityExecutionAbandonBatchStatement = `
	UPDATE quality_execution
	SET state = $1, finished_at = $2
	WHERE run_id = $3 AND state IN ($4, $5)
`

// AbandonQualityExecutions marks the unfinished executions of one dispatch batch
// ABANDONED, and returns how many it changed. It is the cleanup path for a batch
// whose work could not be completed: an execution left PENDING or RUNNING would
// otherwise never reach a terminal state.
func (s *Store) AbandonQualityExecutions(ctx context.Context, runID int64, at time.Time) (int64, error) {
	if runID <= 0 {
		return 0, common.Errorf(common.Invalid, "quality dispatch batch id is required")
	}
	if at.IsZero() {
		return 0, common.Errorf(common.Invalid, "the abandon time is required")
	}

	result, err := s.GetDB().ExecContext(ctx, qualityExecutionAbandonBatchStatement,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED,
		at,
		runID,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
	)
	if err != nil {
		return 0, errors.Wrap(err, "failed to abandon quality executions")
	}
	abandoned, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count abandoned quality executions")
	}
	return abandoned, nil
}

// qualityExecutionAbandonStaleStatement closes every execution that started
// before the cutoff and never reached a terminal state.
const qualityExecutionAbandonStaleStatement = `
	UPDATE quality_execution
	SET state = $1, finished_at = NOW()
	WHERE started_at < $2 AND state IN ($3, $4)
`

// AbandonStaleQualityExecutions marks every execution that started before the
// cutoff and never reached a terminal state ABANDONED, and returns how many it
// changed. The server calls it at startup: a PENDING or RUNNING execution it
// finds then belongs to a process that is gone, and retrying it is not safe, so
// it is closed and left for the next scheduled tick.
//
// finished_at is stamped with the database clock, like every other finish path.
// A zero cutoff is rejected so a misconfigured caller cannot abandon the whole
// table.
func (s *Store) AbandonStaleQualityExecutions(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, common.Errorf(common.Invalid, "a stale quality execution cutoff is required")
	}

	result, err := s.GetDB().ExecContext(ctx, qualityExecutionAbandonStaleStatement,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED,
		cutoff,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
	)
	if err != nil {
		return 0, errors.Wrap(err, "failed to abandon stale quality executions")
	}
	abandoned, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count abandoned stale quality executions")
	}
	return abandoned, nil
}

// qualityExecutionByIDStatement reads one execution by its row id. The public
// resource name carries this id, so a lookup is a primary-key read.
const qualityExecutionByIDStatement = `
	SELECT ` + qualityExecutionColumns + `
	FROM quality_execution
	WHERE id = $1
`

// GetQualityExecution returns one execution by row id, or (nil, nil) when no
// such execution exists. The id is validated before any query so a malformed
// name fails without touching the database.
func (s *Store) GetQualityExecution(ctx context.Context, id int64) (*QualityExecutionMessage, error) {
	if id <= 0 {
		return nil, common.Errorf(common.Invalid, "quality execution id is required")
	}
	execution, err := scanQualityExecution(s.GetDB().QueryRowContext(ctx, qualityExecutionByIDStatement, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to get the quality execution")
	}
	return execution, nil
}

// ListQualityExecutions lists executions matching the filter. Descending
// started_at is the default because the execution list reads the newest first;
// OrderAsc serves callers reading a rule's history forwards.
func (s *Store) ListQualityExecutions(ctx context.Context, find *FindQualityExecutionMessage) ([]*QualityExecutionMessage, error) {
	where, args := buildQualityExecutionWhere(find)
	orderAsc := find != nil && find.OrderAsc
	query := `SELECT ` + qualityExecutionColumns + ` FROM quality_execution WHERE ` + where + qualityExecutionOrderClause(orderAsc)

	if find != nil && find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find != nil && find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list quality executions")
	}
	defer rows.Close()

	var executions []*QualityExecutionMessage
	for rows.Next() {
		execution, err := scanQualityExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality executions")
	}
	return executions, nil
}

// ListRecentQualityExecutions returns the rule's newest completed executions.
// The row_count_change rule type reads its baseline through this method, and an
// execution that never reached a verdict (PENDING, RUNNING or ABANDONED) is not
// a baseline observation.
func (s *Store) ListRecentQualityExecutions(ctx context.Context, ruleID int64, limit int) ([]*QualityExecutionMessage, error) {
	if ruleID <= 0 {
		return nil, common.Errorf(common.Invalid, "quality rule id is required")
	}
	if limit <= 0 {
		return nil, common.Errorf(common.Invalid, "quality execution limit must be positive")
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityExecutionColumns+`
		FROM quality_execution
		WHERE rule_id = $1 AND state = $2
		ORDER BY started_at DESC, id DESC
		LIMIT $3
	`, ruleID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list recent quality executions")
	}
	return scanQualityExecutions(rows)
}

// qualityExecutionLatestStatement selects the newest completed execution of each
// rule in one statement. DISTINCT ON (rule_id) with the matching ORDER BY prefix
// is what keeps this one index scan per rule instead of one query per rule.
const qualityExecutionLatestStatement = `
	SELECT DISTINCT ON (rule_id) ` + qualityExecutionColumns + `
	FROM quality_execution
	WHERE rule_id = ANY($1::bigint[]) AND state = $2
	ORDER BY rule_id, started_at DESC, id DESC
`

// ListLatestQualityExecutions returns each rule's newest completed execution,
// keyed by rule id. Rules that have no completed execution are absent from the
// map; a merely planned execution is not a result. An empty input is a no-op and
// never touches the database.
func (s *Store) ListLatestQualityExecutions(ctx context.Context, ruleIDs []int64) (map[int64]*QualityExecutionMessage, error) {
	latest := make(map[int64]*QualityExecutionMessage, len(ruleIDs))
	if len(ruleIDs) == 0 {
		return latest, nil
	}

	rows, err := s.GetDB().QueryContext(ctx, qualityExecutionLatestStatement, pq.Array(ruleIDs),
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list latest quality executions")
	}
	executions, err := scanQualityExecutions(rows)
	if err != nil {
		return nil, err
	}
	for _, execution := range executions {
		latest[execution.RuleID] = execution
	}
	return latest, nil
}

// scanQualityExecutions drains a result set into messages and closes it. Every
// list in this file reads the same projection, so they share one scan loop.
func scanQualityExecutions(rows *sql.Rows) ([]*QualityExecutionMessage, error) {
	defer rows.Close()

	var executions []*QualityExecutionMessage
	for rows.Next() {
		execution, err := scanQualityExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality executions")
	}
	return executions, nil
}

// CountQualityExecutionsSince counts the executions planned at or after since,
// whatever their lifecycle state. It is the workspace's daily execution tally:
// the runner compares it with the configured ceiling before scanning for due
// rules, so an accidentally dense schedule cannot hammer the target databases
// all day.
func (s *Store) CountQualityExecutionsSince(ctx context.Context, since time.Time) (int, error) {
	if since.IsZero() {
		return 0, common.Errorf(common.Invalid, "a start time is required")
	}
	var count int
	scanErr := s.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM quality_execution WHERE started_at >= $1`,
		since,
	).Scan(&count)
	if scanErr != nil {
		return 0, errors.Wrap(scanErr, "failed to count quality executions")
	}
	return count, nil
}

// ListRecentQualityExecutionsByRuleIDs returns each rule's newest completed
// executions, up to limitPerRule, in one statement. The workspace draws a history
// strip per rule, so the query partitions by rule instead of running one query per
// rule. Rules without a completed execution are absent from the map.
func (s *Store) ListRecentQualityExecutionsByRuleIDs(ctx context.Context, ruleIDs []int64, limitPerRule int) (map[int64][]*QualityExecutionMessage, error) {
	if len(ruleIDs) == 0 {
		return map[int64][]*QualityExecutionMessage{}, nil
	}
	if limitPerRule <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive execution limit is required")
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		WITH ranked AS (
			SELECT `+qualityExecutionColumns+`,
				ROW_NUMBER() OVER (PARTITION BY rule_id ORDER BY started_at DESC, id DESC) AS rn
			FROM quality_execution
			WHERE rule_id = ANY($1::bigint[]) AND state = $2
		)
		SELECT `+qualityExecutionColumns+`
		FROM ranked
		WHERE rn <= $3
		ORDER BY rule_id, started_at DESC, id DESC
	`, pq.Array(ruleIDs), int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED), limitPerRule)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the rules' recent executions")
	}
	executions, err := scanQualityExecutions(rows)
	if err != nil {
		return nil, err
	}
	byRule := make(map[int64][]*QualityExecutionMessage, len(ruleIDs))
	for _, execution := range executions {
		byRule[execution.RuleID] = append(byRule[execution.RuleID], execution)
	}
	return byRule, nil
}
