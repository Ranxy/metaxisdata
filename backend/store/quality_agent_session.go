package store

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentSessionMessage is one target's durable agent conversation and wake
// schedule. "Running" is expressed by LeaseUntil only; State is the durable
// lifecycle, and every human-visible mutation bumps Revision so a stale writer is
// rejected.
type QualityAgentSessionMessage struct {
	ID                int64
	ResourceID        string
	WorkspaceID       string
	TargetGUID        string
	TargetType        storepb.MetaType
	DatabaseRef       string
	AgentID           int64
	AgentPrincipalID  int32
	State             storepb.QualityAgentSessionState
	BlockedCode       string
	BlockedReason     string
	Revision          int32
	WakeCount         int32
	Summary           string
	SummaryThroughSeq int64
	ProcessedSeq      int64
	EngineState       *storepb.QualityAgentSessionEngineState
	ContextUsage      *storepb.QualityAgentSessionContextUsage
	NextWakeAt        *time.Time
	LeaseUntil        *time.Time
	// ManualResumeAt is set when a person resumed a blocked session. The next wake spends it to
	// bypass the hourly budget once, so "resume" cannot be silently undone by the budget it was
	// meant to override.
	ManualResumeAt *time.Time
	RestartCount   int32
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
	Mode           storepb.QualityAgentSessionMode
	// Kind is 1 for a target work thread and 2 for the agent's conversation.
	Kind int16
	// LastActivityAt is the newest message's created_at; set by the read queries that
	// join the transcript.
	LastActivityAt *time.Time
}

// QualityAgentTranscriptMessage is one append-only conversation entry of a session.
type QualityAgentTranscriptMessage struct {
	ID             int64
	SessionID      int64
	WorkspaceID    string
	Seq            int64
	Wake           int32
	Turn           int32
	Role           storepb.QualityAgentMessageRole
	Origin         storepb.QualityAgentMessageOrigin
	Content        string
	Reasoning      string
	ToolCalls      *storepb.QualityToolCallList
	ToolCallID     string
	ToolName       string
	Reference      string
	Tokens         int32
	Error          string
	Truncated      bool
	IdempotencyKey string
	CreatedAt      time.Time
}

const qualityAgentSessionColumns = `id, resource_id, workspace_id, target_guid, target_type,
	database_ref, agent_id,
	COALESCE((SELECT principal_id FROM quality_agent WHERE quality_agent.id = agent_id), 0) AS agent_principal_id,
	state, blocked_code, blocked_reason, revision, wake_count, summary,
	summary_through_seq, processed_seq, engine_state, next_wake_at, lease_until,
	restart_count, created_at, updated_at, archived_at, mode, kind, context_usage,
	manual_resume_at`

// qualityAgentSessionReadColumns adds the newest message timestamp the list and detail
// readers show as "last activity".
const qualityAgentSessionReadColumns = qualityAgentSessionColumns + `,
	(SELECT MAX(m.created_at) FROM quality_agent_message m WHERE m.session_id = quality_agent_session.id) AS last_activity_at`

const qualityAgentTranscriptColumns = `id, session_id, workspace_id, seq, wake, turn, role,
	origin, content, reasoning, tool_calls, tool_call_id, tool_name, reference, tokens, error,
	truncated, idempotency_key, created_at`

// CreateQualityAgentSessionInput is one session creation. A duplicate active session
// for the same target is reported as a conflict by the partial unique index.
type CreateQualityAgentSessionInput struct {
	WorkspaceID string
	TargetGUID  string
	TargetType  storepb.MetaType
	DatabaseRef string
	AgentID     int64
	Mode        storepb.QualityAgentSessionMode
	// Kind is 1 for a target work thread and 2 for the conversation. Zero means 1.
	Kind int16
	// NextWakeAt staggers admitted sessions so a batch does not all become due in the
	// same second. Nil means wake immediately.
	NextWakeAt *time.Time
}

// CreateQualityAgentSession records one enabled session that wakes immediately.
func (s *Store) CreateQualityAgentSession(ctx context.Context, input *CreateQualityAgentSessionInput) (*QualityAgentSessionMessage, error) {
	if input == nil || input.WorkspaceID == "" || input.AgentID <= 0 {
		return nil, common.Errorf(common.Invalid, "a workspace id and agent id are required")
	}
	kind := input.Kind
	if kind == 0 {
		kind = 1
	}
	if kind == 1 && input.TargetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target session needs a target guid")
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the session id")
	}
	mode := input.Mode
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_UNSPECIFIED {
		mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR
	}
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_session
			(resource_id, workspace_id, target_guid, target_type, database_ref, agent_id,
			 state, next_wake_at, mode, kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW()), $9, $10)
		RETURNING `+qualityAgentSessionColumns,
		resourceID, input.WorkspaceID, input.TargetGUID, int16(input.TargetType),
		input.DatabaseRef, input.AgentID,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED),
		input.NextWakeAt, int16(mode), kind))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "an active session already covers this target")
		}
		return nil, errors.Wrap(err, "failed to create the quality agent session")
	}
	return message, nil
}

// GetQualityAgentSession reads one session by resource id; nil means absent.
func (s *Store) GetQualityAgentSession(ctx context.Context, resourceID string) (*QualityAgentSessionMessage, error) {
	message, err := scanQualityAgentSessionRead(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentSessionReadColumns+`
		 FROM quality_agent_session WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent session")
	}
	return message, nil
}

