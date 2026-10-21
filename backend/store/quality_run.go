package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityRunColumns is the projection every quality_run query returns, in the
// order scanQualityRun reads it.
const qualityRunColumns = "id, resource_id, instance_resource_id, trigger, status, outcome, counts, initiator_id, started_at, finished_at"

// QualityRunMessage is one row of quality_run: an internal dispatch batch of
// rules that are checked together, either on request (MANUAL) or by one
// scheduler tick (SCHEDULED). The batch is never exposed through the API; it
// exists to admit one connection lease per instance, aggregate a storm
// notification, and cascade retention. The user-facing unit is the
// quality_execution row it plans.
type QualityRunMessage struct {
	ID                 int64
	ResourceID         string
	InstanceResourceID string
	Trigger            storepb.QualityExecutionTrigger
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
		msg       QualityRunMessage
		outcome   sql.NullInt32
		countsRaw []byte
		initiator sql.NullInt32
		finished  sql.NullTime
	)
	err := scanner.Scan(
		&msg.ID,
		&msg.ResourceID,
		&msg.InstanceResourceID,
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
	outcome := sql.NullInt32{}
	if run.Outcome != storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		outcome = sql.NullInt32{Int32: int32(run.Outcome), Valid: true}
	}
	initiator := sql.NullInt32{}
	if run.InitiatorID != nil {
		initiator = sql.NullInt32{Int32: *run.InitiatorID, Valid: true}
	}

	msg, err := scanQualityRun(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_run (resource_id, instance_resource_id, trigger, status, outcome, counts, initiator_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+qualityRunColumns,
		run.ResourceID,
		run.InstanceResourceID,
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
