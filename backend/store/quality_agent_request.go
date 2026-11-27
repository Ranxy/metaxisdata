package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentRequestKind is why a subagent needs the lead.
type QualityAgentRequestKind int16

const (
	QualityAgentRequestKindDecision QualityAgentRequestKind = 1
	QualityAgentRequestKindProposal QualityAgentRequestKind = 2
	QualityAgentRequestKindBlocked  QualityAgentRequestKind = 3
	QualityAgentRequestKindFrozen   QualityAgentRequestKind = 4
	QualityAgentRequestKindAnomaly  QualityAgentRequestKind = 5
	QualityAgentRequestKindQuestion QualityAgentRequestKind = 6
)

// QualityAgentRequestStatus is the request's lifecycle.
type QualityAgentRequestStatus int16

const (
	QualityAgentRequestStatusOpen      QualityAgentRequestStatus = 1
	QualityAgentRequestStatusForwarded QualityAgentRequestStatus = 2
	QualityAgentRequestStatusAnswered  QualityAgentRequestStatus = 3
	QualityAgentRequestStatusResolved  QualityAgentRequestStatus = 4
	QualityAgentRequestStatusDismissed QualityAgentRequestStatus = 5
)

// QualityAgentRequestMessage is one subagent decision request to the lead.
type QualityAgentRequestMessage struct {
	ID              int64
	ResourceID      string
	WorkspaceID     string
	AgentID         int64
	OriginSessionID *int64
	TargetGUID      string
	TargetType      storepb.MetaType
	Kind            QualityAgentRequestKind
	Status          QualityAgentRequestStatus
	Title           string
	Question        string
	CaseResourceID  string
	SuggestedPatch  *storepb.QualityAgentFieldPatch
	DedupeKey       string
	AskID           *int64
	Resolution      string
	ResolvedBy      *int32
	ForwardedAt     *time.Time
	ResolvedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const qualityAgentRequestColumns = `id, resource_id, workspace_id, agent_id,
	origin_session_id, target_guid, target_type, kind, status, title, question,
	case_resource_id, suggested_patch, dedupe_key, ask_id, resolution, resolved_by,
	forwarded_at, resolved_at, created_at, updated_at`

// CreateQualityAgentRequest inserts one active request. A retried escalation of the
// same (agent, dedupe_key) returns the existing OPEN/FORWARDED row.
func (s *Store) CreateQualityAgentRequest(ctx context.Context, message *QualityAgentRequestMessage) (*QualityAgentRequestMessage, bool, error) {
	if message == nil || message.AgentID <= 0 || message.WorkspaceID == "" || message.DedupeKey == "" {
		return nil, false, common.Errorf(common.Invalid, "a request needs an agent, workspace and dedupe key")
	}
	if message.Kind == 0 {
		message.Kind = QualityAgentRequestKindDecision
	}
	if message.Status == 0 {
		message.Status = QualityAgentRequestStatusOpen
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
			return nil, false, errors.Wrap(err, "failed to generate the request id")
		}
	}
	var patchJSON []byte
	if message.SuggestedPatch != nil {
		encoded, marshalErr := protojson.Marshal(message.SuggestedPatch)
		if marshalErr != nil {
			return nil, false, errors.Wrap(marshalErr, "failed to marshal a suggested patch")
		}
		patchJSON = encoded
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_agent_request
			(resource_id, workspace_id, agent_id, origin_session_id, target_guid, target_type,
			 kind, status, title, question, case_resource_id, suggested_patch, dedupe_key,
			 ask_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (agent_id, dedupe_key) WHERE status IN (1, 2) DO NOTHING
		RETURNING `+qualityAgentRequestColumns,
		resourceID, message.WorkspaceID, message.AgentID, message.OriginSessionID,
		message.TargetGUID, int16(message.TargetType), int16(message.Kind), int16(message.Status),
		message.Title, message.Question, message.CaseResourceID, patchJSON, message.DedupeKey,
		message.AskID, message.CreatedAt, message.UpdatedAt)
	created, err := scanQualityAgentRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		existing, readErr := s.getActiveQualityAgentRequestByDedupe(ctx, message.AgentID, message.DedupeKey)
		if readErr != nil {
			return nil, false, readErr
		}
		if existing == nil {
			return nil, false, errors.New("the request conflicted but no active row was found")
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality agent request")
	}
	return created, true, nil
}

// GetQualityAgentRequestByResourceID reads one request; nil means absent.
func (s *Store) GetQualityAgentRequestByResourceID(ctx context.Context, resourceID string) (*QualityAgentRequestMessage, error) {
	message, err := scanQualityAgentRequest(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentRequestColumns+` FROM quality_agent_request WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality agent request")
	}
	return message, nil
}

// ListQualityAgentRequests lists an agent's requests, newest first.
func (s *Store) ListQualityAgentRequests(ctx context.Context, agentID int64, statuses []QualityAgentRequestStatus, targetGUID string, limit int) ([]*QualityAgentRequestMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + qualityAgentRequestColumns + ` FROM quality_agent_request WHERE agent_id = $1`
	args := []any{agentID}
	if targetGUID != "" {
		args = append(args, targetGUID)
		query += ` AND target_guid = $` + strconv.Itoa(len(args))
	}
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
		return nil, errors.Wrap(err, "failed to list the quality agent requests")
	}
	defer rows.Close()
	var messages []*QualityAgentRequestMessage
	for rows.Next() {
		message, err := scanQualityAgentRequest(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality agent request")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality agent requests")
	}
	return messages, nil
}

