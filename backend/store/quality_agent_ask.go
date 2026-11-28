package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// QualityAgentAskKind is what the lead is telling the owner.
type QualityAgentAskKind int16

const (
	QualityAgentAskKindQuestion QualityAgentAskKind = 1
	QualityAgentAskKindAnomaly  QualityAgentAskKind = 2
	QualityAgentAskKindApproval QualityAgentAskKind = 3
)

// QualityAgentAskStatus is the ask's lifecycle.
type QualityAgentAskStatus int16

const (
	QualityAgentAskStatusOpen      QualityAgentAskStatus = 1
	QualityAgentAskStatusAnswered  QualityAgentAskStatus = 2
	QualityAgentAskStatusResolved  QualityAgentAskStatus = 3
	QualityAgentAskStatusDismissed QualityAgentAskStatus = 4
)

// QualityAgentAskMessage is one owner-facing question from the lead. It can cover
// several requests through quality_agent_request.ask_id.
type QualityAgentAskMessage struct {
	ID                    int64
	ResourceID            string
	WorkspaceID           string
	AgentID               int64
	ConversationSessionID *int64
	Kind                  QualityAgentAskKind
	MessageSeq            int64
	Title                 string
	Question              string
	Status                QualityAgentAskStatus
	AnsweredBy            *int32
	AnsweredAt            *time.Time
	AnswerMessageSeq      int64
	ResolvedAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

const qualityAgentAskColumns = `id, resource_id, workspace_id, agent_id,
	conversation_session_id, kind, message_seq, title, question, status,
	answered_by, answered_at, answer_message_seq, resolved_at, created_at, updated_at`

// CreateQualityAgentAsk inserts one owner-facing question.
func (s *Store) CreateQualityAgentAsk(ctx context.Context, message *QualityAgentAskMessage) (*QualityAgentAskMessage, error) {
	if message == nil || message.AgentID <= 0 || message.WorkspaceID == "" {
		return nil, common.Errorf(common.Invalid, "an ask needs an agent and a workspace")
	}
	if message.Kind == 0 {
		message.Kind = QualityAgentAskKindQuestion
	}
	if message.Status == 0 {
		message.Status = QualityAgentAskStatusOpen
	}
	now := time.Now().UTC()
	if message.CreatedAt.IsZero() {
		message.CreatedAt = now
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = now
	}
	resourceID := message.ResourceID
	var err error
	if resourceID == "" {
		resourceID, err = common.RandomString(12)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate the ask id")
		}
	}
	created, err := scanQualityAgentAsk(s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_ask
			(resource_id, workspace_id, agent_id, conversation_session_id, kind, message_seq,
			 title, question, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+qualityAgentAskColumns,
		resourceID, message.WorkspaceID, message.AgentID, message.ConversationSessionID,
		int16(message.Kind), message.MessageSeq, message.Title, message.Question,
		int16(message.Status), message.CreatedAt, message.UpdatedAt))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality agent ask")
	}
	return created, nil
}

// GetQualityAgentAskByResourceID reads one ask; nil means absent.
func (s *Store) GetQualityAgentAskByResourceID(ctx context.Context, resourceID string) (*QualityAgentAskMessage, error) {
	message, err := scanQualityAgentAsk(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentAskColumns+` FROM quality_agent_ask WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent ask")
	}
	return message, nil
}

// ListQualityAgentAsks lists an agent's asks, newest first.
func (s *Store) ListQualityAgentAsks(ctx context.Context, agentID int64, statuses []QualityAgentAskStatus, limit int) ([]*QualityAgentAskMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + qualityAgentAskColumns + ` FROM quality_agent_ask WHERE agent_id = $1`
	args := []any{agentID}
	if len(statuses) > 0 {
		values := make([]int16, 0, len(statuses))
		for _, status := range statuses {
			values = append(values, int16(status))
		}
		args = append(args, values)
		query += ` AND status = ANY($` + strconv.Itoa(len(args)) + `::int2[])`
	}
	args = append(args, limit)
	query += ` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args))
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent asks")
	}
	defer rows.Close()
	var messages []*QualityAgentAskMessage
	for rows.Next() {
		message, err := scanQualityAgentAsk(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent ask")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent asks")
	}
	return messages, nil
}

// MarkQualityAgentAskAnswered records the owner's reply and returns the open ask.
func (s *Store) MarkQualityAgentAskAnswered(ctx context.Context, resourceID string, answeredBy int32, answerSeq int64, at time.Time) (*QualityAgentAskMessage, bool, error) {
	message, err := scanQualityAgentAsk(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_ask
		SET status = $2, answered_by = $3, answered_at = $4, answer_message_seq = $5, updated_at = $4
		WHERE resource_id = $1 AND status = $6
		RETURNING `+qualityAgentAskColumns,
		resourceID, int16(QualityAgentAskStatusAnswered), answeredBy, at.UTC(), answerSeq,
		int16(QualityAgentAskStatusOpen)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to mark the ask answered")
	}
	return message, true, nil
}

// ResolveQualityAgentAsk closes an ask once every request under it settled.
func (s *Store) ResolveQualityAgentAsk(ctx context.Context, resourceID string, status QualityAgentAskStatus, at time.Time) (bool, error) {
	if status != QualityAgentAskStatusResolved && status != QualityAgentAskStatusDismissed {
		return false, common.Errorf(common.Invalid, "an ask can only resolve or dismiss")
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_ask SET status = $2, resolved_at = $3, updated_at = $3
		WHERE resource_id = $1 AND status IN ($4, $5)`,
		resourceID, int16(status), at.UTC(),
		int16(QualityAgentAskStatusOpen), int16(QualityAgentAskStatusAnswered))
	if err != nil {
		return false, errors.Wrap(err, "failed to resolve the ask")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the resolved asks")
	}
	return affected > 0, nil
}

func scanQualityAgentAsk(scanner interface{ Scan(dest ...any) error }) (*QualityAgentAskMessage, error) {
	var (
		message      QualityAgentAskMessage
		kind         int16
		status       int16
		conversation sql.NullInt64
		answeredBy   sql.NullInt32
		answeredAt   sql.NullTime
		resolvedAt   sql.NullTime
	)
	err := scanner.Scan(
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.AgentID,
		&conversation, &kind, &message.MessageSeq, &message.Title, &message.Question,
		&status, &answeredBy, &answeredAt, &message.AnswerMessageSeq, &resolvedAt,
		&message.CreatedAt, &message.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	message.Kind = QualityAgentAskKind(kind)
	message.Status = QualityAgentAskStatus(status)
	if conversation.Valid {
		value := conversation.Int64
		message.ConversationSessionID = &value
	}
	if answeredBy.Valid {
		value := answeredBy.Int32
		message.AnsweredBy = &value
	}
	if answeredAt.Valid {
		value := answeredAt.Time
		message.AnsweredAt = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		message.ResolvedAt = &value
	}
	return &message, nil
}
