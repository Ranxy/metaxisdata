package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentAttentionKind is why the owner is needed.
type QualityAgentAttentionKind int16

const (
	QualityAgentAttentionKindBlocked         QualityAgentAttentionKind = 1
	QualityAgentAttentionKindProposalPending QualityAgentAttentionKind = 2
	QualityAgentAttentionKindReportAnomaly   QualityAgentAttentionKind = 3
)

// QualityAgentAttentionStatus is the to-do's lifecycle.
type QualityAgentAttentionStatus int16

const (
	QualityAgentAttentionStatusOpen      QualityAgentAttentionStatus = 1
	QualityAgentAttentionStatusResolved  QualityAgentAttentionStatus = 2
	QualityAgentAttentionStatusDismissed QualityAgentAttentionStatus = 3
)

// QualityAgentAttentionMessage is one owner escalation.
type QualityAgentAttentionMessage struct {
	ID          int64
	ResourceID  string
	WorkspaceID string
	AgentID     int64
	Kind        QualityAgentAttentionKind
	SessionID   *int64
	Title       string
	Question    string
	Status      QualityAgentAttentionStatus
	DedupeKey   string
	ResolvedBy  *int32
	ResolvedAt  *time.Time
	CreatedAt   time.Time
}

const qualityAgentAttentionColumns = `id, resource_id, workspace_id, agent_id, kind,
	session_id, title, question, status, dedupe_key, resolved_by, resolved_at,
	created_at`

// CreateQualityAgentAttention inserts one open escalation. The partial unique
// index on (agent_id, dedupe_key) makes a retried runner pass converge on the
// existing open row; created reports whether this call inserted it.
func (s *Store) CreateQualityAgentAttention(ctx context.Context, message *QualityAgentAttentionMessage) (*QualityAgentAttentionMessage, bool, error) {
	if message == nil || message.AgentID <= 0 || message.WorkspaceID == "" {
		return nil, false, common.Errorf(common.Invalid, "an attention needs an agent and a workspace")
	}
	if message.Kind == 0 {
		message.Kind = QualityAgentAttentionKindBlocked
	}
	if message.Status == 0 {
		message.Status = QualityAgentAttentionStatusOpen
	}
	resourceID := message.ResourceID
	var err error
	if resourceID == "" {
		resourceID, err = common.RandomString(12)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to generate the attention id")
		}
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_attention
			(resource_id, workspace_id, agent_id, kind, session_id, title,
			 question, status, dedupe_key, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (agent_id, dedupe_key) WHERE status = 1 DO NOTHING
		RETURNING `+qualityAgentAttentionColumns,
		resourceID, message.WorkspaceID, message.AgentID, int16(message.Kind),
		message.SessionID, message.Title, message.Question,
		int16(message.Status), message.DedupeKey, message.CreatedAt)
	created, err := scanQualityAgentAttention(row)
	if errors.Is(err, sql.ErrNoRows) {
		existing, readErr := s.GetOpenQualityAgentAttentionByDedupe(ctx, message.AgentID, message.DedupeKey)
		if readErr != nil {
			return nil, false, readErr
		}
		if existing == nil {
			return nil, false, errors.New("the attention conflicted but no open row was found")
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality agent attention")
	}
	return created, true, nil
}

// GetOpenQualityAgentAttentionByDedupe reads one open escalation.
func (s *Store) GetOpenQualityAgentAttentionByDedupe(ctx context.Context, agentID int64, dedupeKey string) (*QualityAgentAttentionMessage, error) {
	message, err := scanQualityAgentAttention(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentAttentionColumns+` FROM quality_agent_attention
		 WHERE agent_id = $1 AND dedupe_key = $2 AND status = $3`,
		agentID, dedupeKey, int16(QualityAgentAttentionStatusOpen)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the open quality agent attention")
	}
	return message, nil
}

// SetOpenQualityAgentAttentionQuestion refreshes the wording of the one open item
// for an agent/dedupe key. It never re-notifies: the notification is sent only when
// the item is first created, while the question must stay truthful as later passes
// change the counts behind it.
func (s *Store) SetOpenQualityAgentAttentionQuestion(ctx context.Context, agentID int64, dedupeKey, question string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_attention
		SET question = $3
		WHERE agent_id = $1 AND dedupe_key = $2 AND status = $4`,
		agentID, dedupeKey, question, int16(QualityAgentAttentionStatusOpen))
	if err != nil {
		return errors.Wrap(err, "failed to update the open quality agent attention")
	}
	return nil
}

