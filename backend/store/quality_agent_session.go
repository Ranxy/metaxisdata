package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
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
	Objective         string
	State             storepb.QualityAgentSessionState
	BlockedCode       string
	BlockedReason     string
	Revision          int32
	WakeCount         int32
	TokensUsed        int64
	ContextTokens     int32
	Summary           string
	SummaryThroughSeq int64
	ProcessedSeq      int64
	EngineState       *storepb.QualityAgentSessionEngineState
	NextWakeAt        *time.Time
	LeaseUntil        *time.Time
	RestartCount      int32
	CreatedBy         *int32
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	// LastActivityAt is the newest message's created_at; set by the read queries that
	// join the transcript.
	LastActivityAt *time.Time
}

// QualityAgentTranscriptMessage is one append-only conversation entry of a session.
type QualityAgentTranscriptMessage struct {
	ID               int64
	SessionID        int64
	WorkspaceID      string
	Seq              int64
	Wake             int32
	Turn             int32
	Role             storepb.QualityAgentMessageRole
	Origin           storepb.QualityAgentMessageOrigin
	Content          string
	ToolCalls        *storepb.QualityToolCallList
	ToolCallID       string
	ToolName         string
	Reference        string
	Tokens           int32
	PromptTokens     int32
	CompletionTokens int32
	Error            string
	Truncated        bool
	IdempotencyKey   string
	CreatedAt        time.Time
}

const qualityAgentSessionColumns = `id, resource_id, workspace_id, target_guid, target_type,
	database_ref, agent_id,
	COALESCE((SELECT principal_id FROM quality_agent WHERE quality_agent.id = agent_id), 0) AS agent_principal_id,
	objective, state, blocked_code, blocked_reason, revision, wake_count, tokens_used,
	context_tokens, summary, summary_through_seq, processed_seq, engine_state,
	next_wake_at, lease_until, restart_count, created_by, created_at, updated_at, archived_at`

// qualityAgentSessionReadColumns adds the newest message timestamp the list and detail
// readers show as "last activity".
const qualityAgentSessionReadColumns = qualityAgentSessionColumns + `,
	(SELECT MAX(m.created_at) FROM quality_agent_message m WHERE m.session_id = quality_agent_session.id) AS last_activity_at`

const qualityAgentTranscriptColumns = `id, session_id, workspace_id, seq, wake, turn, role,
	origin, content, tool_calls, tool_call_id, tool_name, reference, tokens, prompt_tokens,
	completion_tokens, error, truncated, idempotency_key, created_at`

// CreateQualityAgentSessionInput is one session creation. A duplicate active session
// for the same target is reported as a conflict by the partial unique index.
type CreateQualityAgentSessionInput struct {
	WorkspaceID string
	TargetGUID  string
	TargetType  storepb.MetaType
	DatabaseRef string
	AgentID     int64
	Objective   string
	CreatedBy   *int32
}

