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

// QualityInvestigationMessage is one agent's work product on one event. The evidence
// is references, never sample payloads; the investigation never changes a check's
// verdict or an issue's state.
type QualityInvestigationMessage struct {
	ID                 int64
	ResourceID         string
	TargetGUID         string
	TargetType         storepb.MetaType
	Trigger            storepb.QualityInvestigationTrigger
	TriggerReference   string
	Status             storepb.QualityInvestigationStatus
	Verdict            storepb.QualityInvestigationVerdict
	Summary            string
	Hypotheses         *storepb.QualityInvestigationHypothesisList
	Evidence           *storepb.QualityInvestigationEvidenceList
	ActionRefs         *storepb.QualityInvestigationActionRefs
	Provider           string
	Model              string
	PromptVersion      string
	PromptTokens       int64
	CompletionTokens   int64
	FailureReason      string
	RequestFingerprint string
	CreatedBy          *int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const qualityInvestigationColumns = `id, resource_id, target_guid, target_type, trigger,
	trigger_reference, status, verdict, summary, hypotheses, evidence, action_refs,
	provider, model, prompt_version, prompt_tokens, completion_tokens, failure_reason,
	request_fingerprint, created_by, created_at, updated_at`

// CreateQualityInvestigationInput is one record attempt. A non-empty fingerprint
// makes the write idempotent for (target, type, fingerprint).
type CreateQualityInvestigationInput struct {
	ResourceID         string
	TargetGUID         string
	TargetType         storepb.MetaType
	Trigger            storepb.QualityInvestigationTrigger
	TriggerReference   string
	Status             storepb.QualityInvestigationStatus
	Verdict            storepb.QualityInvestigationVerdict
	Summary            string
	Hypotheses         *storepb.QualityInvestigationHypothesisList
	Evidence           *storepb.QualityInvestigationEvidenceList
	ActionRefs         *storepb.QualityInvestigationActionRefs
	Provider           string
	Model              string
	PromptVersion      string
	PromptTokens       int64
	CompletionTokens   int64
	FailureReason      string
	RequestFingerprint string
	CreatedBy          *int32
}

// CreateQualityInvestigation records one investigation, or returns the existing one
// when the idempotency key already has a row.
func (s *Store) CreateQualityInvestigation(ctx context.Context, input *CreateQualityInvestigationInput) (*QualityInvestigationMessage, bool, error) {
	if input == nil || input.ResourceID == "" {
		return nil, false, common.Errorf(common.Invalid, "a resource id is required")
	}
	if input.TargetGUID == "" {
		return nil, false, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if input.Status == storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_UNSPECIFIED {
		input.Status = storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED
	}
	if input.Hypotheses == nil {
		input.Hypotheses = &storepb.QualityInvestigationHypothesisList{}
	}
	if input.Evidence == nil {
		input.Evidence = &storepb.QualityInvestigationEvidenceList{}
	}
	if input.ActionRefs == nil {
		input.ActionRefs = &storepb.QualityInvestigationActionRefs{}
	}
	hypotheses, err := protojson.Marshal(input.Hypotheses)
	if err != nil {
		return nil, false, err
	}
	evidence, err := protojson.Marshal(input.Evidence)
	if err != nil {
		return nil, false, err
	}
	actionRefs, err := protojson.Marshal(input.ActionRefs)
	if err != nil {
		return nil, false, err
	}
	row := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO quality_investigation
			(resource_id, target_guid, target_type, trigger, trigger_reference, status, verdict,
			 summary, hypotheses, evidence, action_refs, provider, model, prompt_version,
			 prompt_tokens, completion_tokens, failure_reason, request_fingerprint, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (target_guid, target_type, request_fingerprint)
			WHERE request_fingerprint <> '' DO UPDATE SET updated_at = quality_investigation.updated_at
		RETURNING `+qualityInvestigationColumns,
		input.ResourceID, input.TargetGUID, int16(input.TargetType), int16(input.Trigger),
		input.TriggerReference, int16(input.Status), int16(input.Verdict), input.Summary,
		hypotheses, evidence, actionRefs, input.Provider, input.Model, input.PromptVersion,
		input.PromptTokens, input.CompletionTokens, input.FailureReason,
		input.RequestFingerprint, input.CreatedBy)
	message, err := scanQualityInvestigation(row)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to create the quality investigation")
	}
	return message, message.ResourceID == input.ResourceID, nil
}

// GetQualityInvestigation reads one investigation; nil means it does not exist.
func (s *Store) GetQualityInvestigation(ctx context.Context, resourceID string) (*QualityInvestigationMessage, error) {
	message, err := scanQualityInvestigation(s.GetDB().QueryRowContext(ctx,
		`SELECT `+qualityInvestigationColumns+` FROM quality_investigation WHERE resource_id = $1`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the quality investigation")
	}
	return message, nil
}

// FindQualityInvestigationMessage filters a list; every field is optional.
type FindQualityInvestigationMessage struct {
	TargetGUID *string
	Statuses   []storepb.QualityInvestigationStatus
	Triggers   []storepb.QualityInvestigationTrigger
	Limit      *int
	Offset     *int
}

// ListQualityInvestigations lists investigations newest first.
func (s *Store) ListQualityInvestigations(ctx context.Context, find *FindQualityInvestigationMessage) ([]*QualityInvestigationMessage, error) {
	if find == nil {
		find = &FindQualityInvestigationMessage{}
	}
	where := "TRUE"
	args := []any{}
	if find.TargetGUID != nil {
		args = append(args, *find.TargetGUID)
		where += ` AND target_guid = $1`
	}
	if len(find.Statuses) > 0 {
		statuses := make([]int16, 0, len(find.Statuses))
		for _, status := range find.Statuses {
			statuses = append(statuses, int16(status))
		}
		args = append(args, statuses)
		where += ` AND status = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	if len(find.Triggers) > 0 {
		triggers := make([]int16, 0, len(find.Triggers))
		for _, trigger := range find.Triggers {
			triggers = append(triggers, int16(trigger))
		}
		args = append(args, triggers)
		where += ` AND "trigger" = ANY($` + strconv.Itoa(len(args)) + `)`
	}
	limit, offset := 100, 0
	if find.Limit != nil && *find.Limit > 0 {
		limit = *find.Limit
	}
	if find.Offset != nil && *find.Offset > 0 {
		offset = *find.Offset
	}
	args = append(args, limit, offset)
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+qualityInvestigationColumns+`
		FROM quality_investigation WHERE `+where+`
		ORDER BY created_at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality investigations")
	}
	defer rows.Close()
	var messages []*QualityInvestigationMessage
	for rows.Next() {
		message, err := scanQualityInvestigation(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality investigation")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality investigations")
	}
	return messages, nil
}

// scanQualityInvestigation reads one quality_investigation row.
func scanQualityInvestigation(scanner interface{ Scan(dest ...any) error }) (*QualityInvestigationMessage, error) {
	var (
		message    QualityInvestigationMessage
		targetType int16
		trigger    int16
		status     int16
		verdict    int16
		hypotheses []byte
		evidence   []byte
		actionRefs []byte
		createdBy  sql.NullInt32
	)
	if err := scanner.Scan(&message.ID, &message.ResourceID, &message.TargetGUID, &targetType,
		&trigger, &message.TriggerReference, &status, &verdict, &message.Summary,
		&hypotheses, &evidence, &actionRefs, &message.Provider, &message.Model,
		&message.PromptVersion, &message.PromptTokens, &message.CompletionTokens,
		&message.FailureReason, &message.RequestFingerprint, &createdBy,
		&message.CreatedAt, &message.UpdatedAt); err != nil {
		return nil, err
	}
	message.TargetType = storepb.MetaType(targetType)
	message.Trigger = storepb.QualityInvestigationTrigger(trigger)
	message.Status = storepb.QualityInvestigationStatus(status)
	message.Verdict = storepb.QualityInvestigationVerdict(verdict)
	message.Hypotheses = &storepb.QualityInvestigationHypothesisList{}
	if len(hypotheses) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(hypotheses, message.Hypotheses); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the investigation hypotheses")
		}
	}
	message.Evidence = &storepb.QualityInvestigationEvidenceList{}
	if len(evidence) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(evidence, message.Evidence); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the investigation evidence")
		}
	}
	message.ActionRefs = &storepb.QualityInvestigationActionRefs{}
	if len(actionRefs) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(actionRefs, message.ActionRefs); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal the investigation action refs")
		}
	}
	if createdBy.Valid {
		id := createdBy.Int32
		message.CreatedBy = &id
	}
	return &message, nil
}
