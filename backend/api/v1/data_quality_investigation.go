package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityInvestigationPrefix = "qualityInvestigations/"
	maxInvestigationSummary    = 4000
	maxInvestigationHypotheses = 32
	maxInvestigationEvidence   = 64
	maxInvestigationActionRefs = 20
	maxInvestigationText       = 1000
)

// CreateQualityInvestigation records an agent's work product on one event. It never
// changes the check's verdict or an issue's state; it stores hypotheses, evidence
// references and a verdict, and requires at least one evidence reference so an
// investigation without evidence cannot pass as one.
func (s *DataQualityService) CreateQualityInvestigation(ctx context.Context, req *connect.Request[v1pb.CreateQualityInvestigationRequest]) (*connect.Response[v1pb.QualityInvestigation], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, err := s.authorizeAgentAction(ctx, dataquality.ActionWriteInvestigation, 0, 0)
	if err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's quality rules")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED && len(rules) > 0 {
		metaType = rules[0].TargetType
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, connectErrorInvalid("meta_type is required when the target has no rules")
	}
	contract, err := s.store.GetQualityContract(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	if agent != nil {
		if _, err := s.authorizeAgentAction(ctx, dataquality.ActionWriteInvestigation, 0,
			qualityChangeRequestCriticality(contract, rules)); err != nil {
			return nil, err
		}
	}
	input, err := validateQualityInvestigationRequest(req.Msg, targetGUID, metaType, currentUserID(ctx))
	if err != nil {
		return nil, err
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the investigation id")
	}
	input.ResourceID = resourceID
	message, _, err := s.store.CreateQualityInvestigation(ctx, input)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityInvestigationToV1(workspaceID, message)), nil
}

// GetQualityInvestigation reads one investigation.
func (s *DataQualityService) GetQualityInvestigation(ctx context.Context, req *connect.Request[v1pb.GetQualityInvestigationRequest]) (*connect.Response[v1pb.QualityInvestigation], error) {
	workspaceID, resourceID, err := s.qualityInvestigationResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	message, err := s.fetchQualityInvestigation(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityInvestigationToV1(workspaceID, message)), nil
}