// GetQualityAgentSessionByTarget reads the target's active session; nil means none.
func (s *Store) GetQualityAgentSessionByTarget(ctx context.Context, workspaceID string, agentID int64, targetGUID string) (*QualityAgentSessionMessage, error) {
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session
		 WHERE workspace_id = $1 AND agent_id = $2 AND target_guid = $3
			AND kind = 1 AND archived_at IS NULL`, workspaceID, agentID, targetGUID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the target's quality agent session")
	}
	return message, nil
}

// ListQualityAgentSessionsByTarget lists the target work threads covering one
// target. With overlapping agent scopes a target may have one session per agent;
// the failure-event intake appends the event to each of them.
func (s *Store) ListQualityAgentSessionsByTarget(ctx context.Context, workspaceID, targetGUID string) ([]*QualityAgentSessionMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session
		 WHERE workspace_id = $1 AND target_guid = $2 AND kind = 1 AND archived_at IS NULL
		 ORDER BY agent_id`, workspaceID, targetGUID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the sessions for a target")
	}
	defer rows.Close()
	var messages []*QualityAgentSessionMessage
	for rows.Next() {
		message, err := scanQualityAgentSession(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a target session")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the target sessions")
	}
	return messages, nil
}

// FindQualityAgentSessionMessage filters a session list; every field is optional.
type FindQualityAgentSessionMessage struct {
	WorkspaceID     *string
	AgentID         *int64
	Kind            int16
	TargetGUID      *string
	DatabaseRef     *string
	States          []storepb.QualityAgentSessionState
	Mode            storepb.QualityAgentSessionMode
	IncludeArchived bool
	Limit           *int
	Offset          *int
}