// GetQualityAgentAttentionByResourceID reads one escalation; nil means absent.
func (s *Store) GetQualityAgentAttentionByResourceID(ctx context.Context, resourceID string) (*QualityAgentAttentionMessage, error) {
	message, err := scanQualityAgentAttention(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentAttentionColumns+` FROM quality_agent_attention WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent attention")
	}
	return message, nil
}

// ListQualityAgentAttentions lists one agent's to-dos. An empty status list keeps
// every status; the caller passes OPEN for the pending view.
func (s *Store) ListQualityAgentAttentions(ctx context.Context, agentID int64, statuses []QualityAgentAttentionStatus, limit int) ([]*QualityAgentAttentionMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + qualityAgentAttentionColumns + ` FROM quality_agent_attention
		WHERE agent_id = $1`
	args := []any{agentID}
	if len(statuses) > 0 {
		placeholders := ""
		for i, status := range statuses {
			if i > 0 {
				placeholders += ", "
			}
			args = append(args, int16(status))
			placeholders += "$" + strconv.Itoa(len(args))
		}
		query += " AND status IN (" + placeholders + ")"
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent attentions")
	}
	defer rows.Close()
	var attentions []*QualityAgentAttentionMessage
	for rows.Next() {
		attention, err := scanQualityAgentAttention(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent attention")
		}
		attentions = append(attentions, attention)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent attentions")
	}
	return attentions, nil
}

// ResolveQualityAgentAttention closes one open escalation. applied is false when
// it was already closed, so a retried reply is idempotent.
func (s *Store) ResolveQualityAgentAttention(ctx context.Context, resourceID string, status QualityAgentAttentionStatus, resolvedBy *int32, now time.Time) (*QualityAgentAttentionMessage, bool, error) {
	row := s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_attention
		SET status = $2, resolved_by = $3, resolved_at = $4
		WHERE resource_id = $1 AND status = $5
		RETURNING `+qualityAgentAttentionColumns,
		resourceID, int16(status), resolvedBy, now, int16(QualityAgentAttentionStatusOpen))
	attention, err := scanQualityAgentAttention(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to resolve the quality agent attention")
	}
	return attention, true, nil
}

func scanQualityAgentAttention(scanner interface{ Scan(dest ...any) error }) (*QualityAgentAttentionMessage, error) {
	var (
		message    QualityAgentAttentionMessage
		kind       int16
		status     int16
		sessionID  sql.NullInt64
		resolvedBy sql.NullInt32
		resolvedAt sql.NullTime
	)
	scanErr := scanner.Scan(
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.AgentID,
		&kind, &sessionID, &message.Title, &message.Question,
		&status, &message.DedupeKey, &resolvedBy, &resolvedAt, &message.CreatedAt,
	)
	if scanErr != nil {
		return nil, scanErr
	}
	message.Kind = QualityAgentAttentionKind(kind)
	message.Status = QualityAgentAttentionStatus(status)
	if sessionID.Valid {
		value := sessionID.Int64
		message.SessionID = &value
	}
	if resolvedBy.Valid {
		value := resolvedBy.Int32
		message.ResolvedBy = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		message.ResolvedAt = &value
	}
	return &message, nil
}