// ListQualityInvestigations lists investigations newest first.
func (s *DataQualityService) ListQualityInvestigations(ctx context.Context, req *connect.Request[v1pb.ListQualityInvestigationsRequest]) (*connect.Response[v1pb.ListQualityInvestigationsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	limit := offset.limit + 1
	find := &store.FindQualityInvestigationMessage{Limit: &limit, Offset: &offset.offset}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	switch status := req.Msg.GetStatus(); status {
	case v1pb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_UNSPECIFIED:
	case v1pb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED,
		v1pb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_FAILED,
		v1pb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_INTERRUPTED:
		find.Statuses = []storepb.QualityInvestigationStatus{storepb.QualityInvestigationStatus(status)}
	default:
		return nil, connectErrorInvalid("unknown investigation status %d", status)
	}
	switch trigger := req.Msg.GetTrigger(); trigger {
	case v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_UNSPECIFIED:
	case v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_ISSUE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_SCHEMA_CHANGE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_COVERAGE_GAP,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_CONTRACT_DRIFT,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_MANUAL:
		find.Triggers = []storepb.QualityInvestigationTrigger{storepb.QualityInvestigationTrigger(trigger)}
	default:
		return nil, connectErrorInvalid("unknown investigation trigger %d", trigger)
	}
	messages, err := s.store.ListQualityInvestigations(ctx, find)
	if err != nil {
		return nil, err
	}
	messages, nextPageToken, err := paginate(messages, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityInvestigationsResponse{NextPageToken: nextPageToken}
	for _, message := range messages {
		response.QualityInvestigations = append(response.QualityInvestigations, convertQualityInvestigationToV1(workspaceID, message))
	}
	return connect.NewResponse(response), nil
}

// validateQualityInvestigationRequest validates the shape and the invariants: a
// concluded investigation needs a verdict and at least one evidence reference, and a
// failed one records why. It computes the idempotency fingerprint when the caller did
// not supply one.
func validateQualityInvestigationRequest(request *v1pb.CreateQualityInvestigationRequest, targetGUID string, metaType storepb.MetaType, createdBy int) (*store.CreateQualityInvestigationInput, error) {
	if request.GetTrigger() == v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_UNSPECIFIED {
		return nil, connectErrorInvalid("trigger is required")
	}
	if !validInvestigationTrigger(request.GetTrigger()) {
		return nil, connectErrorInvalid("unknown investigation trigger %d", request.GetTrigger())
	}
	triggerReference := strings.TrimSpace(request.GetTriggerReference())
	if triggerReference == "" || len(triggerReference) > maxInvestigationText {
		return nil, connectErrorInvalid("trigger_reference must be a non-empty reference (max %d bytes)", maxInvestigationText)
	}
	status := storepb.QualityInvestigationStatus(request.GetStatus())
	if status == storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_UNSPECIFIED {
		status = storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED
	}
	verdict := storepb.QualityInvestigationVerdict(request.GetVerdict())
	switch status {
	case storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_CONCLUDED:
		if verdict == storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_UNSPECIFIED {
			return nil, connectErrorInvalid("a concluded investigation needs a verdict")
		}
		if !validInvestigationVerdict(request.GetVerdict()) {
			return nil, connectErrorInvalid("unknown investigation verdict %d", request.GetVerdict())
		}
		if len(request.GetEvidence()) == 0 {
			return nil, connectErrorInvalid("a concluded investigation needs at least one evidence reference")
		}
	case storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_FAILED,
		storepb.QualityInvestigationStatus_QUALITY_INVESTIGATION_STATUS_INTERRUPTED:
		if strings.TrimSpace(request.GetFailureReason()) == "" {
			return nil, connectErrorInvalid("a failed or interrupted investigation needs a failure reason")
		}
		if verdict != storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_UNSPECIFIED {
			return nil, connectErrorInvalid("a failed or interrupted investigation cannot carry a verdict")
		}
	default:
		return nil, connectErrorInvalid("unknown investigation status %d", request.GetStatus())
	}
	summary := strings.TrimSpace(request.GetSummary())
	if len(summary) > maxInvestigationSummary {
		return nil, connectErrorInvalid("the investigation summary is too long (max %d bytes)", maxInvestigationSummary)
	}
	failureReason := strings.TrimSpace(request.GetFailureReason())
	if len(failureReason) > maxInvestigationText {
		return nil, connectErrorInvalid("the failure reason is too long (max %d bytes)", maxInvestigationText)
	}
	if len(request.GetHypotheses()) > maxInvestigationHypotheses {
		return nil, connectErrorInvalid("an investigation carries at most %d hypotheses", maxInvestigationHypotheses)
	}
	hypotheses := &storepb.QualityInvestigationHypothesisList{}
	for _, hypothesis := range request.GetHypotheses() {
		statement := strings.TrimSpace(hypothesis.GetStatement())
		if statement == "" || len(statement) > maxInvestigationText {
			return nil, connectErrorInvalid("a hypothesis needs a statement (max %d bytes)", maxInvestigationText)
		}
		switch strings.ToUpper(strings.TrimSpace(hypothesis.GetStatus())) {
		case "CONFIRMED", "REJECTED", "OPEN":
		default:
			return nil, connectErrorInvalid("a hypothesis status must be CONFIRMED, REJECTED or OPEN")
		}
		hypotheses.Hypotheses = append(hypotheses.Hypotheses, &storepb.QualityInvestigationHypothesis{
			Statement: statement,
			Status:    strings.ToUpper(strings.TrimSpace(hypothesis.GetStatus())),
		})
	}
	if len(request.GetEvidence()) > maxInvestigationEvidence {
		return nil, connectErrorInvalid("an investigation carries at most %d evidence references", maxInvestigationEvidence)
	}
	evidence := &storepb.QualityInvestigationEvidenceList{}
	for _, reference := range request.GetEvidence() {
		kind := strings.ToUpper(strings.TrimSpace(reference.GetKind()))
		value := strings.TrimSpace(reference.GetReference())
		if kind == "" || value == "" || len(kind) > 64 || len(value) > maxInvestigationText {
			return nil, connectErrorInvalid("an evidence reference needs a kind and a reference (max %d bytes)", maxInvestigationText)
		}
		evidence.Evidence = append(evidence.Evidence, &storepb.QualityInvestigationEvidence{
			Kind:      kind,
			Reference: value,
			Note:      strings.TrimSpace(reference.GetNote()),
		})
	}
	if len(request.GetActionRefs()) > maxInvestigationActionRefs {
		return nil, connectErrorInvalid("an investigation names at most %d action references", maxInvestigationActionRefs)
	}
	actionRefs := &storepb.QualityInvestigationActionRefs{}
	for _, ref := range request.GetActionRefs() {
		if ref = strings.TrimSpace(ref); ref != "" {
			actionRefs.Refs = append(actionRefs.Refs, ref)
		}
	}
	if request.GetPromptTokens() < 0 || request.GetCompletionTokens() < 0 {
		return nil, connectErrorInvalid("token counts cannot be negative")
	}
	fingerprint := strings.TrimSpace(request.GetRequestFingerprint())
	if fingerprint == "" {
		fingerprint = qualityInvestigationFingerprint(targetGUID, metaType, request.GetTrigger(),
			triggerReference, status, verdict, summary, hypotheses, evidence, actionRefs)
	}
	return &store.CreateQualityInvestigationInput{
		TargetGUID:         targetGUID,
		TargetType:         metaType,
		Trigger:            storepb.QualityInvestigationTrigger(request.GetTrigger()),
		TriggerReference:   triggerReference,
		Status:             status,
		Verdict:            verdict,
		Summary:            summary,
		Hypotheses:         hypotheses,
		Evidence:           evidence,
		ActionRefs:         actionRefs,
		Provider:           strings.TrimSpace(request.GetProvider()),
		Model:              strings.TrimSpace(request.GetModel()),
		PromptVersion:      strings.TrimSpace(request.GetPromptVersion()),
		PromptTokens:       request.GetPromptTokens(),
		CompletionTokens:   request.GetCompletionTokens(),
		FailureReason:      failureReason,
		RequestFingerprint: fingerprint,
		CreatedBy:          principalIDPointer(createdBy),
	}, nil
}

func validInvestigationTrigger(trigger v1pb.QualityInvestigationTrigger) bool {
	switch trigger {
	case v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_ISSUE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_SCHEMA_CHANGE,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_COVERAGE_GAP,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_CONTRACT_DRIFT,
		v1pb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_MANUAL:
		return true
	default:
		return false
	}
}

func validInvestigationVerdict(verdict v1pb.QualityInvestigationVerdict) bool {
	switch verdict {
	case v1pb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_ROOT_CAUSE,
		v1pb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_CONTRIBUTING_FACTOR,
		v1pb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE,
		v1pb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_UNRELATED:
		return true
	default:
		return false
	}
}

// qualityInvestigationFingerprint is the deterministic idempotency key of one
// recorded investigation, so a retried agent run returns the same work product.
func qualityInvestigationFingerprint(targetGUID string, metaType storepb.MetaType, trigger v1pb.QualityInvestigationTrigger,
	triggerReference string, status storepb.QualityInvestigationStatus, verdict storepb.QualityInvestigationVerdict,
	summary string, hypotheses *storepb.QualityInvestigationHypothesisList,
	evidence *storepb.QualityInvestigationEvidenceList, actionRefs *storepb.QualityInvestigationActionRefs) string {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&storepb.QualityInvestigation{
		Target:           &storepb.QualityTargetPath{Object: targetGUID},
		Trigger:          storepb.QualityInvestigationTrigger(trigger),
		TriggerReference: triggerReference,
		Status:           status,
		Verdict:          verdict,
		Summary:          summary,
		Hypotheses:       hypotheses.GetHypotheses(),
		Evidence:         evidence.GetEvidence(),
		ActionRefs:       actionRefs.GetRefs(),
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("investigation|"+strconv.Itoa(int(metaType))+"|"), payload...))
	return "inv:" + hex.EncodeToString(sum[:])
}

// qualityInvestigationResourceID resolves an investigation name to its workspace and
// resource id.
func (s *DataQualityService) qualityInvestigationResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityInvestigationPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality investigation name")
	}
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

