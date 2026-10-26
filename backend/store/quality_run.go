package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityRunColumns is the projection every quality_run query returns, in the
// order scanQualityRun reads it.
const qualityRunColumns = "id, resource_id, instance_resource_id, target_guid, target_type, target_path, trigger, status, outcome, counts, initiator_id, started_at, finished_at"

// QualityRunMessage is one row of quality_run: a dispatch batch that belongs to
// exactly one target. It is created either on request (MANUAL) or by one
// scheduler tick (SCHEDULED) and exists to admit one connection lease per
// instance, aggregate a storm notification, and cascade retention. The
// user-facing unit is still the quality_execution row it plans, but the batch
// itself is the target's run in the read model.
type QualityRunMessage struct {
	ID                 int64
	ResourceID         string
	InstanceResourceID string
	// TargetGUID and TargetType are the target the batch belongs to. They are
	// part of the batch identity, not derived from the executions.
	TargetGUID string
	TargetType storepb.MetaType
	// TargetPath is the display snapshot the plan used, so the batch still names
	// where its target lived after the target is renamed or orphaned.
	TargetPath *storepb.QualityTargetPath
	Trigger    storepb.QualityExecutionTrigger
	// Status is the batch's lifecycle, not its quality result. A COMPLETED batch
	// can still have failed executions; Outcome carries what the batch found.
	Status storepb.QualityExecutionState
	// Outcome is the worst QualityStatus the batch's completed executions
	// reported, computed by the executor when it finishes the batch. UNSPECIFIED
	// is how the store spells the NULL column: a queued batch has no outcome yet,
	// and an abandoned batch never produced one. Lifecycle and outcome are
	// separate on purpose, because a batch that finished successfully may still
	// have found a failing rule, and a batch that was abandoned is not a quality
	// failure.
	Outcome storepb.QualityStatus
	Counts  *storepb.QualityStatusCounts
	// InitiatorID is the principal that asked for the batch; nil for a scheduled
	// batch, which has no human initiator.
	InitiatorID *int32
	// StartedAt is stamped by the database when the row is created (queue time)
	// and rewritten when the runner picks the batch up, so a batch that sits
	// queued is still reachable by the stale-batch sweep.
	StartedAt time.Time
	// FinishedAt is nil until the batch reaches a terminal status.
	FinishedAt *time.Time
	// RuleRevisionMin and RuleRevisionMax are the range of rule revisions the
	// batch's executions actually judged. They are not columns: the read paths
	// fill them from the executions so an edit after planning is reflected.
	RuleRevisionMin int32
	RuleRevisionMax int32
}

// marshalQualityRunCounts renders the counts JSONB column. A nil message and an
// empty message both store "{}", matching the column default, so the column is
// never NULL.
func marshalQualityRunCounts(counts *storepb.QualityStatusCounts) ([]byte, error) {
	if counts == nil {
		counts = &storepb.QualityStatusCounts{}
	}
	payload, err := protojson.Marshal(counts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality run counts")
	}
	return payload, nil
}

// scanQualityRun reads one quality_run row. The JSONB counts column is
// unmarshalled with the protojson wrapper, and the nullable outcome, initiator
// and finished_at columns keep their "absent" representation instead of a zero
// value.
func scanQualityRun(scanner interface{ Scan(...any) error }) (*QualityRunMessage, error) {
	var (
		msg        QualityRunMessage
		targetPath []byte
		outcome    sql.NullInt32
		countsRaw  []byte
		initiator  sql.NullInt32
		finished   sql.NullTime
	)
	err := scanner.Scan(
		&msg.ID,
		&msg.ResourceID,
		&msg.InstanceResourceID,
		&msg.TargetGUID,
		&msg.TargetType,
		&targetPath,
		&msg.Trigger,
		&msg.Status,
		&outcome,
		&countsRaw,
		&initiator,
		&msg.StartedAt,
		&finished,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan quality run")
	}

	msg.TargetPath = &storepb.QualityTargetPath{}
	if len(targetPath) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(targetPath, msg.TargetPath); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal quality run target path")
		}
	}
	msg.Outcome = storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
	if outcome.Valid {
		msg.Outcome = storepb.QualityStatus(outcome.Int32)
	}
	msg.Counts = &storepb.QualityStatusCounts{}
	if len(countsRaw) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(countsRaw, msg.Counts); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal quality run counts")
		}
	}
	if initiator.Valid {
		msg.InitiatorID = &initiator.Int32
	}
	if finished.Valid {
		msg.FinishedAt = &finished.Time
	}
	return &msg, nil
}