// ListQualityAgentSessions lists sessions by newest activity first.
func (s *Store) ListQualityAgentSessions(ctx context.Context, find *FindQualityAgentSessionMessage) ([]*QualityAgentSessionMessage, error) {
	if find == nil {
		find = &FindQualityAgentSessionMessage{}
	}
	where := "1 = 1"
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if find.WorkspaceID != nil && *find.WorkspaceID != "" {
		where += ` AND workspace_id = ` + arg(*find.WorkspaceID)
	}
	if find.AgentID != nil && *find.AgentID > 0 {
		where += ` AND agent_id = ` + arg(*find.AgentID)
	}
	if find.Kind != 0 {
		where += ` AND kind = ` + arg(find.Kind)
	}
	if find.TargetGUID != nil && *find.TargetGUID != "" {
		where += ` AND target_guid = ` + arg(*find.TargetGUID)
	}
	if find.DatabaseRef != nil && *find.DatabaseRef != "" {
		where += ` AND database_ref = ` + arg(*find.DatabaseRef)
	}
	if !find.IncludeArchived {
		where += ` AND archived_at IS NULL`
	}
	if len(find.States) > 0 {
		states := make([]int16, 0, len(find.States))
		for _, state := range find.States {
			states = append(states, int16(state))
		}
		where += ` AND state = ANY(` + arg(pq.Array(states)) + `::int2[])`
	}
	if find.Mode != storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_UNSPECIFIED {
		where += ` AND mode = ` + arg(int16(find.Mode))
	}
	limit, offset := 100, 0
	if find.Limit != nil && *find.Limit > 0 {
		limit = *find.Limit
	}
	if find.Offset != nil && *find.Offset > 0 {
		offset = *find.Offset
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentSessionReadColumns+`
		FROM quality_agent_session WHERE `+where+`
		ORDER BY updated_at DESC, id DESC LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent sessions")
	}
	defer rows.Close()
	var messages []*QualityAgentSessionMessage
	for rows.Next() {
		message, err := scanQualityAgentSessionRead(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent session")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent sessions")
	}
	return messages, nil
}

// retryableSessionBlockedCodes are blocked codes the runner may wake again on its
// own; anything else needs a person.
func retryableSessionBlockedCodes() []string {
	return []string{"token-budget", "wake-budget", "internal-error", "concurrency-limit"}
}

// QualityAgentSessionBlockedCodeInfrastructure reports whether a block is about the runtime rather
// than about a decision: a budget window, a transient internal error, a concurrency limit. The agent
// may not park a session for anything else — a decision belongs to a case, which the attention list
// tracks, while a parked session silenced every target it covered and no event could wake it.
func QualityAgentSessionBlockedCodeInfrastructure(code string) bool {
	return QualityAgentSessionBlockedCodeRetryable(code)
}

// QualityAgentSessionBlockedCodeRetryable reports whether a block is transient (a
// budget window, a transient internal error). The check runner uses it to keep
// appending failure events while the session is parked on such a block.
func QualityAgentSessionBlockedCodeRetryable(code string) bool {
	for _, candidate := range retryableSessionBlockedCodes() {
		if candidate == code {
			return true
		}
	}
	return false
}

// claimDueQualityAgentSessionsQuery is the one wake-claim predicate: an enabled (or
// retryably blocked) session whose wake is due and whose lease is free. Keeping the
// predicate in one constant lets the store test pin it.
const claimDueQualityAgentSessionsQuery = `
		UPDATE quality_agent_session s
		SET lease_until = $2, updated_at = NOW()
		WHERE s.id IN (
			SELECT id FROM quality_agent_session
			WHERE archived_at IS NULL
				AND (state = $3 OR (state = $5 AND blocked_code = ANY($6)))
				AND next_wake_at IS NOT NULL
				AND next_wake_at <= $1
				AND (lease_until IS NULL OR lease_until <= $1)
			ORDER BY next_wake_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $4
		)
		RETURNING ` + qualityAgentSessionColumns

// ClaimDueQualityAgentSessions leases up to limit runnable sessions. A session is
// runnable when it is enabled (or blocked on a retryable condition) and its wake is
// due; the lease, not a state, expresses "running".
func (s *Store) ClaimDueQualityAgentSessions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*QualityAgentSessionMessage, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.GetDB().QueryContext(ctx, claimDueQualityAgentSessionsQuery,
		now, now.Add(lease),
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED), limit,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
		pq.Array(retryableSessionBlockedCodes()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to claim the quality agent sessions")
	}
	defer rows.Close()
	var messages []*QualityAgentSessionMessage
	for rows.Next() {
		message, err := scanQualityAgentSession(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a claimed session")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the claimed sessions")
	}
	return messages, nil
}

// UpdateQualityAgentSessionState applies a durable state change with a revision
// compare-and-set. It clears the lease; the caller decides the next wakeup.
func (s *Store) UpdateQualityAgentSessionState(ctx context.Context, resourceID string, expectedRevision int32, state storepb.QualityAgentSessionState, blockedCode, blockedReason string, nextWakeAt *time.Time) (*QualityAgentSessionMessage, bool, error) {
	if state == storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED {
		blockedCode, blockedReason = "", ""
	}
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_session
		SET state = $2, blocked_code = $3, blocked_reason = $4, next_wake_at = $5,
			restart_count = CASE WHEN $2::int2 = $7::int2 THEN 0 ELSE restart_count END,
			lease_until = NULL, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $6 AND archived_at IS NULL
		RETURNING `+qualityAgentSessionColumns,
		resourceID, int16(state), blockedCode, blockedReason, nextWakeAt, expectedRevision,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to update the quality agent session")
	}
	// The session runs again, so the notice about its block is stale. Closing it here covers every
	// path that clears a block — a person's resume, the budget window reopening, an enrollment
	// enabling a dormant session — because they all go through this one write.
	if state == storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED {
		if closeErr := s.resolveSessionBlockedAttentions(ctx, message.ID, time.Now().UTC()); closeErr != nil {
			slog.Warn("failed to close a resumed session's block notice",
				slog.String("session", resourceID), log.WithError(closeErr))
		}
	}
	return message, true, nil
}

// SessionBlockedAttentionDedupeKey is the key of the notice about one blocked session. The session
// and the code are both in it: a session blocked for a different reason is a different question.
func SessionBlockedAttentionDedupeKey(sessionResourceID, code string) string {
	return "blocked:" + sessionResourceID + ":" + code
}

// resolveSessionBlockedAttentions closes the block notices of one session. A case card hangs off the
// same session but its dedupe key is the target's, so only the block notices are touched.
func (s *Store) resolveSessionBlockedAttentions(ctx context.Context, sessionID int64, now time.Time) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_attention
		SET status = $2, resolved_at = $3
		WHERE session_id = $1 AND status = $4 AND dedupe_key LIKE 'blocked:%'`,
		sessionID, int16(QualityAgentAttentionStatusResolved), now,
		int16(QualityAgentAttentionStatusOpen))
	if err != nil {
		return errors.Wrap(err, "failed to resolve the session's block notices")
	}
	return nil
}

// MarkQualityAgentSessionManualResume records that a person resumed a blocked session. The next
// budget check spends the marker to allow one wake; it is not a permanent exemption.
func (s *Store) MarkQualityAgentSessionManualResume(ctx context.Context, resourceID string, at time.Time) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session SET manual_resume_at = $2, updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL`, resourceID, at.UTC()); err != nil {
		return errors.Wrap(err, "failed to record a manual resume")
	}
	return nil
}

