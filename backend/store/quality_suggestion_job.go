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

// qualitySuggestionJobTargetLimit is how many targets one job may cover. The API
// rejects a larger request; the store refuses to create a row that would make the
// runner hold a request open for an unbounded time.
const qualitySuggestionJobTargetLimit = 20

// qualitySuggestionJobColumns lists the columns every read of
// quality_suggestion_job returns, in the order scanQualitySuggestionJob expects
// them.
const qualitySuggestionJobColumns = `id, resource_id, status, intent, language, provider, model,
	prompt_version, targets, cancel_requested, created_by, created_at, started_at, finished_at, error`

// createQualitySuggestionJobQuery inserts one pending job. The runner claims it
// later; nothing is executed in the request that creates it.
const createQualitySuggestionJobQuery = `
	INSERT INTO quality_suggestion_job (
		resource_id, status, intent, language, provider, model, prompt_version,
		targets, cancel_requested, created_by, created_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	RETURNING id, created_at`

// claimQualitySuggestionJobQuery claims one pending job. FOR UPDATE SKIP LOCKED
// makes two runner replicas (or a restart overlapping itself) pick different jobs;
// the status predicate keeps a claimed job out of the candidate set.
const claimQualitySuggestionJobQuery = `SELECT ` + qualitySuggestionJobColumns + `
	FROM quality_suggestion_job
	WHERE status = 1
	ORDER BY id
	FOR UPDATE SKIP LOCKED
	LIMIT 1`

// markQualitySuggestionJobRunningQuery is the second half of the claim. It is a
// separate statement because the candidate row is already locked by the read.
const markQualitySuggestionJobRunningQuery = `
	UPDATE quality_suggestion_job SET status = 2, started_at = $2 WHERE id = $1`

// updateQualitySuggestionJobProgressQuery persists the targets snapshot and the
// provider facts the job started with. It only applies while the job is RUNNING, so
// a canceled or finished job cannot be rewritten by a late progress write.
const updateQualitySuggestionJobProgressQuery = `
	UPDATE quality_suggestion_job
	SET targets = $2, provider = $3, model = $4, prompt_version = $5
	WHERE id = $1 AND status = 2`

// finishQualitySuggestionJobQuery writes the terminal status. The RUNNING predicate
// is the finish guard: a job is finished once, and a duplicate finish is reported
// to the caller rather than rewriting the outcome.
const finishQualitySuggestionJobQuery = `
	UPDATE quality_suggestion_job
	SET status = $2, error = $3, finished_at = $4
	WHERE id = $1 AND status = 2`

// qualitySuggestionJobCancelLockQuery reads one job for the cancel decision with
// the row locked, so a concurrent runner progress write cannot interleave.
const qualitySuggestionJobCancelLockQuery = `SELECT ` + qualitySuggestionJobColumns + `
	FROM quality_suggestion_job WHERE resource_id = $1 FOR UPDATE`

// deleteExpiredQualitySuggestionJobsQuery prunes only terminal rows. The three
// status arguments are COMPLETED, FAILED and CANCELED; a pending or running job is
// never reclaimed.
const deleteExpiredQualitySuggestionJobsQuery = `
	DELETE FROM quality_suggestion_job
	WHERE status IN ($1, $2, $3) AND finished_at IS NOT NULL AND finished_at < $4`

// QualitySuggestionJobMessage is one row of quality_suggestion_job: a durable
// background AI drafting job over a fixed set of targets. The per-target progress
// is the JSONB snapshot, so one poll of the row reports the whole run.
type QualitySuggestionJobMessage struct {
	ID         int64
	ResourceID string
	Status     storepb.QualitySuggestionJobStatus
	Intent     string
	Language   string
	// Provider, Model and PromptVersion are the provider facts; the runner writes
	// them together with its first progress update.
	Provider      string
	Model         string
	PromptVersion string
	// Targets is the per-target snapshot; never nil on a row that was read.
	Targets         *storepb.QualitySuggestionJobTargets
	CancelRequested bool
	CreatedBy       *int32
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	Error           string
}

// FindQualitySuggestionJobMessage filters jobs. Every field is optional for a
// list; a get must name the resource.
type FindQualitySuggestionJobMessage struct {
	ResourceID *string
	Statuses   []storepb.QualitySuggestionJobStatus
	Limit      *int
	Offset     *int
}