// CreateQualityRun inserts one queued run. The caller owns ResourceID, and
// started_at is stamped by the database; a run is normally created PENDING and
// only FinishQualityRun writes a terminal status. The unique resource_id index
// makes a duplicated dispatch a Conflict rather than a duplicate batch.
func (s *Store) CreateQualityRun(ctx context.Context, run *QualityRunMessage) (*QualityRunMessage, error) {
	if run == nil {
		return nil, common.Errorf(common.Invalid, "quality run is required")
	}
	if strings.TrimSpace(run.ResourceID) == "" {
		return nil, common.Errorf(common.Invalid, "quality run resource id is required")
	}
	if strings.TrimSpace(run.TargetGUID) == "" {
		return nil, common.Errorf(common.Invalid, "quality run target guid is required")
	}
	if run.TargetType == storepb.MetaType_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality run target type is required")
	}
	if run.Trigger == storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality run trigger is required")
	}

	status := run.Status
	if status == storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED {
		status = storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING
	}
	if status == storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED || status == storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED {
		return nil, common.Errorf(common.Invalid, "a quality run cannot be created in terminal status %s", status)
	}

	counts, err := marshalQualityRunCounts(run.Counts)
	if err != nil {
		return nil, err
	}
	targetPath := run.TargetPath
	if targetPath == nil {
		targetPath = &storepb.QualityTargetPath{}
	}
	targetPathJSON, err := marshalQualityProto(targetPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality run target path")
	}
	outcome := sql.NullInt32{}
	if run.Outcome != storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		outcome = sql.NullInt32{Int32: int32(run.Outcome), Valid: true}
	}
	initiator := sql.NullInt32{}
	if run.InitiatorID != nil {
		initiator = sql.NullInt32{Int32: *run.InitiatorID, Valid: true}
	}

	msg, err := scanQualityRun(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_run (
			resource_id, instance_resource_id, target_guid, target_type, target_path,
			trigger, status, outcome, counts, initiator_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+qualityRunColumns,
		run.ResourceID,
		run.InstanceResourceID,
		strings.TrimSpace(run.TargetGUID),
		int16(run.TargetType),
		targetPathJSON,
		run.Trigger,
		status,
		outcome,
		counts,
		initiator,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "quality run %q already exists", run.ResourceID)
		}
		return nil, errors.Wrap(err, "failed to create quality run")
	}
	return msg, nil
}

// MarkQualityRunRunning moves a queued run to RUNNING and records the moment the
// runner actually picked it up. The rewrite of started_at matters for the
// stale-run sweep: a run that waited in the queue must not look like it had been
// executing since it was created.
//
// The transition is idempotent for a run that is already RUNNING, so a retried
// dispatch is harmless; a terminal run cannot be revived and returns
// common.Conflict.
func (s *Store) MarkQualityRunRunning(ctx context.Context, runID int64, at time.Time) error {
	if runID <= 0 {
		return common.Errorf(common.Invalid, "quality run id is required")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin quality run transaction")
	}
	defer tx.Rollback()

	var current storepb.QualityExecutionState
	if err := tx.QueryRowContext(ctx, `SELECT status FROM quality_run WHERE id = $1 FOR UPDATE`, runID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality run %d not found", runID)
		}
		return errors.Wrap(err, "failed to read quality run status")
	}

	switch current {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING:
		_, err = tx.ExecContext(ctx, `
			UPDATE quality_run SET status = $2, started_at = $3 WHERE id = $1
		`, runID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING, at)
		if err != nil {
			return errors.Wrap(err, "failed to mark quality run running")
		}
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING:
		// A retried dispatch of a run another worker already owns is a no-op.
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		return common.Errorf(common.Conflict, "quality run %d is already %s", runID, current)
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED:
		return common.Errorf(common.Internal, "quality run %d has no lifecycle status", runID)
	default:
		return common.Errorf(common.Internal, "quality run %d has unknown status %d", runID, current)
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit quality run transaction")
	}
	return nil
}