// CreateQualityAgentSession records one enabled session that wakes immediately.
func (s *Store) CreateQualityAgentSession(ctx context.Context, input *CreateQualityAgentSessionInput) (*QualityAgentSessionMessage, error) {
	if input == nil || input.WorkspaceID == "" || input.TargetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a workspace id and target guid are required")
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the session id")
	}
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_session
			(resource_id, workspace_id, target_guid, target_type, database_ref, agent_id,
			 objective, state, next_wake_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), $9)
		RETURNING `+qualityAgentSessionColumns,
		resourceID, input.WorkspaceID, input.TargetGUID, int16(input.TargetType),
		input.DatabaseRef, input.AgentID, input.Objective,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED),
		input.CreatedBy))
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
func (s *Store) GetQualityAgentSessionByTarget(ctx context.Context, workspaceID, targetGUID string) (*QualityAgentSessionMessage, error) {
	message, err := scanQualityAgentSession(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentSessionColumns+` FROM quality_agent_session
		 WHERE workspace_id = $1 AND target_guid = $2 AND archived_at IS NULL`, workspaceID, targetGUID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the target's quality agent session")
	}
	return message, nil
}

// FindQualityAgentSessionMessage filters a session list; every field is optional.
type FindQualityAgentSessionMessage struct {
	WorkspaceID     *string
	TargetGUID      *string
	DatabaseRef     *string
	States          []storepb.QualityAgentSessionState
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
	limit, offset := 100, 0
	if find.Limit != nil && *find.Limit > 0 {
		limit = *find.Limit
	}
	if find.Offset != nil && *find.Offset > 0 {
		offset = *find.Offset
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentSessionReadColumns+`
		FROM quality_agent_session WHERE `+where+`
		ORDER BY COALESCE((SELECT MAX(m.created_at) FROM quality_agent_message m WHERE m.session_id = quality_agent_session.id), updated_at) DESC,
			id DESC LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
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
	return []string{"token-budget", "wake-budget", "autonomy-off", "llm-quota", "no-work", "internal-error"}
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
			lease_until = NULL, revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $6 AND archived_at IS NULL
		RETURNING `+qualityAgentSessionColumns,
		resourceID, int16(state), blockedCode, blockedReason, nextWakeAt, expectedRevision))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to update the quality agent session")
	}
	return message, true, nil
}

// ArchiveQualityAgentSession tombstones a session; the transcript stays.
func (s *Store) ArchiveQualityAgentSession(ctx context.Context, resourceID string, expectedRevision int32, now time.Time) (bool, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_session
		SET state = $2, archived_at = $3, next_wake_at = NULL, lease_until = NULL,
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND revision = $4 AND archived_at IS NULL`,
		resourceID,
		int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED),
		now, expectedRevision)
	if err != nil {
		return false, errors.Wrap(err, "failed to archive the quality agent session")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the archived session")
	}
	return affected > 0, nil
}

// QualityAgentSessionWakeResult is what one wake persists on the session row.
type QualityAgentSessionWakeResult struct {
	WakeCountIncrement int32
	TokensUsed         int64
	ContextTokens      int32
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
		SET wake_count = wake_count + $2, tokens_used = tokens_used + $3,
			context_tokens = GREATEST(context_tokens, $4),
			processed_seq = GREATEST(processed_seq, $5), engine_state = $6,
			revision = revision + 1, updated_at = NOW()
		WHERE resource_id = $1 AND archived_at IS NULL
		RETURNING `+qualityAgentSessionColumns,
		resourceID, result.WakeCountIncrement, result.TokensUsed, result.ContextTokens,
		result.ProcessedSeq, payload))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.Conflict, "the session is no longer active")
		}
		return nil, errors.Wrap(err, "failed to update the session progress")
	}
	return message, nil
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

// AppendQualityAgentTranscriptMessage appends one transcript entry, allocating the next sequence
// number. A retried idempotency key returns the existing row instead of a duplicate.
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
	message, err := scanQualityAgentTranscript(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_message
			(session_id, workspace_id, seq, wake, turn, role, origin, content, tool_calls,
			 tool_call_id, tool_name, reference, tokens, prompt_tokens, completion_tokens,
			 error, truncated, idempotency_key)
		VALUES ($1, $2,
			(SELECT COALESCE(MAX(seq), 0) + 1 FROM quality_agent_message WHERE session_id = $1),
			$3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING `+qualityAgentTranscriptColumns,
		input.SessionID, input.WorkspaceID, input.Wake, input.Turn, int16(input.Role),
		int16(input.Origin), input.Content, toolCalls, input.ToolCallID, input.ToolName,
		input.Reference, input.Tokens, input.PromptTokens, input.CompletionTokens,
		input.Error, input.Truncated, input.IdempotencyKey))
	if err != nil {
		if isUniqueViolation(err) && input.IdempotencyKey != "" {
			existing, findErr := scanQualityAgentTranscript(s.GetDB().QueryRowContext(ctx,
				`SELECT `+qualityAgentTranscriptColumns+` FROM quality_agent_message
				 WHERE session_id = $1 AND idempotency_key = $2`, input.SessionID, input.IdempotencyKey))
			if findErr != nil {
				return nil, errors.Wrap(findErr, "failed to read the existing session message")
			}
			return existing, nil
		}
		return nil, errors.Wrap(err, "failed to append the session message")
	}
	return message, nil
}