// qualitySuggestionJobQuery assembles one filtered, ordered job read.
func qualitySuggestionJobQuery(find *FindQualitySuggestionJobMessage) (string, []any) {
	where := []string{"TRUE"}
	args := []any{}
	if find != nil && find.ResourceID != nil {
		args = append(args, *find.ResourceID)
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	if find != nil && len(find.Statuses) > 0 {
		statuses := make([]int32, 0, len(find.Statuses))
		for _, status := range find.Statuses {
			statuses = append(statuses, int32(status))
		}
		args = append(args, pq.Array(statuses))
		where = append(where, fmt.Sprintf("status = ANY($%d::int[])", len(args)))
	}
	query := `SELECT ` + qualitySuggestionJobColumns + ` FROM quality_suggestion_job WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY created_at DESC, id DESC`
	if find != nil && find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find != nil && find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}
	return query, args
}

// scanQualitySuggestionJob maps one quality_suggestion_job row. The nullable
// lifetime columns stay nil pointers, so "not started yet" is distinct from the
// zero instant.
func scanQualitySuggestionJob(row rowScanner) (*QualitySuggestionJobMessage, error) {
	var (
		message    QualitySuggestionJobMessage
		status     int16
		targets    []byte
		createdBy  sql.NullInt32
		startedAt  sql.NullTime
		finishedAt sql.NullTime
	)
	scanErr := row.Scan(
		&message.ID, &message.ResourceID, &status, &message.Intent, &message.Language, &message.Provider,
		&message.Model, &message.PromptVersion, &targets, &message.CancelRequested, &createdBy,
		&message.CreatedAt, &startedAt, &finishedAt, &message.Error,
	)
	if scanErr != nil {
		return nil, scanErr
	}
	message.Status = storepb.QualitySuggestionJobStatus(status)
	messageTargets := &storepb.QualitySuggestionJobTargets{}
	if len(targets) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(targets, messageTargets); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the quality suggestion job targets")
		}
	}
	message.Targets = messageTargets
	if createdBy.Valid {
		value := createdBy.Int32
		message.CreatedBy = &value
	}
	if startedAt.Valid {
		value := startedAt.Time
		message.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		message.FinishedAt = &value
	}
	return &message, nil
}

// qualitySuggestionJobStatusIsValid reports whether status is a real lifecycle
// state. UNSPECIFIED is not a state; a created job is PENDING and the runner moves
// it from there.
func qualitySuggestionJobStatusIsValid(status storepb.QualitySuggestionJobStatus) bool {
	switch status {
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED:
		return true
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED:
		return false
	default:
		return false
	}
}

// qualitySuggestionJobTerminalStatusIsValid reports whether status is one a
// finished job may carry.
func qualitySuggestionJobTerminalStatusIsValid(status storepb.QualitySuggestionJobStatus) bool {
	switch status {
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED:
		return true
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING:
		return false
	default:
		return false
	}
}

// qualitySuggestionJobTargetStateIsValid reports whether state is a real target
// state.
func qualitySuggestionJobTargetStateIsValid(state storepb.QualitySuggestionJobTargetState) bool {
	switch state {
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
		storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING,
		storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE,
		storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED,
		storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED:
		return true
	case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED:
		return false
	default:
		return false
	}
}

// validateQualitySuggestionJobTargets checks the target snapshot a job is stored
// with: one to twenty targets, each naming a real object and a real state. The
// runner relies on every target being addressable.
func validateQualitySuggestionJobTargets(targets *storepb.QualitySuggestionJobTargets) error {
	if targets == nil {
		return common.Errorf(common.Invalid, "quality suggestion job targets are required")
	}
	list := targets.GetTargets()
	if len(list) == 0 {
		return common.Errorf(common.Invalid, "a quality suggestion job needs at least one target")
	}
	if len(list) > qualitySuggestionJobTargetLimit {
		return common.Errorf(common.Invalid, "a quality suggestion job may have at most %d targets", qualitySuggestionJobTargetLimit)
	}
	for _, target := range list {
		if target == nil {
			return common.Errorf(common.Invalid, "a quality suggestion job target is required")
		}
		if strings.TrimSpace(target.GetTargetGuid()) == "" {
			return common.Errorf(common.Invalid, "a quality suggestion job target needs a target guid")
		}
		if target.GetTargetType() == 0 {
			return common.Errorf(common.Invalid, "a quality suggestion job target needs a target type")
		}
		if !qualitySuggestionJobTargetStateIsValid(target.GetState()) {
			return common.Errorf(common.Invalid, "unknown quality suggestion job target state %d", target.GetState())
		}
	}
	return nil
}

