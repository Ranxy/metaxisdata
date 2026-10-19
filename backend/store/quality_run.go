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
const qualityRunColumns = "id, resource_id, instance_resource_id, trigger, status, outcome, counts, initiator_id, started_at, finished_at"

// QualityRunMessage is one row of quality_run: a batch of rules executed
// together, either on request (MANUAL) or by one scheduler tick (SCHEDULED).
type QualityRunMessage struct {
	ID                 int64
	ResourceID         string
	InstanceResourceID string
	Trigger            storepb.QualityRunTrigger
	// Status is the run's lifecycle, not its quality result. A COMPLETED run can
	// still have failed rules; Outcome carries what the run found.
	Status storepb.QualityRunStatus
	// Outcome is the worst QualityStatus the run's results reported, computed by
	// the executor when it finishes the run. UNSPECIFIED is how the store spells
	// the NULL column: a queued run has no outcome yet, and an abandoned run
	// never produced one. Lifecycle and outcome are separate on purpose, because
	// a run that finished successfully may still have found a failing rule, and
	// a run that was abandoned is not a quality failure.
	Outcome storepb.QualityStatus
	Counts  *storepb.QualityRunCounts
	// InitiatorID is the principal that asked for the run; nil for a scheduled
	// run, which has no human initiator.
	InitiatorID *int32
	// StartedAt is stamped by the database when the row is created (queue time)
	// and rewritten when the runner picks the run up, so a run that sits queued
	// is still reachable by the stale-run sweep.
	StartedAt time.Time
	// FinishedAt is nil until the run reaches a terminal status.
	FinishedAt *time.Time
}

// FindQualityRunMessage is the query filter for quality runs. Filters compose
// with AND; an empty filter lists every run.
type FindQualityRunMessage struct {
	ResourceID *string
	// Statuses keeps only runs in any of the listed lifecycle states.
	Statuses []storepb.QualityRunStatus
	// Outcome filters on the rollup status. UNSPECIFIED matches rows whose
	// outcome is still NULL, which is how a caller asks for unfinished runs.
	Outcome            *storepb.QualityStatus
	Trigger            *storepb.QualityRunTrigger
	InstanceResourceID *string
	InitiatorID        *int32
	// StartAt and EndAt bound started_at as the half-open interval
	// [StartAt, EndAt), so paging over a window cannot return a boundary run
	// twice.
	StartAt *time.Time
	EndAt   *time.Time
	Limit   *int
	Offset  *int
}

