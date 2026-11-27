package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"

	"github.com/lib/pq"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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
	// TargetGUID is the target an approval ask is about; empty for an agent-level ask.
	TargetGUID string
	TargetType storepb.MetaType
	// DedupeKey keeps one active ask per (agent, key); empty means not deduped.
	DedupeKey        string
	Status           QualityAgentAskStatus
	AnsweredBy       *int32
	AnsweredAt       *time.Time
	AnswerMessageSeq int64
	ResolvedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const qualityAgentAskColumns = `id, resource_id, workspace_id, agent_id,
	conversation_session_id, kind, message_seq, title, question, target_guid, target_type,
	dedupe_key, status, answered_by, answered_at, answer_message_seq, resolved_at,
	created_at, updated_at`

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
			 title, question, target_guid, target_type, dedupe_key, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+qualityAgentAskColumns,
		resourceID, message.WorkspaceID, message.AgentID, message.ConversationSessionID,
		int16(message.Kind), message.MessageSeq, message.Title, message.Question,
		message.TargetGUID, int16(message.TargetType), message.DedupeKey,
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
	return s.ListQualityAgentAsksPage(ctx, agentID, statuses, limit, 0)
}

// ListQualityAgentAsksPage lists one page of an agent's asks, newest first.
func (s *Store) ListQualityAgentAsksPage(ctx context.Context, agentID int64, statuses []QualityAgentAskStatus, limit, offset int) ([]*QualityAgentAskMessage, error) {
	if limit <= 0 || limit > 1001 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
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
	args = append(args, limit, offset)
	query += ` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)-1) +
		` OFFSET $` + strconv.Itoa(len(args))
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

// ListQualityAgentAsksByIDs maps ask ids to rows, for rendering a request's ask link.
func (s *Store) ListQualityAgentAsksByIDs(ctx context.Context, ids []int64) (map[int64]*QualityAgentAskMessage, error) {
	asks := map[int64]*QualityAgentAskMessage{}
	if len(ids) == 0 {
		return asks, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityAgentAskColumns+` FROM quality_agent_ask WHERE id = ANY($1)`,
		pq.Array(ids))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality agent asks by id")
	}
	defer rows.Close()
	for rows.Next() {
		ask, err := scanQualityAgentAsk(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent ask")
		}
		asks[ask.ID] = ask
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent asks")
	}
	return asks, nil
}

// MarkLatestOpenQualityAgentAskAnswered records the owner's reply against the ask
// whose question was the last owner-visible message. The lead may speak again after
// asking (a reply_owner note), which is why the newest owner-facing message and not
// "any open ask" decides: a free-form message must not close an older question.
func (s *Store) MarkLatestOpenQualityAgentAskAnswered(ctx context.Context, agentID int64, answeredBy int32, answerSeq int64, at time.Time) (*QualityAgentAskMessage, bool, error) {
	message, err := scanQualityAgentAsk(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_ask a
		SET status = $2, answered_by = $3, answered_at = $4, answer_message_seq = $5, updated_at = $4
		WHERE a.id = (
			SELECT candidate.id FROM quality_agent_ask candidate
			WHERE candidate.agent_id = $1 AND candidate.status = $6
			  AND candidate.message_seq = (
				SELECT MAX(seq) FROM quality_agent_message
				WHERE session_id = candidate.conversation_session_id
				  AND audience = $7 AND seq < $5
			  )
			ORDER BY candidate.id DESC LIMIT 1
		)
		RETURNING `+qualityAgentAskColumns,
		agentID, int16(QualityAgentAskStatusAnswered), answeredBy, at.UTC(), answerSeq,
		int16(QualityAgentAskStatusOpen),
		int16(storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to mark the ask answered")
	}
	return message, true, nil
}