// validateQualitySuggestionJob checks what a job row cannot be stored without.
func validateQualitySuggestionJob(message *QualitySuggestionJobMessage) error {
	if message == nil {
		return common.Errorf(common.Invalid, "quality suggestion job is required")
	}
	if strings.TrimSpace(message.ResourceID) == "" {
		return common.Errorf(common.Invalid, "quality suggestion job resource id is required")
	}
	if !qualitySuggestionJobStatusIsValid(message.Status) {
		return common.Errorf(common.Invalid, "quality suggestion job status is required")
	}
	if strings.TrimSpace(message.Intent) == "" {
		return common.Errorf(common.Invalid, "quality suggestion job intent is required")
	}
	return validateQualitySuggestionJobTargets(message.Targets)
}

// validateFindQualitySuggestionJob keeps a get pointed at exactly one job: by
// status multiple rows are expected, so only a resource-scoped get is allowed and
// the queue is read with ListQualitySuggestionJobs.
func validateFindQualitySuggestionJob(find *FindQualitySuggestionJobMessage) error {
	if find == nil || find.ResourceID == nil || strings.TrimSpace(*find.ResourceID) == "" {
		return common.Errorf(common.Invalid, "quality suggestion job resource id is required")
	}
	return nil
}

// marshalQualitySuggestionJobTargets serializes the per-target snapshot.
func marshalQualitySuggestionJobTargets(targets *storepb.QualitySuggestionJobTargets) ([]byte, error) {
	raw, err := protojson.Marshal(targets)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal the quality suggestion job targets")
	}
	return raw, nil
}

// CreateQualitySuggestionJob inserts one PENDING job and returns it with the id and
// create time the store assigned.
func (s *Store) CreateQualitySuggestionJob(ctx context.Context, message *QualitySuggestionJobMessage) (*QualitySuggestionJobMessage, error) {
	if err := validateQualitySuggestionJob(message); err != nil {
		return nil, err
	}
	targetsRaw, err := marshalQualitySuggestionJobTargets(message.Targets)
	if err != nil {
		return nil, err
	}
	createdAt := message.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	var (
		id              int64
		storedCreatedAt time.Time
	)
	insertErr := s.GetDB().QueryRowContext(ctx, createQualitySuggestionJobQuery,
		message.ResourceID, int32(message.Status), message.Intent, message.Language, message.Provider,
		message.Model, message.PromptVersion, targetsRaw, message.CancelRequested, message.CreatedBy,
		createdAt,
	).Scan(&id, &storedCreatedAt)
	if insertErr != nil {
		return nil, errors.Wrap(insertErr, "failed to create a quality suggestion job")
	}

	stored := *message
	stored.ID = id
	stored.CreatedAt = storedCreatedAt
	return &stored, nil
}

// GetQualitySuggestionJob returns the job the find names, or (nil, nil) when it
// does not exist.
func (s *Store) GetQualitySuggestionJob(ctx context.Context, find *FindQualitySuggestionJobMessage) (*QualitySuggestionJobMessage, error) {
	if err := validateFindQualitySuggestionJob(find); err != nil {
		return nil, err
	}
	list, err := s.ListQualitySuggestionJobs(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Internal, "found %d quality suggestion jobs for one resource id, expect 1", len(list))
	}
	return list[0], nil
}

// ListQualitySuggestionJobs lists jobs matching the filter, newest first.
func (s *Store) ListQualitySuggestionJobs(ctx context.Context, find *FindQualitySuggestionJobMessage) ([]*QualitySuggestionJobMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin a quality suggestion job transaction")
	}
	defer tx.Rollback()

	query, args := qualitySuggestionJobQuery(find)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality suggestion jobs")
	}
	defer rows.Close()

	var jobs []*QualitySuggestionJobMessage
	for rows.Next() {
		job, err := scanQualitySuggestionJob(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality suggestion job")
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality suggestion jobs")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit a quality suggestion job transaction")
	}
	return jobs, nil
}

// ClaimPendingQualitySuggestionJob claims one PENDING job for the runner, marks it
// RUNNING and returns it. It returns (nil, nil) when no job is waiting. The claim is
// atomic: the candidate row is locked with SKIP LOCKED, so two workers never get
// the same job.
func (s *Store) ClaimPendingQualitySuggestionJob(ctx context.Context, now time.Time) (*QualitySuggestionJobMessage, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin a quality suggestion job claim")
	}
	defer tx.Rollback()

	job, err := scanQualitySuggestionJob(tx.QueryRowContext(ctx, claimQualitySuggestionJobQuery))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read a pending quality suggestion job")
	}
	if _, err := tx.ExecContext(ctx, markQualitySuggestionJobRunningQuery, job.ID, now); err != nil {
		return nil, errors.Wrap(err, "failed to mark the quality suggestion job running")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality suggestion job claim")
	}
	job.Status = storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING
	job.StartedAt = &now
	return job, nil
}