// FindQualityAgentTranscriptMessage filters a transcript read.
type FindQualityAgentTranscriptMessage struct {
	SessionID int64
	// AfterSeq returns messages with seq greater than this; the polling watermark.
	AfterSeq int64
	// ExcludeEngine drops engine notes, which are UI/runner bookkeeping and never part
	// of the model context.
	ExcludeEngine bool
	Limit         int
	Offset        int
	// NewestFirst returns the most recent page first; the caller reverses it when it
	// needs chronological order.
	NewestFirst bool
}

// ListQualityAgentTranscriptMessages lists one session's transcript.
func (s *Store) ListQualityAgentTranscriptMessages(ctx context.Context, find *FindQualityAgentTranscriptMessage) ([]*QualityAgentTranscriptMessage, error) {
	if find == nil || find.SessionID == 0 {
		return nil, common.Errorf(common.Invalid, "a session id is required")
	}
	where := "session_id = $1 AND seq > $2"
	args := []any{find.SessionID, find.AfterSeq}
	if find.ExcludeEngine {
		args = append(args, int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE))
		where += ` AND role <> $` + strconv.Itoa(len(args))
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

// MaxQualityAgentTranscriptSeq is the highest message seq of one session.
func (s *Store) MaxQualityAgentTranscriptSeq(ctx context.Context, sessionID int64) (int64, error) {
	var seq int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM quality_agent_message WHERE session_id = $1`,
		sessionID).Scan(&seq); err != nil {
		return 0, errors.Wrap(err, "failed to read the session message seq")
	}
	return seq, nil
}

// SumQualityAgentSessionTokensSince is the workspace's agent token spend since one
// instant, the hourly budget the runner enforces.
func (s *Store) SumQualityAgentSessionTokensSince(ctx context.Context, workspaceID string, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(SUM(tokens), 0) FROM quality_agent_message
		WHERE workspace_id = $1 AND created_at > $2`, workspaceID, since).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to sum the session tokens")
	}
	return total, nil
}