// RefreshQualityAgentAskByDedupe rewrites an open ask's wording without notifying again.
// The approval queue changes as drafts are added or decided, and a stale "3 pending"
// would mislead the owner.
func (s *Store) RefreshQualityAgentAskByDedupe(ctx context.Context, agentID int64, dedupeKey, title, question string, at time.Time) (bool, error) {
	if agentID <= 0 || dedupeKey == "" {
		return false, nil
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_ask
		SET title = $3, question = $4, updated_at = $5
		WHERE agent_id = $1 AND dedupe_key = $2 AND status = $6`,
		agentID, dedupeKey, title, question, at.UTC(),
		int16(QualityAgentAskStatusOpen))
	if err != nil {
		return false, errors.Wrap(err, "failed to refresh the open ask")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the refreshed asks")
	}
	return affected > 0, nil
}

// ProposalAskDedupeKey is the one key a target's approval ask uses. Creating and
// draining it must use the same value, or the ask can never close.
func ProposalAskDedupeKey(targetGUID string) string {
	return "proposal:" + targetGUID
}

// ResolveProposalAskIfDrained closes a target's approval ask only when it has no
// pending rule draft left. A single decided draft must not close a queue that still
// holds others; the old attention writer guarded this with the same NOT EXISTS.
func (s *Store) ResolveProposalAskIfDrained(ctx context.Context, agentID int64, targetGUID, dedupeKey string, at time.Time) (bool, error) {
	if agentID <= 0 || targetGUID == "" || dedupeKey == "" {
		return false, nil
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_ask a
		SET status = $3, resolved_at = $4, updated_at = $4
		WHERE a.agent_id = $1 AND a.dedupe_key = $2 AND a.status IN ($5, $6)
		  AND NOT EXISTS (
			SELECT 1 FROM quality_suggestion s
			WHERE s.agent_id = $1 AND s.target_guid = $7
			  AND s.status = $8 AND s.kind = $9
		  )`,
		agentID, dedupeKey, int16(QualityAgentAskStatusResolved), at.UTC(),
		int16(QualityAgentAskStatusOpen), int16(QualityAgentAskStatusAnswered),
		targetGUID, int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
		int16(storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT))
	if err != nil {
		return false, errors.Wrap(err, "failed to resolve a drained proposal ask")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the resolved proposal asks")
	}
	return affected > 0, nil
}