// ClearQualityAgentSessionManualResume consumes the marker once the wake it bought has run.
func (s *Store) ClearQualityAgentSessionManualResume(ctx context.Context, resourceID string) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session SET manual_resume_at = NULL, updated_at = NOW()
		WHERE resource_id = $1 AND manual_resume_at IS NOT NULL`, resourceID); err != nil {
		return errors.Wrap(err, "failed to clear a manual resume")
	}
	return nil
}

// QualityAgentSessionWakeResult is what one wake persists on the session row.
type QualityAgentSessionWakeResult struct {
	WakeCountIncrement int32
	ProcessedSeq       int64
	EngineState        *storepb.QualityAgentSessionEngineState
}

// UpdateQualityAgentSessionProgress records what one wake produced. It only applies
// while the session is not archived, so a concurrent archive wins.
func (s *Store) UpdateQualityAgentSessionProgress(ctx context.Context, resourceID string, result *QualityAgentSessionWakeResult) (*QualityAgentSessionMessage, error) {
	if result == nil {
		return nil, common.Errorf(common.Invalid, "a wake result is required")
	}
	state := result.EngineState
	if state == nil {
		state = &storepb.QualityAgentSessionEngineState{}
	}
	payload, err := protojson.Marshal(state)
	if err != nil {
		return nil, err
	}
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_session
		SET wake_count = wake_count + $2,
			processed_seq = GREATEST(processed_seq, $3), engine_state = $4,
			restart_count = 0, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL
		RETURNING `+qualityAgentSessionColumns,
		resourceID, result.WakeCountIncrement, result.ProcessedSeq, payload))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.Conflict, "the session is no longer active")
		}
		return nil, errors.Wrap(err, "failed to update the session progress")
	}
	return message, nil
}

// updateQualityAgentSessionContextUsageQuery pins the observability contract: a
// sample never bumps revision or updated_at and never writes an archived session.
const updateQualityAgentSessionContextUsageQuery = `
	UPDATE quality_agent_session
	SET context_usage = $2
	WHERE resource_id = $1 AND archived_at IS NULL`

// UpdateQualityAgentSessionContextUsage records the engine's latest model-input
// sample. It is observability only: it neither bumps revision nor updated_at, because
// a sample must not interfere with the human state CAS or the list's activity order.
// It reports false when the session is archived.
func (s *Store) UpdateQualityAgentSessionContextUsage(ctx context.Context, resourceID string, usage *storepb.QualityAgentSessionContextUsage) (bool, error) {
	if usage == nil {
		return false, common.Errorf(common.Invalid, "a context usage sample is required")
	}
	payload, err := protojson.Marshal(usage)
	if err != nil {
		return false, err
	}
	result, err := s.GetDB().ExecContext(ctx, updateQualityAgentSessionContextUsageQuery, resourceID, payload)
	if err != nil {
		return false, errors.Wrap(err, "failed to update the session context usage")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the updated session")
	}
	return affected > 0, nil
}

// UpdateQualityAgentSessionCompaction stores the rolling checkpoint summary that
// replaces messages up to throughSeq in the next model context.
func (s *Store) UpdateQualityAgentSessionCompaction(ctx context.Context, resourceID, summary string, throughSeq int64) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session
		SET summary = $2, summary_through_seq = GREATEST(summary_through_seq, $3),
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL`, resourceID, summary, throughSeq)
	if err != nil {
		return false, errors.Wrap(err, "failed to store the session summary")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the updated session")
	}
	return affected > 0, nil
}

// ReleaseQualityAgentSessionLease clears the lease and schedules the next wakeup. It
// reports false when the session is not leased any more (for example it was archived).
func (s *Store) ReleaseQualityAgentSessionLease(ctx context.Context, resourceID string, nextWakeAt *time.Time) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session
		SET lease_until = NULL, next_wake_at = $2, updated_at = NOW()
		WHERE resource_id = $1 AND lease_until IS NOT NULL`,
		resourceID, nextWakeAt)
	if err != nil {
		return false, errors.Wrap(err, "failed to release the session lease")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the released session")
	}
	return affected > 0, nil
}

