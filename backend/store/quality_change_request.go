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

// QualityChangeRequestMessage is one approve-or-reject batch of typed changes
// against one target, with the decision and the per-item application results.
type QualityChangeRequestMessage struct {
	ID                 int64
	ResourceID         string
	TargetGUID         string
	TargetType         storepb.MetaType
	State              storepb.QualityChangeRequestState
	BaseVersion        int32
	Source             storepb.QualityChangeRequestSource
	Items              *storepb.QualityChangeRequestItems
	ItemResults        *storepb.QualityChangeRequestItemResults
	RequestFingerprint string
	RejectionReasons   []storepb.QualityChangeRejectionReason
	DecisionNote       string
	CreatedBy          *int32
	DecidedBy          *int32
	DecidedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const qualityChangeRequestColumns = `id, resource_id, target_guid, target_type, state, base_version,
	source, items, item_results, request_fingerprint, rationale_codes, decision_note,
	created_by, decided_by, decided_at, created_at, updated_at`

// CreateQualityChangeRequestInput is one create attempt. The idempotency key is
// (target, type, base version, source, fingerprint): a retry returns the existing
// request instead of writing a second one.
type CreateQualityChangeRequestInput struct {
	ResourceID         string
	TargetGUID         string
	TargetType         storepb.MetaType
	BaseVersion        int32
	Source             storepb.QualityChangeRequestSource
	Items              *storepb.QualityChangeRequestItems
	RequestFingerprint string
	CreatedBy          *int32
}

// CreateQualityChangeRequest records a proposed request, or returns the existing
// one with created=false when a request with the same idempotency key exists.
func (s *Store) CreateQualityChangeRequest(ctx context.Context, input *CreateQualityChangeRequestInput) (*QualityChangeRequestMessage, bool, error) {
	if input == nil || input.ResourceID == "" {
		return nil, false, common.Errorf(common.Invalid, "a resource id is required")
	}
	if input.TargetGUID == "" {
		return nil, false, common.Errorf(common.Invalid, "a target GUID is required")
	}
	items := input.Items
	if items == nil {
		items = &storepb.QualityChangeRequestItems{}
	}
	itemsPayload, err := protojson.Marshal(items)
	if err != nil {
		return nil, false, err
	}
	resultsPayload, err := protojson.Marshal(&storepb.QualityChangeRequestItemResults{})
	if err != nil {
		return nil, false, err
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_change_request
			(resource_id, target_guid, target_type, state, base_version, source, items,
			 item_results, request_fingerprint, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (target_guid, target_type, base_version, source, request_fingerprint)
			DO UPDATE SET updated_at = quality_change_request.updated_at
		RETURNING `+qualityChangeRequestColumns,
		input.ResourceID, input.TargetGUID, int16(input.TargetType),
		int16(storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_PROPOSED),
		input.BaseVersion, int16(input.Source), itemsPayload, resultsPayload,
		input.RequestFingerprint, input.CreatedBy)
	message, err := scanQualityChangeRequest(row)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality change request")
	}
	return message, message.ResourceID == input.ResourceID, nil
}

// GetQualityChangeRequest reads one request by its resource id.
func (s *Store) GetQualityChangeRequest(ctx context.Context, resourceID string) (*QualityChangeRequestMessage, error) {
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "a resource id is required")
	}
	message, err := scanQualityChangeRequest(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityChangeRequestColumns+` FROM quality_change_request WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality change request")
	}
	return message, nil
}

// FindQualityChangeRequestMessage filters a list; every field is optional.
type FindQualityChangeRequestMessage struct {
	TargetGUID *string
	States     []storepb.QualityChangeRequestState
	Limit      *int
	Offset     *int
}

// ListQualityChangeRequests lists requests newest first.
func (s *Store) ListQualityChangeRequests(ctx context.Context, find *FindQualityChangeRequestMessage) ([]*QualityChangeRequestMessage, error) {
	if find == nil {
		find = &FindQualityChangeRequestMessage{}
	}
	where := "TRUE"
	args := []any{}
	if find.TargetGUID != nil {
		args = append(args, *find.TargetGUID)
		where += ` AND target_guid = $` + strconv.Itoa(len(args))
	}
	if len(find.States) > 0 {
		states := make([]int16, 0, len(find.States))
		for _, state := range find.States {
			states = append(states, int16(state))
		}
		args = append(args, states)
		where += ` AND state = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	args = append(args, find.limitOr(100), find.offsetOr(0))
	query := `SELECT ` + qualityChangeRequestColumns + ` FROM quality_change_request
		WHERE ` + where + ` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality change requests")
	}
	defer rows.Close()

	messages := []*QualityChangeRequestMessage{}
	for rows.Next() {
		message, err := scanQualityChangeRequest(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality change request")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality change requests")
	}
	return messages, nil
}

// DecisionQualityChangeRequestInput is one state transition. ExpectedState and
// ExpectedItemResults make the write a compare-and-set: two decisions on the same
// request cannot both apply their items.
type DecisionQualityChangeRequestInput struct {
	ResourceID          string
	ExpectedState       storepb.QualityChangeRequestState
	ExpectedItemResults *storepb.QualityChangeRequestItemResults
	State               storepb.QualityChangeRequestState
	ItemResults         *storepb.QualityChangeRequestItemResults
	RejectionReasons    []storepb.QualityChangeRejectionReason
	DecisionNote        string
	DecidedBy           *int32
}

// DecideQualityChangeRequest records a decision under a compare-and-set on the
// state and results the caller read. A zero-row update means another decision
// won; the caller reloads instead of applying items twice.
func (s *Store) DecideQualityChangeRequest(ctx context.Context, input *DecisionQualityChangeRequestInput) (*QualityChangeRequestMessage, error) {
	if input == nil || input.ResourceID == "" {
		return nil, common.Errorf(common.Invalid, "a resource id is required")
	}
	results := input.ItemResults
	if results == nil {
		results = &storepb.QualityChangeRequestItemResults{}
	}
	resultsPayload, err := protojson.Marshal(results)
	if err != nil {
		return nil, err
	}
	expectedResults := input.ExpectedItemResults
	if expectedResults == nil {
		expectedResults = &storepb.QualityChangeRequestItemResults{}
	}
	expectedPayload, err := protojson.Marshal(expectedResults)
	if err != nil {
		return nil, err
	}
	reasons := &storepb.QualityChangeRejectionReasons{Reasons: input.RejectionReasons}
	reasonsPayload, err := protojson.Marshal(reasons)
	if err != nil {
		return nil, err
	}
	row := s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_change_request
		SET state = $3, item_results = $4, rationale_codes = $5, decision_note = $6,
			decided_by = $7, decided_at = NOW(), updated_at = NOW()
		WHERE resource_id = $1 AND state = $2 AND item_results = $8::jsonb
		RETURNING `+qualityChangeRequestColumns,
		input.ResourceID, int16(input.ExpectedState), int16(input.State), resultsPayload,
		reasonsPayload, input.DecisionNote, input.DecidedBy, expectedPayload)
	message, err := scanQualityChangeRequest(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.Conflict, "the quality change request changed; reload and decide again")
		}
		return nil, errors.Wrap(err, "failed to record the quality change request decision")
	}
	return message, nil
}

// scanQualityChangeRequest reads one quality_change_request row.
func scanQualityChangeRequest(scanner interface{ Scan(dest ...any) error }) (*QualityChangeRequestMessage, error) {
	var (
		message     QualityChangeRequestMessage
		targetType  int16
		state       int16
		source      int16
		itemsJSON   []byte
		resultsJSON []byte
		reasonsJSON []byte
		createdBy   sql.NullInt32
		decidedBy   sql.NullInt32
		decidedAt   sql.NullTime
	)
	if err := scanner.Scan(
		&message.ID, &message.ResourceID, &message.TargetGUID, &targetType, &state,
		&message.BaseVersion, &source, &itemsJSON, &resultsJSON, &message.RequestFingerprint,
		&reasonsJSON, &message.DecisionNote, &createdBy, &decidedBy, &decidedAt,
		&message.CreatedAt, &message.UpdatedAt,
	); err != nil {
		return nil, err
	}
	message.TargetType = storepb.MetaType(targetType)
	message.State = storepb.QualityChangeRequestState(state)
	message.Source = storepb.QualityChangeRequestSource(source)
	items := &storepb.QualityChangeRequestItems{}
	if len(itemsJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(itemsJSON, items); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the quality change request items")
		}
	}
	message.Items = items
	results := &storepb.QualityChangeRequestItemResults{}
	if len(resultsJSON) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(resultsJSON, results); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the quality change request item results")
		}
	}
	message.ItemResults = results
	if len(reasonsJSON) > 0 {
		reasons := &storepb.QualityChangeRejectionReasons{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(reasonsJSON, reasons); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the quality change request rejection reasons")
		}
		message.RejectionReasons = reasons.GetReasons()
	}
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
	}
	if decidedBy.Valid {
		id := decidedBy.Int32
		message.DecidedBy = &id
	}
	if decidedAt.Valid {
		at := decidedAt.Time
		message.DecidedAt = &at
	}
	return &message, nil
}

func (find *FindQualityChangeRequestMessage) limitOr(fallback int) int {
	if find.Limit != nil && *find.Limit > 0 {
		return *find.Limit
	}
	return fallback
}

func (find *FindQualityChangeRequestMessage) offsetOr(fallback int) int {
	if find.Offset != nil && *find.Offset > 0 {
		return *find.Offset
	}
	return fallback
}