// FinishQualityRun writes a batch's terminal status, its rollup outcome, the
// per-status counts and finished_at. Only a PENDING or RUNNING batch can finish;
// finishing a batch twice is a Conflict rather than a rewrite, because the
// executions it reported are already immutable.
//
// A COMPLETED batch must carry the rollup outcome: NULL outcome is reserved for
// batches that never finished (queued) or never produced a rollup (abandoned).
func (s *Store) FinishQualityRun(ctx context.Context, runID int64, status storepb.QualityExecutionState, outcome storepb.QualityStatus, counts *storepb.QualityStatusCounts, at time.Time) error {
	if runID <= 0 {
		return common.Errorf(common.Invalid, "quality run id is required")
	}
	if status != storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED && status != storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED {
		return common.Errorf(common.Invalid, "quality run status %s is not terminal", status)
	}
	if status == storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED && outcome == storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		return common.Errorf(common.Invalid, "a completed quality run must carry its rollup outcome")
	}

	countsRaw, err := marshalQualityRunCounts(counts)
	if err != nil {
		return err
	}
	outcomeArg := sql.NullInt32{}
	if outcome != storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		outcomeArg = sql.NullInt32{Int32: int32(outcome), Valid: true}
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin quality run transaction")
	}
	defer tx.Rollback()

	var current storepb.QualityExecutionState
	if err := tx.QueryRowContext(ctx, `SELECT status FROM quality_run WHERE id = $1 FOR UPDATE`, runID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality run %d not found", runID)
		}
		return errors.Wrap(err, "failed to read quality run status")
	}

	switch current {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING:
		_, err = tx.ExecContext(ctx, `
			UPDATE quality_run
			SET status = $2, outcome = $3, counts = $4, finished_at = $5
			WHERE id = $1
		`, runID, status, outcomeArg, countsRaw, at)
		if err != nil {
			return errors.Wrap(err, "failed to finish quality run")
		}
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		return common.Errorf(common.Conflict, "quality run %d is already %s", runID, current)
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED:
		return common.Errorf(common.Internal, "quality run %d has no lifecycle status", runID)
	default:
		return common.Errorf(common.Internal, "quality run %d has unknown status %d", runID, current)
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit quality run transaction")
	}
	return nil
}