// ResetStaleQualityAgentSessionLeases clears leases left by a previous process and
// counts the restart. Sessions over the restart limit are parked for a person.
func (s *Store) ResetStaleQualityAgentSessionLeases(ctx context.Context, now time.Time, restartLimit int32) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session
		SET lease_until = NULL,
			restart_count = restart_count + 1,
			state = CASE WHEN restart_count + 1 > $2 THEN $3 ELSE state END,
			blocked_code = CASE WHEN restart_count + 1 > $2 THEN 'restart-limit' ELSE blocked_code END,
			blocked_reason = CASE WHEN restart_count + 1 > $2
				THEN 'the process restarted while this session was running too many times'
				ELSE blocked_reason END,
			next_wake_at = CASE WHEN restart_count + 1 > $2 THEN NULL ELSE next_wake_at END,
			revision = revision + 1,
			updated_at = NOW()
		WHERE archived_at IS NULL AND lease_until IS NOT NULL AND lease_until <= $1`,
		now, restartLimit,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED))
	if err != nil {
		return 0, errors.Wrap(err, "failed to reset the stale session leases")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the reset session leases")
	}
	return affected, nil
}

// AppendQualityAgentTranscriptMessage appends one transcript entry. Appends to one
// session are serialized with the session row lock, so the runner and an API/event
// writer can race without allocating the same seq or losing a row to a unique
// violation. A retried idempotency key returns the existing row.
func (s *Store) AppendQualityAgentTranscriptMessage(ctx context.Context, input *QualityAgentTranscriptMessage) (*QualityAgentTranscriptMessage, error) {
	if input == nil || input.SessionID == 0 {
		return nil, common.Errorf(common.Invalid, "a session id is required")
	}
	if input.ToolCalls == nil {
		input.ToolCalls = &storepb.QualityToolCallList{}
	}
	toolCalls, err := protojson.Marshal(input.ToolCalls)
	if err != nil {
		return nil, err
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin the session message transaction")
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize every writer of this session: the seq allocation below is MAX+1, and an
	// external writer (the API's wake or an event from the check runner) does not hold
	// the runner's lease.
	var sessionID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM quality_agent_session WHERE id = $1 FOR UPDATE`,
		input.SessionID).Scan(&sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "the session no longer exists")
		}
		return nil, errors.Wrap(err, "failed to lock the session")
	}
	if input.IdempotencyKey != "" {
		existing, findErr := scanQualityAgentTranscript(tx.QueryRowContext(ctx,
			`SELECT `+qualityAgentTranscriptColumns+` FROM quality_agent_message
			 WHERE session_id = $1 AND idempotency_key = $2`, input.SessionID, input.IdempotencyKey))
		if findErr == nil {
			return existing, nil
		}
		if !errors.Is(findErr, sql.ErrNoRows) {
			return nil, errors.Wrap(findErr, "failed to read the existing session message")
		}
	}
	message, err := scanQualityAgentTranscript(tx.QueryRowContext(ctx, `
		INSERT INTO quality_agent_message
			(session_id, workspace_id, seq, wake, turn, role, origin, content, reasoning,
			 tool_calls, tool_call_id, tool_name, reference, tokens, error, truncated, idempotency_key)
		VALUES ($1, $2,
			(SELECT COALESCE(MAX(seq), 0) + 1 FROM quality_agent_message WHERE session_id = $1),
			$3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING `+qualityAgentTranscriptColumns,
		input.SessionID, input.WorkspaceID, input.Wake, input.Turn, int16(input.Role),
		int16(input.Origin), input.Content, input.Reasoning, toolCalls, input.ToolCallID,
		input.ToolName, input.Reference, input.Tokens, input.Error, input.Truncated,
		input.IdempotencyKey))
	if err != nil {
		return nil, errors.Wrap(err, "failed to append the session message")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit the session message")
	}
	return message, nil
}