// CountQualityAgentSessionWakesSince counts the wakes a workspace started since one
// instant, the hourly wake budget.
func (s *Store) CountQualityAgentSessionWakesSince(ctx context.Context, workspaceID string, since time.Time) (int64, error) {
	var total int64
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_message
		WHERE workspace_id = $1 AND created_at > $2 AND role = $3
			AND origin IN ($4, $5, $6)`, workspaceID, since,
		int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT),
		int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK)).Scan(&total); err != nil {
		return 0, errors.Wrap(err, "failed to count the session wakes")
	}
	return total, nil
}

// QualityAgentSessionMetrics is the runner's evaluation derived from session rows.
type QualityAgentSessionMetrics struct {
	StateCounts                 map[storepb.QualityAgentSessionState]int32
	TokensTotal                 int64
	TokensLastHour              int64
	InvestigationsTotal         int32
	InvestigationsInconclusive  int32
	InvestigationsRootCause     int32
	InvestigationsContributing  int32
	InvestigationsUnrelated     int32
	AgentChangeRequestsTotal    int32
	AgentChangeRequestsApplied  int32
	AgentChangeRequestsRejected int32
	AgentChangeRequestsPending  int32
	AgentChangeRequestsApproved int32
	AgentChangeRequestsExpired  int32
	TopTargets                  []QualityAgentSessionTargetCost
}

// QualityAgentSessionTargetCost is one target's share of the agent token spend.
type QualityAgentSessionTargetCost struct {
	TargetGUID   string
	TargetType   storepb.MetaType
	TokensUsed   int64
	SessionCount int32
}

// GetQualityAgentSessionMetrics aggregates the runner's sessions, their token spend
// from the transcript (so archived sessions still count) and the products the agent
// recorded. Change-request and investigation rows carry no workspace column, so those
// counts are global by design in this single-workspace deployment.
func (s *Store) GetQualityAgentSessionMetrics(ctx context.Context, workspaceID string) (*QualityAgentSessionMetrics, error) {
	metrics := &QualityAgentSessionMetrics{StateCounts: map[storepb.QualityAgentSessionState]int32{}}
	stateQuery := `SELECT state, COUNT(*) FROM quality_agent_session
		WHERE workspace_id = $1 AND archived_at IS NULL GROUP BY state`
	rows, err := s.GetDB().QueryContext(ctx, stateQuery, workspaceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the session states")
	}
	defer rows.Close()
	for rows.Next() {
		var (
			state int16
			count int32
		)
		if err := rows.Scan(&state, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan a session state")
		}
		metrics.StateCounts[storepb.QualityAgentSessionState(state)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the session states")
	}

	tokenQuery := `SELECT COALESCE((SELECT SUM(tokens_used) FROM quality_agent_session WHERE workspace_id = $1), 0),
			COALESCE((SELECT SUM(tokens) FROM quality_agent_message WHERE workspace_id = $1 AND created_at > NOW() - INTERVAL '1 hour'), 0)`
	if err := s.GetDB().QueryRowContext(ctx, tokenQuery, workspaceID).
		Scan(&metrics.TokensTotal, &metrics.TokensLastHour); err != nil {
		return nil, errors.Wrap(err, "failed to read the session token spend")
	}

	changeRequestQuery := `SELECT COUNT(*),
			COUNT(*) FILTER (WHERE state = $2),
			COUNT(*) FILTER (WHERE state = $3),
			COUNT(*) FILTER (WHERE state = $4),
			COUNT(*) FILTER (WHERE state = $5),
			COUNT(*) FILTER (WHERE state = $6)
		FROM quality_change_request WHERE source = $1`
	if err := s.GetDB().QueryRowContext(ctx, changeRequestQuery,
		int16(storepb.QualityChangeRequestSource_QUALITY_CHANGE_REQUEST_SOURCE_AGENT),
		int16(storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPLIED),
		int16(storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_REJECTED),
		int16(storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_PROPOSED),
		int16(storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPROVED),
		int16(storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_EXPIRED),
	).Scan(&metrics.AgentChangeRequestsTotal, &metrics.AgentChangeRequestsApplied,
		&metrics.AgentChangeRequestsRejected, &metrics.AgentChangeRequestsPending,
		&metrics.AgentChangeRequestsApproved, &metrics.AgentChangeRequestsExpired); err != nil {
		return nil, errors.Wrap(err, "failed to read the agent change requests")
	}

	investigationQuery := `SELECT COUNT(*),
			COUNT(*) FILTER (WHERE verdict = $1),
			COUNT(*) FILTER (WHERE verdict = $2),
			COUNT(*) FILTER (WHERE verdict = $3),
			COUNT(*) FILTER (WHERE verdict = $4)
		FROM quality_investigation`
	if err := s.GetDB().QueryRowContext(ctx, investigationQuery,
		int16(storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE),
		int16(storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE),
		int16(storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_CONTRIBUTING_FACTOR),
		int16(storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_UNRELATED),
	).Scan(&metrics.InvestigationsTotal, &metrics.InvestigationsInconclusive,
		&metrics.InvestigationsRootCause, &metrics.InvestigationsContributing,
		&metrics.InvestigationsUnrelated); err != nil {
		return nil, errors.Wrap(err, "failed to read the investigation verdicts")
	}

	targetQuery := `SELECT g.target_guid, g.target_type, COALESCE(SUM(m.tokens), 0),
			COUNT(DISTINCT g.id)
		FROM quality_agent_message m
		JOIN quality_agent_session g ON g.id = m.session_id
		WHERE g.workspace_id = $1
		GROUP BY g.target_guid, g.target_type
		ORDER BY SUM(m.tokens) DESC, COUNT(DISTINCT g.id) DESC, g.target_guid ASC LIMIT 5`
	targetRows, err := s.GetDB().QueryContext(ctx, targetQuery, workspaceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target costs")
	}
	defer targetRows.Close()
	for targetRows.Next() {
		var (
			cost       QualityAgentSessionTargetCost
			targetType int16
		)
		if err := targetRows.Scan(&cost.TargetGUID, &targetType, &cost.TokensUsed, &cost.SessionCount); err != nil {
			return nil, errors.Wrap(err, "failed to scan a target cost")
		}
		cost.TargetType = storepb.MetaType(targetType)
		metrics.TopTargets = append(metrics.TopTargets, cost)
	}
	if err := targetRows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the target costs")
	}
	return metrics, nil
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
		message     QualityAgentSessionMessage
		targetType  int16
		state       int16
		engineState []byte
		agentID     int64
		agentPrinID int32
	)
	dest := []any{
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.TargetGUID,
		&targetType, &message.DatabaseRef, &agentID, &agentPrinID, &message.Objective,
		&state, &message.BlockedCode, &message.BlockedReason, &message.Revision,
		&message.WakeCount, &message.TokensUsed, &message.ContextTokens, &message.Summary,
		&message.SummaryThroughSeq, &message.ProcessedSeq, &engineState,
		&message.NextWakeAt, &message.LeaseUntil, &message.RestartCount, &message.CreatedBy,
		&message.CreatedAt, &message.UpdatedAt, &message.ArchivedAt,
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
	engineStateMessage := &storepb.QualityAgentSessionEngineState{}
	if len(engineState) > 0 && string(engineState) != "{}" {
		if err := common.ProtojsonUnmarshaler.Unmarshal(engineState, engineStateMessage); err != nil {
			return nil, errors.Wrap(err, "failed to decode the session engine state")
		}
	}
	message.EngineState = engineStateMessage
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
		&message.Turn, &role, &origin, &message.Content, &toolCalls, &message.ToolCallID,
		&message.ToolName, &message.Reference, &message.Tokens, &message.PromptTokens,
		&message.CompletionTokens, &message.Error, &message.Truncated,
		&message.IdempotencyKey, &message.CreatedAt,
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

// CountQualityAgentSessionsByLease counts the sessions currently running (a live lease).
func (s *Store) CountQualityAgentSessionsByLease(ctx context.Context, workspaceID string, now time.Time) (int32, error) {
	var count int32
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_session
		WHERE workspace_id = $1 AND archived_at IS NULL AND lease_until > $2`,
		workspaceID, now).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the running sessions")
	}
	return count, nil
}

// CountQualityAgentSessionsByState counts the non-archived sessions in one state.
func (s *Store) CountQualityAgentSessionsByState(ctx context.Context, workspaceID string, state storepb.QualityAgentSessionState) (int32, error) {
	var count int32
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM quality_agent_session
		WHERE workspace_id = $1 AND archived_at IS NULL AND state = $2`,
		workspaceID, int16(state)).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count the session states")
	}
	return count, nil
}

// LastQualityAgentSessionActivity is the workspace's newest transcript entry, the
// runner status's "last activity" heartbeat.
func (s *Store) LastQualityAgentSessionActivity(ctx context.Context, workspaceID string) (*time.Time, error) {
	var last sql.NullTime
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT MAX(created_at) FROM quality_agent_message WHERE workspace_id = $1`,
		workspaceID).Scan(&last); err != nil {
		return nil, errors.Wrap(err, "failed to read the last session activity")
	}
	if !last.Valid {
		return nil, nil
	}
	value := last.Time.UTC()
	return &value, nil
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