// buildQualityRunWhere renders the WHERE predicate and positional arguments for
// a run filter. It is a pure function so the predicate is unit-testable without
// a database; ordering and paging stay in the callers.
func buildQualityRunWhere(find *FindQualityRunMessage) (string, []any) {
	if find == nil {
		find = &FindQualityRunMessage{}
	}
	conditions := []string{}
	args := []any{}
	placeholder := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if find.ResourceID != nil {
		conditions = append(conditions, "resource_id = "+placeholder(*find.ResourceID))
	}
	if len(find.Statuses) > 0 {
		// The column is INT2 while lib/pq sends int4 arrays for []int32, so the
		// cast is explicit and lets PostgreSQL narrow the array.
		statuses := make([]int32, len(find.Statuses))
		for i, status := range find.Statuses {
			statuses[i] = int32(status)
		}
		conditions = append(conditions, "status = ANY("+placeholder(pq.Array(statuses))+"::int[])")
	}
	if find.Outcome != nil {
		if *find.Outcome == storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
			conditions = append(conditions, "outcome IS NULL")
		} else {
			conditions = append(conditions, "outcome = "+placeholder(int32(*find.Outcome))+"::int")
		}
	}
	if find.Trigger != nil {
		conditions = append(conditions, "trigger = "+placeholder(int32(*find.Trigger))+"::int")
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

// marshalQualityRunCounts renders the counts JSONB column. A nil message and an
// empty message both store "{}", matching the column default, so the column is
// never NULL.
func marshalQualityRunCounts(counts *storepb.QualityRunCounts) ([]byte, error) {
	if counts == nil {
		counts = &storepb.QualityRunCounts{}
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
	msg.Counts = &storepb.QualityRunCounts{}
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
	if run.Trigger == storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality run trigger is required")
	}

	status := run.Status
	if status == storepb.QualityRunStatus_QUALITY_RUN_STATUS_UNSPECIFIED {
		status = storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING
	}
	if status == storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED || status == storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED {
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

	var current storepb.QualityRunStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM quality_run WHERE id = $1 FOR UPDATE`, runID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality run %d not found", runID)
		}
		return errors.Wrap(err, "failed to read quality run status")
	}

	switch current {
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING:
		_, err = tx.ExecContext(ctx, `
			UPDATE quality_run SET status = $2, started_at = $3 WHERE id = $1
		`, runID, storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING, at)
		if err != nil {
			return errors.Wrap(err, "failed to mark quality run running")
		}
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING:
		// A retried dispatch of a run another worker already owns is a no-op.
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED:
		return common.Errorf(common.Conflict, "quality run %d is already %s", runID, current)
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_UNSPECIFIED:
		return common.Errorf(common.Internal, "quality run %d has no lifecycle status", runID)
	default:
		return common.Errorf(common.Internal, "quality run %d has unknown status %d", runID, current)
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit quality run transaction")
	}
	return nil
}

// FinishQualityRun writes a run's terminal status, its rollup outcome, the
// per-status counts and finished_at. Only a PENDING or RUNNING run can finish;
// finishing a run twice is a Conflict rather than a rewrite, because the results
// it reported are already immutable.
//
// A COMPLETED run must carry the rollup outcome: NULL outcome is reserved for
// runs that never finished (queued) or never produced a rollup (abandoned).
func (s *Store) FinishQualityRun(ctx context.Context, runID int64, status storepb.QualityRunStatus, outcome storepb.QualityStatus, counts *storepb.QualityRunCounts, at time.Time) error {
	if runID <= 0 {
		return common.Errorf(common.Invalid, "quality run id is required")
	}
	if status != storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED && status != storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED {
		return common.Errorf(common.Invalid, "quality run status %s is not terminal", status)
	}
	if status == storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED && outcome == storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
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

	var current storepb.QualityRunStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM quality_run WHERE id = $1 FOR UPDATE`, runID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Errorf(common.NotFound, "quality run %d not found", runID)
		}
		return errors.Wrap(err, "failed to read quality run status")
	}

	switch current {
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING, storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING:
		_, err = tx.ExecContext(ctx, `
			UPDATE quality_run
			SET status = $2, outcome = $3, counts = $4, finished_at = $5
			WHERE id = $1
		`, runID, status, outcomeArg, countsRaw, at)
		if err != nil {
			return errors.Wrap(err, "failed to finish quality run")
		}
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED:
		return common.Errorf(common.Conflict, "quality run %d is already %s", runID, current)
	case storepb.QualityRunStatus_QUALITY_RUN_STATUS_UNSPECIFIED:
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
		storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED,
		startedBefore,
		storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
		storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING,
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

// GetQualityRun returns the one run matching the filter, or nil. More than one
// match is a Conflict: a caller asking for a run is asking for an identity, not
// a page.
func (s *Store) GetQualityRun(ctx context.Context, find *FindQualityRunMessage) (*QualityRunMessage, error) {
	runs, err := s.ListQualityRuns(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	if len(runs) > 1 {
		return nil, common.Errorf(common.Conflict, "found %d quality runs, expect 1", len(runs))
	}
	return runs[0], nil
}

// ListQualityRuns lists runs matching the filter, newest first. The id breaks
// ties on started_at so a page boundary is deterministic.
func (s *Store) ListQualityRuns(ctx context.Context, find *FindQualityRunMessage) ([]*QualityRunMessage, error) {
	where, args := buildQualityRunWhere(find)
	query := `SELECT ` + qualityRunColumns + ` FROM quality_run WHERE ` + where + ` ORDER BY started_at DESC, id DESC`

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
		return nil, errors.Wrap(err, "failed to list quality runs")
	}
	defer rows.Close()

	var runs []*QualityRunMessage
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