// AbandonStaleQualityRuns marks every run that started before the cutoff and
// never reached a terminal state ABANDONED, and returns how many it changed. The
// server calls it at startup: a PENDING or RUNNING run it finds then belongs to a
// process that is gone, and retrying it is not safe, so it is closed and left for
// the next scheduled tick.
//
// finished_at is stamped with the database clock, like every other finish path.
// A zero cutoff is rejected so a misconfigured caller cannot abandon the whole
// table.
func (s *Store) AbandonStaleQualityRuns(ctx context.Context, startedBefore time.Time) (int64, error) {
	if startedBefore.IsZero() {
		return 0, common.Errorf(common.Invalid, "a stale quality run cutoff is required")
	}

	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_run
		SET status = $1, finished_at = NOW()
		WHERE started_at < $2 AND status IN ($3, $4)
	`,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED,
		startedBefore,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
	)
	if err != nil {
		return 0, errors.Wrap(err, "failed to abandon stale quality runs")
	}
	abandoned, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count abandoned quality runs")
	}
	return abandoned, nil
}

// qualityRunDeleteExpiredStatement reclaims one batch and, through the ON DELETE
// CASCADE on quality_execution.run_id, every execution the batch planned.
const qualityRunDeleteExpiredStatement = `DELETE FROM quality_run WHERE started_at < $1`

// DeleteExpiredQualityRuns removes the internal dispatch batches that started
// before cutoff. Each batch's executions go with it through the ON DELETE
// CASCADE, so one statement reclaims a batch and the history it holds.
func (s *Store) DeleteExpiredQualityRuns(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, common.Errorf(common.Invalid, "a cutoff is required")
	}
	result, err := s.GetDB().ExecContext(ctx, qualityRunDeleteExpiredStatement, cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired quality runs")
	}
	return result.RowsAffected()
}

// FindQualityRunMessage is the query filter for the run directory. Every field is
// optional; an empty filter lists every run newest first.
type FindQualityRunMessage struct {
	InstanceResourceID *string
	Trigger            *storepb.QualityExecutionTrigger
	Status             *storepb.QualityExecutionState
	Outcome            *storepb.QualityStatus
	// TargetGUID and TargetType keep only runs that belong to this target. The
	// type is optional for callers that only know the GUID, but the target
	// workspace passes it because a GUID is not unique across object types.
	TargetGUID *string
	TargetType *storepb.MetaType
	// StartedAfter and StartedBefore bound started_at as the half-open interval
	// [StartedAfter, StartedBefore).
	StartedAfter  *time.Time
	StartedBefore *time.Time
	Limit         *int
	Offset        *int
}

// QualityTargetRunSummaryMessage is a target's newest run plus how many runs have
// touched it. A batch belongs to one target, so the run itself is the target's
// slice; no per-target aggregation is needed.
type QualityTargetRunSummaryMessage struct {
	TargetGUID string
	TargetType storepb.MetaType
	Run        *QualityRunMessage
	RunCount   int32
}

// qualifyQualityRunColumns prefixes qualityRunColumns with a table alias. A query
// that joins quality_run needs the qualification to keep the unqualified columns
// unambiguous.
func qualifyQualityRunColumns(alias string) string {
	columns := strings.Split(qualityRunColumns, ",")
	for index := range columns {
		columns[index] = alias + "." + strings.TrimSpace(columns[index])
	}
	return strings.Join(columns, ", ")
}

// buildQualityRunWhere renders the predicate and positional arguments for a run
// filter. It is a pure function so the scoping (especially the target identity
// predicate) can be guard-tested without a database.
func buildQualityRunWhere(find *FindQualityRunMessage) ([]string, []any) {
	if find == nil {
		find = &FindQualityRunMessage{}
	}
	where := []string{"TRUE"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if find.InstanceResourceID != nil {
		where = append(where, "r.instance_resource_id = "+addArg(*find.InstanceResourceID))
	}
	if find.Trigger != nil {
		where = append(where, "r.trigger = "+addArg(int32(*find.Trigger))+"::int")
	}
	if find.Status != nil {
		where = append(where, "r.status = "+addArg(int32(*find.Status))+"::int")
	}
	if find.Outcome != nil {
		where = append(where, "r.outcome = "+addArg(int32(*find.Outcome))+"::int")
	}
	if find.TargetGUID != nil {
		where = append(where, "r.target_guid = "+addArg(*find.TargetGUID))
	}
	if find.TargetType != nil {
		where = append(where, "r.target_type = "+addArg(int32(*find.TargetType))+"::int2")
	}
	if find.StartedAfter != nil {
		where = append(where, "r.started_at >= "+addArg(*find.StartedAfter))
	}
	if find.StartedBefore != nil {
		where = append(where, "r.started_at < "+addArg(*find.StartedBefore))
	}
	return where, args
}

// ListQualityRuns lists the internal dispatch batches, newest first. A run is
// readable through the API as a projection only: it still owns no write path.
func (s *Store) ListQualityRuns(ctx context.Context, find *FindQualityRunMessage) ([]*QualityRunMessage, error) {
	if find == nil {
		find = &FindQualityRunMessage{}
	}
	where, args := buildQualityRunWhere(find)
	query := `SELECT ` + qualityRunColumns + ` FROM quality_run r WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY r.started_at DESC, r.id DESC`
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list quality runs")
	}
	defer rows.Close()

	runs := []*QualityRunMessage{}
	for rows.Next() {
		run, err := scanQualityRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality runs")
	}
	return runs, nil
}

// GetQualityRun reads one run by its row id. A missing row is (nil, nil), the
// same contract as the other Get methods here.
func (s *Store) GetQualityRun(ctx context.Context, id int64) (*QualityRunMessage, error) {
	if id <= 0 {
		return nil, common.Errorf(common.Invalid, "a quality run id is required")
	}
	run, err := scanQualityRun(s.GetDB().QueryRowContext(ctx, `
		SELECT `+qualityRunColumns+`
		FROM quality_run
		WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality run")
	}
	return run, nil
}

// qualityTargetRunSummaryRow adapts *sql.Rows for scanQualityRun while this query
// also returns the target key and the run count. That keeps one query able to read
// the run's columns and its summary without duplicating the run scan mapping.
type qualityTargetRunSummaryRow struct {
	rows     *sql.Rows
	key      *MetaGUIDKey
	runCount *int32
}

func (r qualityTargetRunSummaryRow) Scan(dest ...any) error {
	extra := []any{&r.key.GUID, &r.key.ObjectType, r.runCount}
	return r.rows.Scan(append(dest, extra...)...)
}