// MaxQualityAgentInputSeqAfter is the highest seq of an external input (event or
// person's question) after the given watermark. The runner compares it with the wake's
// consumed watermark to decide whether another wake is immediately due; transcript rows
// the engine writes (assistant, tool, engine) must never count.
func (s *Store) MaxQualityAgentInputSeqAfter(ctx context.Context, sessionID int64, afterSeq int64) (int64, error) {
	var seq int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM quality_agent_message
		WHERE session_id = $1 AND seq > $2 AND role = $3 AND origin IN ($4, $5)`,
		sessionID, afterSeq,
		int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK)).Scan(&seq); err != nil {
		return 0, errors.Wrap(err, "failed to read the pending session inputs")
	}
	return seq, nil
}

// FindQualityAgentTranscriptMessage filters a transcript read.
type FindQualityAgentTranscriptMessage struct {
	SessionID int64
	// AfterSeq returns messages with seq greater than this; the polling watermark.
	AfterSeq int64
	// BeforeSeq returns messages with seq less than this; the stable older-page
	// anchor. It takes precedence over AfterSeq when both are set.
	BeforeSeq int64
	// ExcludeEngine drops engine notes, which are UI/runner bookkeeping and never part
	// of the model context.
	ExcludeEngine bool
	// Role, when set, keeps only messages of that role.
	Role *storepb.QualityAgentMessageRole
	// InputsOnly keeps only external inputs (a user message whose origin is an event or
	// a person's question). It is how the wake watermark and the pending list are read.
	InputsOnly bool
	// FrontOfficeOnly keeps the owner conversation product rows: an owner question
	// (user + ASK) or an assistant answer. It is applied in SQL so pagination and the
	// after_seq watermark count only visible rows.
	FrontOfficeOnly bool
	Limit           int
	Offset          int
	// NewestFirst returns the most recent page first; the caller reverses it when it
	// needs chronological order.
	NewestFirst bool
}

// ListQualityAgentTranscriptMessages lists one session's transcript.
func (s *Store) ListQualityAgentTranscriptMessages(ctx context.Context, find *FindQualityAgentTranscriptMessage) ([]*QualityAgentTranscriptMessage, error) {
	if find == nil || find.SessionID == 0 {
		return nil, common.Errorf(common.Invalid, "a session id is required")
	}
	where := "session_id = $1"
	args := []any{find.SessionID}
	if find.BeforeSeq > 0 {
		args = append(args, find.BeforeSeq)
		where += " AND seq < $2"
	} else {
		args = append(args, find.AfterSeq)
		where += " AND seq > $2"
	}
	if find.ExcludeEngine {
		args = append(args, int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE))
		where += ` AND role <> $` + strconv.Itoa(len(args))
	}
	if find.Role != nil {
		args = append(args, int16(*find.Role))
		where += ` AND role = $` + strconv.Itoa(len(args))
	}
	if find.InputsOnly {
		args = append(args, int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER))
		args = append(args, int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT))
		args = append(args, int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK))
		where += ` AND role = $` + strconv.Itoa(len(args)-2) + ` AND origin IN ($` +
			strconv.Itoa(len(args)-1) + `, $` + strconv.Itoa(len(args)) + `)`
	}
	if find.FrontOfficeOnly {
		args = append(args, int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER))
		args = append(args, int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK))
		args = append(args, int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT))
		where += ` AND ((role = $` + strconv.Itoa(len(args)-2) + ` AND origin = $` +
			strconv.Itoa(len(args)-1) + `) OR role = $` + strconv.Itoa(len(args)) + `)`
	}
	limit := find.Limit
	if limit <= 0 {
		limit = 200
	}
	order := "seq ASC"
	if find.NewestFirst {
		order = "seq DESC"
	}
	args = append(args, limit)
	limitArg := "$" + strconv.Itoa(len(args))
	offset := ""
	if find.Offset > 0 {
		args = append(args, find.Offset)
		offset = " OFFSET $" + strconv.Itoa(len(args))
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentTranscriptColumns+`
		FROM quality_agent_message WHERE `+where+`
		ORDER BY `+order+` LIMIT `+limitArg+offset, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the session messages")
	}
	defer rows.Close()
	var messages []*QualityAgentTranscriptMessage
	for rows.Next() {
		message, err := scanQualityAgentTranscript(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a session message")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the session messages")
	}
	return messages, nil
}

// SumQualityAgentSessionTokensSince is the workspace's agent token spend since one
// instant, the hourly budget the runner enforces.
func (s *Store) SumQualityAgentSessionTokensSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(m.tokens), 0)
		FROM quality_agent_message m
		JOIN quality_agent_session s ON s.id = m.session_id
		WHERE m.workspace_id = $1 AND s.agent_id = $2 AND m.created_at > $3`,
		workspaceID, agentID, since).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to sum the session tokens")
	}
	return total, nil
}