func (s *DataQualityService) fetchQualityInvestigation(ctx context.Context, resourceID string) (*store.QualityInvestigationMessage, error) {
	message, err := s.store.GetQualityInvestigation(ctx, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality investigation")
	}
	if message == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality investigation %q not found", resourceID))
	}
	return message, nil
}

// convertQualityInvestigationToV1 renders one investigation.
func convertQualityInvestigationToV1(workspaceID string, message *store.QualityInvestigationMessage) *v1pb.QualityInvestigation {
	if message == nil {
		return nil
	}
	converted := &v1pb.QualityInvestigation{
		Name:             common.FormatWorkspace(workspaceID) + "/" + qualityInvestigationPrefix + message.ResourceID,
		Target:           &v1pb.QualityTarget{MetaGuid: message.TargetGUID, MetaType: v1pb.MetaType(message.TargetType)},
		Trigger:          v1pb.QualityInvestigationTrigger(message.Trigger),
		TriggerReference: message.TriggerReference,
		Status:           v1pb.QualityInvestigationStatus(message.Status),
		Verdict:          v1pb.QualityInvestigationVerdict(message.Verdict),
		Summary:          message.Summary,
		ActionRefs:       append([]string(nil), message.ActionRefs.GetRefs()...),
		Provider:         message.Provider,
		Model:            message.Model,
		PromptVersion:    message.PromptVersion,
		PromptTokens:     message.PromptTokens,
		CompletionTokens: message.CompletionTokens,
		FailureReason:    message.FailureReason,
		CreatedBy:        formatQualityContractPublisher(message.CreatedBy),
		CreateTime:       timestamppb.New(message.CreatedAt),
	}
	for _, hypothesis := range message.Hypotheses.GetHypotheses() {
		converted.Hypotheses = append(converted.Hypotheses, &v1pb.QualityInvestigationHypothesis{
			Statement: hypothesis.GetStatement(),
			Status:    hypothesis.GetStatus(),
		})
	}
	for _, reference := range message.Evidence.GetEvidence() {
		converted.Evidence = append(converted.Evidence, &v1pb.QualityInvestigationEvidence{
			Kind:      reference.GetKind(),
			Reference: reference.GetReference(),
			Note:      reference.GetNote(),
		})
	}
	return converted
}