// UpdateQualityAgentRequestStatus applies a compare-and-set status change. It returns
// applied=false when the request moved on, so a retried reply does not route twice.
func (s *Store) UpdateQualityAgentRequestStatus(ctx context.Context, resourceID string, from, to QualityAgentRequestStatus, askID *int64, resolution string, resolvedBy *int32, at time.Time) (*QualityAgentRequestMessage, bool, error) {
	message, err := scanQualityAgentRequest(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_agent_request
		SET status = $3, ask_id = COALESCE($4, ask_id), resolution = $5,
			resolved_by = $6, forwarded_at = CASE WHEN $3 = $7 THEN COALESCE(forwarded_at, $8) ELSE forwarded_at END,
			resolved_at = CASE WHEN $3 IN ($9, $10) THEN $8 ELSE resolved_at END,
			updated_at = $8
		WHERE resource_id = $1 AND status = $2
		RETURNING `+qualityAgentRequestColumns,
		resourceID, int16(from), int16(to), askID, resolution, resolvedBy, int16(QualityAgentRequestStatusForwarded), at.UTC(),
		int16(QualityAgentRequestStatusResolved), int16(QualityAgentRequestStatusDismissed)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "failed to update the quality agent request")
	}
	return message, true, nil
}

// ResolveQualityAgentRequestsForCase closes active requests attached to one case.
func (s *Store) ResolveQualityAgentRequestsForCase(ctx context.Context, agentID int64, caseResourceID string, at time.Time) (int64, error) {
	if agentID <= 0 || caseResourceID == "" {
		return 0, nil
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_agent_request
		SET status = $3, resolved_at = $4, updated_at = $4
		WHERE agent_id = $1 AND case_resource_id = $2 AND status IN ($5, $6)`,
		agentID, caseResourceID, int16(QualityAgentRequestStatusResolved), at.UTC(),
		int16(QualityAgentRequestStatusOpen), int16(QualityAgentRequestStatusForwarded))
	if err != nil {
		return 0, errors.Wrap(err, "failed to resolve the case requests")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the resolved case requests")
	}
	return affected, nil
}

func (s *Store) getActiveQualityAgentRequestByDedupe(ctx context.Context, agentID int64, dedupeKey string) (*QualityAgentRequestMessage, error) {
	message, err := scanQualityAgentRequest(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityAgentRequestColumns+` FROM quality_agent_request
		 WHERE agent_id = $1 AND dedupe_key = $2 AND status IN (1, 2)`, agentID, dedupeKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the active quality agent request")
	}
	return message, nil
}

func scanQualityAgentRequest(scanner interface{ Scan(dest ...any) error }) (*QualityAgentRequestMessage, error) {
	var (
		message    QualityAgentRequestMessage
		targetType int16
		kind       int16
		status     int16
		originID   sql.NullInt64
		askID      sql.NullInt64
		resolvedBy sql.NullInt32
		forwarded  sql.NullTime
		resolved   sql.NullTime
		patch      []byte
	)
	err := scanner.Scan(
		&message.ID, &message.ResourceID, &message.WorkspaceID, &message.AgentID,
		&originID, &message.TargetGUID, &targetType, &kind, &status, &message.Title,
		&message.Question, &message.CaseResourceID, &patch, &message.DedupeKey, &askID,
		&message.Resolution, &resolvedBy, &forwarded, &resolved, &message.CreatedAt, &message.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	message.TargetType = storepb.MetaType(targetType)
	message.Kind = QualityAgentRequestKind(kind)
	message.Status = QualityAgentRequestStatus(status)
	if originID.Valid {
		value := originID.Int64
		message.OriginSessionID = &value
	}
	if askID.Valid {
		value := askID.Int64
		message.AskID = &value
	}
	if resolvedBy.Valid {
		value := resolvedBy.Int32
		message.ResolvedBy = &value
	}
	if forwarded.Valid {
		value := forwarded.Time
		message.ForwardedAt = &value
	}
	if resolved.Valid {
		value := resolved.Time
		message.ResolvedAt = &value
	}
	if len(patch) > 0 {
		decoded := &storepb.QualityAgentFieldPatch{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(patch, decoded); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal a suggested patch")
		}
		message.SuggestedPatch = decoded
	}
	return &message, nil
}