// CountQualityAgentSessionWakesSince counts the wakes one agent's target sessions performed since
// an instant: the wake budget's read.
//
// Only a wake counts. An event message is not a wake — a dozen events are folded into one wake, and
// they have their own per-session ceiling (CountQualityAgentSessionEventsSince). Counting each event
// here let a burst of perception spend the capacity to think, which parked the session exactly when
// it had something to act on.
func (s *Store) CountQualityAgentSessionWakesSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM quality_agent_message m
		JOIN quality_agent_session s ON s.id = m.session_id
		WHERE m.workspace_id = $1 AND s.agent_id = $2 AND m.created_at > $3
			AND s.kind = 1
			AND m.origin = $4`, workspaceID, agentID, since,
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE)).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to count the session wakes")
	}
	return total, nil
}

// scanQualityAgentSession reads one quality_agent_session row without the joined
// last-activity column.
func scanQualityAgentSession(scanner interface{ Scan(dest ...any) error }) (*QualityAgentSessionMessage, error) {
	return scanQualityAgentSessionFields(scanner, false)
}

// scanQualityAgentSessionRead reads one session row with its joined last-activity.
func scanQualityAgentSessionRead(scanner interface{ Scan(dest ...any) error }) (*QualityAgentSessionMessage, error) {
	return scanQualityAgentSessionFields(scanner, true)
}

func scanQualityAgentSessionFields(scanner interface{ Scan(dest ...any) error }, withLastActivity bool) (*QualityAgentSessionMessage, error) {
	var (
		message      QualityAgentSessionMessage
		targetType   int16
		state        int16
		engineState  []byte
		contextUsage []byte
		agentID      int64
		agentPrinID  int32
		mode         int16
		kind         int16
	)
	dest := []any{
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.TargetGUID,
		&targetType, &message.DatabaseRef, &agentID, &agentPrinID,
		&state, &message.BlockedCode, &message.BlockedReason, &message.Revision,
		&message.WakeCount, &message.Summary,
		&message.SummaryThroughSeq, &message.ProcessedSeq, &engineState,
		&message.NextWakeAt, &message.LeaseUntil, &message.RestartCount,
		&message.CreatedAt, &message.UpdatedAt, &message.ArchivedAt,
		&mode, &kind, &contextUsage, &message.ManualResumeAt,
	}
	if withLastActivity {
		dest = append(dest, &message.LastActivityAt)
	}
	if err := scanner.Scan(dest...); err != nil {
		return nil, err
	}
	message.TargetType = storepb.MetaType(targetType)
	message.State = storepb.QualityAgentSessionState(state)
	message.AgentID = agentID
	message.AgentPrincipalID = agentPrinID
	message.Kind = kind
	message.Mode = storepb.QualityAgentSessionMode(mode)
	engineStateMessage := &storepb.QualityAgentSessionEngineState{}
	if len(engineState) > 0 && string(engineState) != "{}" {
		if err := common.ProtojsonUnmarshaler.Unmarshal(engineState, engineStateMessage); err != nil {
			return nil, errors.Wrap(err, "failed to decode the session engine state")
		}
	}
	message.EngineState = engineStateMessage
	if len(contextUsage) > 0 && string(contextUsage) != "{}" {
		usage := &storepb.QualityAgentSessionContextUsage{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(contextUsage, usage); err != nil {
			return nil, errors.Wrap(err, "failed to decode the session context usage")
		}
		message.ContextUsage = usage
	}
	return &message, nil
}

// scanQualityAgentMessage reads one quality_agent_message row.
func scanQualityAgentTranscript(scanner interface{ Scan(dest ...any) error }) (*QualityAgentTranscriptMessage, error) {
	var (
		message   QualityAgentTranscriptMessage
		role      int16
		origin    int16
		toolCalls []byte
	)
	if err := scanner.Scan(
		&message.ID, &message.SessionID, &message.WorkspaceID, &message.Seq, &message.Wake,
		&message.Turn, &role, &origin, &message.Content, &message.Reasoning, &toolCalls,
		&message.ToolCallID, &message.ToolName, &message.Reference, &message.Tokens,
		&message.Error,
		&message.Truncated, &message.IdempotencyKey, &message.CreatedAt,
	); err != nil {
		return nil, err
	}
	message.Role = storepb.QualityAgentMessageRole(role)
	message.Origin = storepb.QualityAgentMessageOrigin(origin)
	toolCallList := &storepb.QualityToolCallList{}
	if len(toolCalls) > 0 && string(toolCalls) != "{}" {
		if err := common.ProtojsonUnmarshaler.Unmarshal(toolCalls, toolCallList); err != nil {
			return nil, errors.Wrap(err, "failed to decode the message tool calls")
		}
	}
	message.ToolCalls = toolCallList
	return &message, nil
}

// UpdateQualityAgentSystemMessage replaces the stored system prompt for one
// session. The language contract is rendered at wake time, so an agent language
// change updates the existing row instead of appending a second system message.
func (s *Store) UpdateQualityAgentSystemMessage(ctx context.Context, sessionID int64, content string) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_message
		SET content = $2
		WHERE session_id = $1 AND role = $3`,
		sessionID, content, int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM))
	if err != nil {
		return false, errors.Wrap(err, "failed to update the session system message")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to read the updated system message count")
	}
	return affected > 0, nil
}

// AdvanceQualityAgentSessionWake makes an enabled session due now without touching its
// lease (a wake already running keeps it) or its state. An event or a person's message
// uses it to request an immediate wake.
func (s *Store) AdvanceQualityAgentSessionWake(ctx context.Context, resourceID string, now time.Time) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session
		SET next_wake_at = LEAST(COALESCE(next_wake_at, $2), $2), updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL AND state = $3`,
		resourceID, now,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED))
	if err != nil {
		return false, errors.Wrap(err, "failed to advance the session wake")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the advanced session")
	}
	return affected > 0, nil
}

// CountQualityAgentSessionEventsSince counts the events appended to one session since
// one instant, the per-session event cap that keeps a storm of failing checks from
// flooding one conversation.
func (s *Store) CountQualityAgentSessionEventsSince(ctx context.Context, sessionID int64, since time.Time) (int64, error) {
	var count int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_message
		WHERE session_id = $1 AND created_at > $2 AND origin = $3`,
		sessionID, since,
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT)).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the session events")
	}
	return count, nil
}