// DismissQualityAgentAsk closes an ask the owner explicitly dismissed, together with
// the requests it covers. It is not an answer: no request becomes ANSWERED and no
// subagent receives a decision it never got.
func (s *Store) DismissQualityAgentAsk(ctx context.Context, askID int64, at time.Time) (bool, error) {
	if askID <= 0 {
		return false, nil
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to begin the ask dismissal")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_ask SET status = $2, resolved_at = $3, updated_at = $3
		WHERE id = $1 AND status IN ($4, $5)`,
		askID, int16(QualityAgentAskStatusDismissed), at.UTC(),
		int16(QualityAgentAskStatusOpen), int16(QualityAgentAskStatusAnswered))
	if err != nil {
		return false, errors.Wrap(err, "failed to dismiss the ask")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the dismissed ask")
	}
	if affected == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE quality_agent_request SET status = $2, resolved_at = $3, updated_at = $3
		WHERE ask_id = $1 AND status IN ($4, $5)`,
		askID, int16(QualityAgentRequestStatusDismissed), at.UTC(),
		int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded)); err != nil {
		return false, errors.Wrap(err, "failed to dismiss the ask's requests")
	}
	if err := tx.Commit(); err != nil {
		return false, errors.Wrap(err, "failed to commit the ask dismissal")
	}
	return true, nil
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
		targetType   int16
		conversation sql.NullInt64
		answeredBy   sql.NullInt32
		answeredAt   sql.NullTime
		resolvedAt   sql.NullTime
	)
	err := scanner.Scan(
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.AgentID,
		&conversation, &kind, &message.MessageSeq, &message.Title, &message.Question,
		&message.TargetGUID, &targetType, &message.DedupeKey, &status,
		&answeredBy, &answeredAt, &message.AnswerMessageSeq, &resolvedAt,
		&message.CreatedAt, &message.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	message.Kind = QualityAgentAskKind(kind)
	message.Status = QualityAgentAskStatus(status)
	message.TargetType = storepb.MetaType(targetType)
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

// CreateQualityAgentAskAtomic writes the owner-visible message, the ask and the
// request forwards in one transaction. A failure leaves no orphan message or ask.
func (s *Store) CreateQualityAgentAskAtomic(ctx context.Context, ask *QualityAgentAskMessage, lead *QualityAgentSessionMessage, message *QualityAgentTranscriptMessage, requests []*QualityAgentRequestMessage, at time.Time) (*QualityAgentAskMessage, error) {
	created, _, err := s.CreateQualityAgentAskDeduped(ctx, ask, lead, message, requests, at)
	return created, err
}

// CreateQualityAgentAskDeduped is CreateQualityAgentAskAtomic plus the created flag.
// When the ask carries a dedupe key and an open or answered ask already holds it, that
// row is returned and no second owner message is written.
func (s *Store) CreateQualityAgentAskDeduped(ctx context.Context, ask *QualityAgentAskMessage, lead *QualityAgentSessionMessage, message *QualityAgentTranscriptMessage, requests []*QualityAgentRequestMessage, at time.Time) (*QualityAgentAskMessage, bool, error) {
	if ask == nil || lead == nil || message == nil {
		return nil, false, common.Errorf(common.Invalid, "an ask needs a lead and a message")
	}
	if ask.AgentID <= 0 || ask.WorkspaceID == "" {
		return nil, false, common.Errorf(common.Invalid, "an ask needs an agent and a workspace")
	}
	if ask.Kind == 0 {
		ask.Kind = QualityAgentAskKindQuestion
	}
	if ask.Status == 0 {
		ask.Status = QualityAgentAskStatusOpen
	}
	if ask.ResourceID == "" {
		resourceID, err := common.RandomString(12)
		if err != nil {
			return nil, false, errors.Wrap(err, "failed to generate the ask id")
		}
		ask.ResourceID = resourceID
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	ask.CreatedAt = at
	ask.UpdatedAt = at
	ask.ConversationSessionID = &lead.ID

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to begin the ask transaction")
	}
	defer func() { _ = tx.Rollback() }()
	if ask.DedupeKey != "" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
			fmt.Sprintf("quality-agent-ask:%d:%s", ask.AgentID, ask.DedupeKey)); err != nil {
			return nil, false, errors.Wrap(err, "failed to serialize the ask dedupe")
		}
		existing, findErr := scanQualityAgentAsk(tx.QueryRowContext(ctx,
			`SELECT `+qualityAgentAskColumns+` FROM quality_agent_ask
			 WHERE agent_id = $1 AND dedupe_key = $2 AND status = $3
			 ORDER BY id DESC LIMIT 1`,
			ask.AgentID, ask.DedupeKey, int16(QualityAgentAskStatusOpen)))
		if findErr == nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, false, errors.Wrap(commitErr, "failed to commit the duplicate ask")
			}
			return existing, false, nil
		}
		if !errors.Is(findErr, sql.ErrNoRows) {
			return nil, false, errors.Wrap(findErr, "failed to read the existing ask")
		}
		// An answered ask no longer owns the key. Close it before inserting the new
		// question so the owner does not see two cards for the same target.
		if _, err := tx.ExecContext(ctx, `
			UPDATE quality_agent_ask
			SET status = $3, resolved_at = $4, updated_at = $4
			WHERE agent_id = $1 AND dedupe_key = $2 AND status = $5`,
			ask.AgentID, ask.DedupeKey, int16(QualityAgentAskStatusResolved), at.UTC(),
			int16(QualityAgentAskStatusAnswered)); err != nil {
			return nil, false, errors.Wrap(err, "failed to close the answered ask")
		}
	}
	message.SessionID = lead.ID
	message.WorkspaceID = lead.WorkspaceID
	createdMessage, err := s.appendQualityAgentTranscriptTx(ctx, tx, message)
	if err != nil {
		return nil, false, err
	}
	ask.MessageSeq = createdMessage.Seq
	created, err := scanQualityAgentAsk(tx.QueryRowContext(ctx, `
		INSERT INTO quality_agent_ask
			(resource_id, workspace_id, agent_id, conversation_session_id, kind, message_seq,
			 title, question, target_guid, target_type, dedupe_key, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+qualityAgentAskColumns,
		ask.ResourceID, ask.WorkspaceID, ask.AgentID, ask.ConversationSessionID,
		int16(ask.Kind), ask.MessageSeq, ask.Title, ask.Question,
		ask.TargetGUID, int16(ask.TargetType), ask.DedupeKey,
		int16(ask.Status), ask.CreatedAt, ask.UpdatedAt))
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality agent ask")
	}
	for _, request := range requests {
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE quality_agent_request
			SET status = $2, ask_id = $3, forwarded_at = COALESCE(forwarded_at, $4), updated_at = $4
			WHERE resource_id = $1 AND status IN ($5, $6)`,
			request.ResourceID, int16(QualityAgentRequestStatusForwarded), ask.ID, at.UTC(),
			int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded))
		if updateErr != nil {
			return nil, false, errors.Wrap(updateErr, "failed to forward a quality agent request")
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
			return nil, false, errors.Wrap(rowsErr, "failed to count the forwarded request")
		} else if affected == 0 {
			return nil, false, common.Errorf(common.Conflict, "request %s moved on before it could be forwarded", request.ResourceID)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, errors.Wrap(err, "failed to commit the ask")
	}
	return created, true, nil
}