// ReplyQualityAgentAttentionAtomic answers an open escalation in one transaction:
// claim the attention, append the owner's ASK to the target session, resume a
// BLOCKED session, record the front-office note, and resolve the attention. A retry
// on a closed attention returns applied=false without writing anything.
func (s *Store) ReplyQualityAgentAttentionAtomic(ctx context.Context, resourceID, workspaceID, content string, createdBy *int32, idempotencyKey string, now time.Time) (*QualityAgentAttentionMessage, *QualityAgentTranscriptMessage, bool, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to begin the attention reply transaction")
	}
	defer func() { _ = tx.Rollback() }()
	attention, err := scanQualityAgentAttention(tx.QueryRowContext(ctx,
		`SELECT `+qualityAgentAttentionColumns+` FROM quality_agent_attention
		 WHERE resource_id = $1 FOR UPDATE`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, false, nil
		}
		return nil, nil, false, errors.Wrap(err, "failed to read the attention for reply")
	}
	if attention.Status != QualityAgentAttentionStatusOpen {
		return attention, nil, false, nil
	}
	var answered *QualityAgentTranscriptMessage
	if attention.SessionID != nil {
		_, lockErr := tx.ExecContext(ctx,
			`SELECT id FROM quality_agent_session WHERE id = $1 FOR UPDATE`, *attention.SessionID)
		if lockErr != nil {
			return nil, nil, false, errors.Wrap(lockErr, "failed to lock the attention's session")
		}
		var wake int32
		if err := tx.QueryRowContext(ctx,
			`SELECT wake_count + 1 FROM quality_agent_session WHERE id = $1`,
			*attention.SessionID).Scan(&wake); err != nil {
			return nil, nil, false, errors.Wrap(err, "failed to read the attention's session wake")
		}
		answered, err = scanQualityAgentTranscript(tx.QueryRowContext(ctx, `
			INSERT INTO quality_agent_message
				(session_id, workspace_id, seq, wake, turn, role, origin, content, tool_calls,
				 tool_call_id, tool_name, reference, tokens, error, truncated, idempotency_key)
			VALUES ($1, $2,
				(SELECT COALESCE(MAX(seq), 0) + 1 FROM quality_agent_message WHERE session_id = $1),
				$3, 0, $4, $5, $6, '{}', '', '', '', 0, '', false, $7)
			RETURNING `+qualityAgentTranscriptColumns,
			*attention.SessionID, workspaceID, wake,
			int16(storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
			int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK),
			content, idempotencyKey))
		if err != nil {
			return nil, nil, false, errors.Wrap(err, "failed to append the attention reply")
		}
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE quality_agent_session
			SET state = CASE WHEN state = $2 THEN $3 ELSE state END,
				blocked_code = CASE WHEN state = $2 THEN '' ELSE blocked_code END,
				blocked_reason = CASE WHEN state = $2 THEN '' ELSE blocked_reason END,
				next_wake_at = LEAST(COALESCE(next_wake_at, $4), $4),
				revision = revision + 1, updated_at = NOW()
			WHERE id = $1 AND archived_at IS NULL`,
			*attention.SessionID,
			int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED),
			int16(storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED), now)
		if updateErr != nil {
			return nil, nil, false, errors.Wrap(updateErr, "failed to resume the attention's session")
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected == 0 {
			return nil, nil, false, errors.New("the attention's session is archived or missing")
		}
	}
	var conversationID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM quality_agent_session
		WHERE agent_id = $1 AND kind = 2 AND archived_at IS NULL LIMIT 1`,
		attention.AgentID).Scan(&conversationID); err == nil {
		role := storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER
		body := content
		if attention.SessionID != nil {
			role = storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE
			body = "The owner replied to the escalation."
		}
		if _, lockErr := tx.ExecContext(ctx,
			`SELECT id FROM quality_agent_session WHERE id = $1 FOR UPDATE`, conversationID); lockErr != nil {
			return nil, nil, false, errors.Wrap(lockErr, "failed to lock the attention conversation")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quality_agent_message
				(session_id, workspace_id, seq, wake, turn, role, origin, content, tool_calls,
				 tool_call_id, tool_name, reference, tokens, error, truncated, idempotency_key)
			VALUES ($1, $2,
				(SELECT COALESCE(MAX(seq), 0) + 1 FROM quality_agent_message WHERE session_id = $1),
				0, 0, $3, $4, $5, '{}', '', '', $6, 0, '', false, $7)`,
			conversationID, workspaceID, int16(role),
			int16(storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK),
			body, resourceID, "attention-note:"+resourceID); err != nil {
			return nil, nil, false, errors.Wrap(err, "failed to record the reply in the conversation")
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE quality_agent_session SET next_wake_at = LEAST(COALESCE(next_wake_at, $2), $2),
				revision = revision + 1, updated_at = NOW()
			WHERE id = $1`, conversationID, now); err != nil {
			return nil, nil, false, errors.Wrap(err, "failed to wake the agent conversation")
		}
	}
	resolved, err := scanQualityAgentAttention(tx.QueryRowContext(ctx, `
		UPDATE quality_agent_attention
		SET status = $2, resolved_by = $3, resolved_at = $4
		WHERE resource_id = $1
		RETURNING `+qualityAgentAttentionColumns,
		resourceID, int16(QualityAgentAttentionStatusResolved), createdBy, now))
	if err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to resolve the attention")
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, errors.Wrap(err, "failed to commit the attention reply")
	}
	return resolved, answered, true, nil
}