// CountQualityAgentSessionsCreatedSince counts the target sessions an agent created
// since a time. The enrollment admission path uses it to bound new sessions per hour.
func (s *Store) CountQualityAgentSessionsCreatedSince(ctx context.Context, workspaceID string, agentID int64, since time.Time) (int, error) {
	var count int
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_session
		WHERE workspace_id = $1 AND agent_id = $2 AND kind = 1 AND created_at > $3`,
		workspaceID, agentID, since).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the sessions created since")
	}
	return count, nil
}

// UpdateQualityAgentSessionMode switches a session between monitoring and triage. It
// does not touch the wake schedule; the caller decides whether the change is due now.
func (s *Store) UpdateQualityAgentSessionMode(ctx context.Context, resourceID string, mode storepb.QualityAgentSessionMode) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session
		SET mode = $2, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL`, resourceID, int16(mode))
	if err != nil {
		return false, errors.Wrap(err, "failed to update the session mode")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the updated session mode")
	}
	return affected > 0, nil
}

// EnsureQualityAgentConversation returns the agent's one front-office conversation,
// creating it on first use. The partial unique index makes concurrent creates
// converge on one row.
func (s *Store) EnsureQualityAgentConversation(ctx context.Context, workspaceID string, agentID int64) (*QualityAgentSessionMessage, error) {
	if workspaceID == "" || agentID <= 0 {
		return nil, common.Errorf(common.Invalid, "a workspace id and agent id are required")
	}
	if existing, err := s.GetQualityAgentConversation(ctx, agentID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the conversation id")
	}
	_, err = s.GetDB().ExecContext(ctx, `
		INSERT INTO quality_agent_session
			(resource_id, workspace_id, target_guid, target_type, database_ref, agent_id,
			 state, next_wake_at, mode, kind)
		VALUES ($1, $2, '', 0, '', $3, $4, NULL, $5, 2)
		ON CONFLICT (agent_id) WHERE archived_at IS NULL AND kind = 2 DO NOTHING`,
		resourceID, workspaceID, agentID,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED),
		int16(storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_UNSPECIFIED))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the agent conversation")
	}
	return s.GetQualityAgentConversation(ctx, agentID)
}

// GetQualityAgentConversation reads the agent's front-office conversation; nil when
// it has not been created yet.
func (s *Store) GetQualityAgentConversation(ctx context.Context, agentID int64) (*QualityAgentSessionMessage, error) {
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session
		 WHERE agent_id = $1 AND kind = 2 AND archived_at IS NULL
		 ORDER BY id DESC LIMIT 1`, agentID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the agent conversation")
	}
	return message, nil
}

// CountQualityAgentConversationMessagesSince counts the owner's questions in one
// agent's conversation since an instant, the per-agent message budget.
func (s *Store) CountQualityAgentConversationMessagesSince(ctx context.Context, agentID int64, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM quality_agent_message m
		JOIN quality_agent_session s ON s.id = m.session_id
		WHERE s.agent_id = $1 AND s.kind = 2 AND m.created_at > $2
			AND m.role = $3 AND m.origin = $4`,
		agentID, since,
		int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK)).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to count the agent conversation messages")
	}
	return total, nil
}

// ListQualityAgentSessionsByIDs reads sessions by internal id for a batch of
// readers, so an attention list can render each target without a query per row.
func (s *Store) ListQualityAgentSessionsByIDs(ctx context.Context, workspaceID string, ids []int64) (map[int64]*QualityAgentSessionMessage, error) {
	sessions := map[int64]*QualityAgentSessionMessage{}
	if len(ids) == 0 {
		return sessions, nil
	}
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session
		 WHERE workspace_id = $1 AND id = ANY($2)`,
		workspaceID, pq.Array(ids))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent sessions by id")
	}
	defer rows.Close()
	for rows.Next() {
		session, err := scanQualityAgentSession(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent session")
		}
		sessions[session.ID] = session
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent sessions")
	}
	return sessions, nil
}

// GetQualityAgentSessionByID reads one session by row id; nil means absent.
func (s *Store) GetQualityAgentSessionByID(ctx context.Context, id int64) (*QualityAgentSessionMessage, error) {
	if id <= 0 {
		return nil, nil
	}
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent session")
	}
	return message, nil
}

// ListQualityAgentSessionsByBlockedCode reads sessions parked with one code, used
// by the runner to escalate restart-limit sessions that the lease sweep parked.
func (s *Store) ListQualityAgentSessionsByBlockedCode(ctx context.Context, code string, limit int) ([]*QualityAgentSessionMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.GetDB().QueryContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session
		 WHERE archived_at IS NULL AND blocked_code = $1
		 ORDER BY updated_at DESC LIMIT $2`, code, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the blocked quality agent sessions")
	}
	defer rows.Close()
	var sessions []*QualityAgentSessionMessage
	for rows.Next() {
		session, err := scanQualityAgentSession(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a blocked quality agent session")
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the blocked quality agent sessions")
	}
	return sessions, nil
}