// ResolveQualityAgentAskIfDrained closes an ask once no active request points at it.
func (s *Store) ResolveQualityAgentAskIfDrained(ctx context.Context, askID int64, at time.Time) (bool, error) {
	if askID <= 0 {
		return false, nil
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_ask
		SET status = $2, resolved_at = $3, updated_at = $3
		WHERE id = $1 AND status IN ($4, $5)
		  AND NOT EXISTS (
			SELECT 1 FROM quality_agent_request
			WHERE ask_id = $1 AND status IN ($6, $7)
		  )`,
		askID, int16(QualityAgentAskStatusResolved), at.UTC(),
		int16(QualityAgentAskStatusOpen), int16(QualityAgentAskStatusAnswered),
		int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded))
	if err != nil {
		return false, errors.Wrap(err, "failed to resolve the drained ask")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Wrap(err, "failed to count the drained asks")
	}
	return affected > 0, nil
}

// ResolveDrainedQualityAgentAsks closes every ask of an agent whose requests are all
// closed. Case transitions and answers call it after changing request state.
func (s *Store) ResolveDrainedQualityAgentAsks(ctx context.Context, agentID int64, at time.Time) (int64, error) {
	if agentID <= 0 {
		return 0, nil
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_ask a
		SET status = $2, resolved_at = $3, updated_at = $3
		WHERE a.agent_id = $1 AND a.status IN ($4, $5)
		  -- Only a request-backed ask can drain. An anomaly or approval ask has no
		  -- requests at all, and an unrelated case ending must not close it.
		  AND EXISTS (
			SELECT 1 FROM quality_agent_request owned WHERE owned.ask_id = a.id
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM quality_agent_request r
			WHERE r.ask_id = a.id AND r.status IN ($6, $7)
		  )`,
		agentID, int16(QualityAgentAskStatusResolved), at.UTC(),
		int16(QualityAgentAskStatusOpen), int16(QualityAgentAskStatusAnswered),
		int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded))
	if err != nil {
		return 0, errors.Wrap(err, "failed to resolve drained asks")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count drained asks")
	}
	return affected, nil
}