// ListQualityTargetRunSummaries reads, for each requested target, its newest run
// and how many runs planned at least one execution for it. A target that has
// never run is absent from the map; a batch that failed before planning any
// execution is not a scan and does not count or become the latest run.
func (s *Store) ListQualityTargetRunSummaries(ctx context.Context, keys []MetaGUIDKey) (map[MetaGUIDKey]*QualityTargetRunSummaryMessage, error) {
	summaries := make(map[MetaGUIDKey]*QualityTargetRunSummaryMessage, len(keys))
	if len(keys) == 0 {
		return summaries, nil
	}
	guids := make([]string, 0, len(keys))
	objectTypes := make([]int16, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, int16(key.ObjectType))
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (r.target_guid, r.target_type) r.id
			FROM quality_run r
			JOIN unnest($1::text[], $2::int2[]) AS k(guid, object_type)
				ON r.target_guid = k.guid AND r.target_type = k.object_type
			WHERE EXISTS (SELECT 1 FROM quality_execution e WHERE e.run_id = r.id)
			ORDER BY r.target_guid, r.target_type, r.started_at DESC, r.id DESC
		),
		run_counts AS (
			SELECT r.target_guid, r.target_type, (COUNT(*))::INT AS run_count
			FROM quality_run r
			JOIN unnest($1::text[], $2::int2[]) AS k(guid, object_type)
				ON r.target_guid = k.guid AND r.target_type = k.object_type
			WHERE EXISTS (SELECT 1 FROM quality_execution e WHERE e.run_id = r.id)
			GROUP BY r.target_guid, r.target_type
		)
		SELECT `+qualifyQualityRunColumns("r")+`, r.target_guid, r.target_type, rc.run_count
		FROM latest l
		JOIN quality_run r ON r.id = l.id
		JOIN run_counts rc ON rc.target_guid = r.target_guid AND rc.target_type = r.target_type
		ORDER BY r.target_guid, r.target_type
	`, pq.Array(guids), pq.Array(objectTypes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the targets' latest runs")
	}
	defer rows.Close()

	runIDs := make([]int64, 0, len(keys))
	for rows.Next() {
		var (
			key      MetaGUIDKey
			runCount int32
		)
		run, err := scanQualityRun(qualityTargetRunSummaryRow{rows: rows, key: &key, runCount: &runCount})
		if err != nil {
			return nil, err
		}
		summaries[key] = &QualityTargetRunSummaryMessage{
			TargetGUID: key.GUID,
			TargetType: key.ObjectType,
			Run:        run,
			RunCount:   runCount,
		}
		runIDs = append(runIDs, run.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the targets' latest runs")
	}

	ranges, err := s.ListQualityRunRevisionRanges(ctx, runIDs)
	if err != nil {
		return nil, err
	}
	for _, summary := range summaries {
		if summary.Run == nil {
			continue
		}
		if revisionRange, ok := ranges[summary.Run.ID]; ok {
			summary.Run.RuleRevisionMin = revisionRange.Min
			summary.Run.RuleRevisionMax = revisionRange.Max
		}
	}
	return summaries, nil
}

// QualityRunRevisionRange is the range of rule revisions one run's executions
// actually judged.
type QualityRunRevisionRange struct {
	Min int32
	Max int32
}

// ListQualityRunRevisionRanges returns the revision range each run judged. The
// execution rows carry the revision that really produced the verdict, so an edit
// after planning is reflected instead of the planned revision.
func (s *Store) ListQualityRunRevisionRanges(ctx context.Context, runIDs []int64) (map[int64]QualityRunRevisionRange, error) {
	ranges := make(map[int64]QualityRunRevisionRange, len(runIDs))
	if len(runIDs) == 0 {
		return ranges, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT run_id, (MIN(rule_revision))::INT, (MAX(rule_revision))::INT
		FROM quality_execution
		WHERE run_id = ANY($1::bigint[])
		GROUP BY run_id
	`, pq.Array(runIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality run revision ranges")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			runID int64
			rng   QualityRunRevisionRange
		)
		if err := rows.Scan(&runID, &rng.Min, &rng.Max); err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality run revision range")
		}
		ranges[runID] = rng
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality run revision ranges")
	}
	return ranges, nil
}