// UpdateQualitySuggestionJobProgress persists the per-target snapshot and the
// provider facts while the job is RUNNING. A job that is no longer running (the
// runner was superseded, or the process restarted) is a conflict, not a rewrite.
func (s *Store) UpdateQualitySuggestionJobProgress(ctx context.Context, id int64, targets *storepb.QualitySuggestionJobTargets, provider, model, promptVersion string) error {
	if id <= 0 {
		return common.Errorf(common.Invalid, "quality suggestion job id is required")
	}
	if targets == nil {
		return common.Errorf(common.Invalid, "quality suggestion job targets are required")
	}
	targetsRaw, err := marshalQualitySuggestionJobTargets(targets)
	if err != nil {
		return err
	}
	result, err := s.GetDB().ExecContext(ctx, updateQualitySuggestionJobProgressQuery,
		id, targetsRaw, provider, model, promptVersion)
	if err != nil {
		return errors.Wrap(err, "failed to update the quality suggestion job progress")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to count the updated quality suggestion job progress")
	}
	if affected == 0 {
		return common.Errorf(common.Conflict, "quality suggestion job %d is not running", id)
	}
	return nil
}

// FinishQualitySuggestionJob writes the terminal status of a RUNNING job. Only
// COMPLETED, FAILED and CANCELED are terminal, and the row must still be RUNNING:
// finishing twice is a conflict rather than a rewrite.
func (s *Store) FinishQualitySuggestionJob(ctx context.Context, id int64, status storepb.QualitySuggestionJobStatus, errText string, at time.Time) error {
	if id <= 0 {
		return common.Errorf(common.Invalid, "quality suggestion job id is required")
	}
	if !qualitySuggestionJobTerminalStatusIsValid(status) {
		return common.Errorf(common.Invalid, "quality suggestion job status %s is not terminal", status)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	result, err := s.GetDB().ExecContext(ctx, finishQualitySuggestionJobQuery, id, int32(status), strings.TrimSpace(errText), at)
	if err != nil {
		return errors.Wrap(err, "failed to finish the quality suggestion job")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to count the finished quality suggestion job")
	}
	if affected == 0 {
		return common.Errorf(common.Conflict, "quality suggestion job %d is not running", id)
	}
	return nil
}

// qualitySuggestionJobTargetFinishedAt stamps a target snapshot's finish time.
func qualitySuggestionJobTargetFinishedAt(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UTC().UnixMilli()
}

// markQualitySuggestionJobTargetsSkipped marks every target that was never asked.
// A target already DONE or FAILED keeps its outcome: skipping is not a rewrite.
func markQualitySuggestionJobTargetsSkipped(targets *storepb.QualitySuggestionJobTargets, at time.Time) {
	for _, target := range targets.GetTargets() {
		if target == nil {
			continue
		}
		switch target.GetState() {
		case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED:
			target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED
			target.FinishedAtUnixMs = qualitySuggestionJobTargetFinishedAt(at)
		case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED,
			storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED:
			// Already visited (or already skipped): the outcome is history.
		default:
			// Unknown states are left as stored.
		}
	}
}

// CancelQualitySuggestionJob cancels one job. A PENDING job is canceled
// immediately and its targets are SKIPPED; a RUNNING job records the cancel request
// and stops between targets (the in-flight model call finishes); a terminal job is
// returned unchanged, which makes cancel idempotent. Suggestions already queued are
// kept.
func (s *Store) CancelQualitySuggestionJob(ctx context.Context, resourceID string, at time.Time) (*QualitySuggestionJobMessage, error) {
	if strings.TrimSpace(resourceID) == "" {
		return nil, common.Errorf(common.Invalid, "quality suggestion job resource id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin a quality suggestion job cancel")
	}
	defer tx.Rollback()

	job, err := scanQualitySuggestionJob(tx.QueryRowContext(ctx, qualitySuggestionJobCancelLockQuery, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality suggestion job %q not found", resourceID)
		}
		return nil, errors.Wrap(err, "failed to read the quality suggestion job")
	}

	switch job.Status {
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING:
		markQualitySuggestionJobTargetsSkipped(job.Targets, at)
		targetsRaw, err := marshalQualitySuggestionJobTargets(job.Targets)
		if err != nil {
			return nil, err
		}
		_, execErr := tx.ExecContext(ctx, `
			UPDATE quality_suggestion_job SET status = $2, targets = $3, finished_at = $4 WHERE id = $1
		`, job.ID, int32(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED), targetsRaw, at)
		if execErr != nil {
			return nil, errors.Wrap(execErr, "failed to cancel the quality suggestion job")
		}
		job.Status = storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED
		job.FinishedAt = &at
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING:
		_, execErr := tx.ExecContext(ctx, `
			UPDATE quality_suggestion_job SET cancel_requested = TRUE WHERE id = $1
		`, job.ID)
		if execErr != nil {
			return nil, errors.Wrap(execErr, "failed to request the quality suggestion job cancel")
		}
		job.CancelRequested = true
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED:
		// Canceling a terminal job is idempotent: the stored outcome stands.
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED:
		return nil, common.Errorf(common.Internal, "quality suggestion job %d has no lifecycle status", job.ID)
	default:
		return nil, common.Errorf(common.Internal, "quality suggestion job %d has unknown status %d", job.ID, job.Status)
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the quality suggestion job cancel")
	}
	return job, nil
}

// AbandonStaleQualitySuggestionJobs closes the RUNNING jobs a previous process left
// behind. It is a startup-only sweep: work whose process is gone is not resumed.
// RUNNING targets become FAILED ("interrupted"), untouched targets become SKIPPED,
// and the job itself becomes FAILED. Suggestions already queued are kept.
func (s *Store) AbandonStaleQualitySuggestionJobs(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, common.Errorf(common.Invalid, "a stale quality suggestion job cutoff is required")
	}
	now := time.Now().UTC()

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Wrap(err, "failed to begin the stale quality suggestion job sweep")
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT `+qualitySuggestionJobColumns+` FROM quality_suggestion_job
		 WHERE status = 2 AND started_at < $1 FOR UPDATE`, cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read the stale quality suggestion jobs")
	}
	defer rows.Close()
	var stale []*QualitySuggestionJobMessage
	for rows.Next() {
		job, err := scanQualitySuggestionJob(rows)
		if err != nil {
			return 0, errors.Wrap(err, "failed to scan a stale quality suggestion job")
		}
		stale = append(stale, job)
	}
	if err := rows.Err(); err != nil {
		return 0, errors.Wrap(err, "failed to iterate the stale quality suggestion jobs")
	}
	// The updates below run on the same transaction connection, so the read must be
	// finished before the first write.
	if err := rows.Close(); err != nil {
		return 0, errors.Wrap(err, "failed to close the stale quality suggestion job rows")
	}

	var abandoned int64
	for _, job := range stale {
		for _, target := range job.Targets.GetTargets() {
			if target == nil {
				continue
			}
			switch target.GetState() {
			case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_RUNNING:
				target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED
				target.Error = "interrupted"
				target.FinishedAtUnixMs = qualitySuggestionJobTargetFinishedAt(now)
			case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
				storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_UNSPECIFIED:
				target.State = storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED
				target.FinishedAtUnixMs = qualitySuggestionJobTargetFinishedAt(now)
			case storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE,
				storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_FAILED,
				storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED:
				// The outcome is history.
			default:
				// Unknown states are left as stored.
			}
		}
		targetsRaw, err := marshalQualitySuggestionJobTargets(job.Targets)
		if err != nil {
			return 0, err
		}
		_, execErr := tx.ExecContext(ctx, `
			UPDATE quality_suggestion_job
			SET status = $2, targets = $3, error = $4, finished_at = $5
			WHERE id = $1
		`, job.ID, int32(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED), targetsRaw,
			"the server restarted before the job finished", now)
		if execErr != nil {
			return 0, errors.Wrap(execErr, "failed to abandon a stale quality suggestion job")
		}
		abandoned++
	}

	if err := tx.Commit(); err != nil {
		return 0, errors.Wrap(err, "failed to commit the stale quality suggestion job sweep")
	}
	return abandoned, nil
}

// DeleteExpiredQualitySuggestionJobs removes terminal jobs finished before cutoff.
// A pending or running job is never pruned: it is still the durable record of work.
func (s *Store) DeleteExpiredQualitySuggestionJobs(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, common.Errorf(common.Invalid, "a cutoff is required")
	}
	result, err := s.GetDB().ExecContext(ctx, deleteExpiredQualitySuggestionJobsQuery, int16(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED),
		int16(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED),
		int16(storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED),
		cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired quality suggestion jobs")
	}
	return result.RowsAffected()
}
