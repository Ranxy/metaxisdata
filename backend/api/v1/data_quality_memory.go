package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityMemoryPrefix   = "qualityMemories/"
	maxMemorySubject      = 500
	maxMemoryStatement    = 4000
	maxMemoryEvidence     = 32
	maxMemoryDomainRef    = 200
	maxMemoryEvidenceText = 1000
)

// CreateQualityMemory records durable knowledge. An agent may write an asset fact
// directly; a decision or an event it writes stays PROPOSED until a person confirms
// it. Every memory needs at least one evidence reference and a subject, so it can
// always answer who, when and based on what.
func (s *DataQualityService) CreateQualityMemory(ctx context.Context, req *connect.Request[v1pb.CreateQualityMemoryRequest]) (*connect.Response[v1pb.QualityMemory], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	kind := storepb.QualityMemoryKind(req.Msg.GetKind())
	action, err := qualityMemoryAction(kind)
	if err != nil {
		return nil, err
	}
	agent, err := s.authorizeAgentAction(ctx, action, 0, 0)
	if err != nil {
		return nil, err
	}
	source := storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_HUMAN
	status := storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED
	var confirmedBy *int32
	if agent != nil {
		source = storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT
		if kind != storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT {
			// A decision or an event is a judgment: the agent proposes, a person confirms.
			status = storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_PROPOSED
		}
		// The server cannot verify a reference, so only evidence shapes a deterministic
		// observation produces may auto-confirm a fact; anything else is recorded as a
		// proposal for a person to judge.
		if !observedEvidence(req.Msg.GetEvidence()) {
			status = storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_PROPOSED
		}
	} else if id := currentUserID(ctx); id > 0 {
		confirmedBy = principalIDPointer(id)
	}
	input, err := validateQualityMemoryRequest(req.Msg, source, status, confirmedBy)
	if err != nil {
		return nil, err
	}
	// The criticality amplifier must see the real asset: a critical subject's judged
	// memory (A1-6) is handed to a person, still before the write.
	if agent != nil && kind != storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT {
		_, err := s.authorizeAgentAction(ctx, action, 0,
			memorySubjectCriticality(ctx, s.store, input.Subject))
		if err != nil {
			return nil, err
		}
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the memory id")
	}
	input.ResourceID = resourceID
	input.CreatedBy = principalIDPointer(currentUserID(ctx))
	message, err := s.store.CreateQualityMemory(ctx, input)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityMemoryToV1(workspaceID, message)), nil
}

// observedEvidenceKinds are the evidence shapes a deterministic observation produces.
// A fact whose evidence claims anything else is not auto-confirmed: the server has not
// verified it, so a person decides.
var observedEvidenceKinds = map[string]bool{
	"EXECUTION": true, "PROFILE": true, "LINEAGE": true, "SCHEMA_FINGERPRINT": true,
	// The plan is a deterministic read of the contract and live rules, so a coverage
	// observation backed by it is a fact the runner may record as confirmed.
	"PLAN": true,
}

func observedEvidence(evidence []*v1pb.QualityInvestigationEvidence) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, reference := range evidence {
		if !observedEvidenceKinds[strings.ToUpper(strings.TrimSpace(reference.GetKind()))] {
			return false
		}
	}
	return true
}

// memorySubjectCriticality resolves the plan criticality of a target subject, or zero
// when the subject is not a target GUID. It never writes.
func memorySubjectCriticality(ctx context.Context, s *store.Store, subject string) dataquality.AssetCriticality {
	if !strings.Contains(subject, ";") {
		return 0
	}
	rules, err := s.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &subject})
	if err != nil || len(rules) == 0 {
		return 0
	}
	contract, err := s.GetQualityContract(ctx, subject, rules[0].TargetType)
	if err != nil {
		return 0
	}
	return qualityChangeRequestCriticality(contract, rules)
}

// qualityMemoryAction maps a memory kind onto the WS2 action it performs. An asset
// fact is an observation (A0-5); a decision or an event is a judgement (A1-6).
func qualityMemoryAction(kind storepb.QualityMemoryKind) (dataquality.ActionID, error) {
	switch kind {
	case storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT:
		return dataquality.ActionWriteAssetFact, nil
	case storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION,
		storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_EVENT:
		return dataquality.ActionRecordDecisionWhy, nil
	default:
		return "", connectErrorInvalid("unknown memory kind %d", kind)
	}
}

// ListQualityMemories lists memories newest first, with expired confirmations read as
// EXPIRED.
func (s *DataQualityService) ListQualityMemories(ctx context.Context, req *connect.Request[v1pb.ListQualityMemoriesRequest]) (*connect.Response[v1pb.ListQualityMemoriesResponse], error) {
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
	find := &store.FindQualityMemoryMessage{Limit: &limit, Offset: &offset.offset}
	switch kind := req.Msg.GetKind(); kind {
	case v1pb.QualityMemoryKind_QUALITY_MEMORY_KIND_UNSPECIFIED:
	case v1pb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT,
		v1pb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION,
		v1pb.QualityMemoryKind_QUALITY_MEMORY_KIND_EVENT:
		value := storepb.QualityMemoryKind(kind)
		find.Kind = &value
	default:
		return nil, connectErrorInvalid("unknown memory kind %d", kind)
	}
	switch status := req.Msg.GetStatus(); status {
	case v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_UNSPECIFIED:
	case v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_PROPOSED,
		v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED,
		v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_REJECTED,
		v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_SUPERSEDED,
		v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_EXPIRED:
		value := storepb.QualityMemoryStatus(status)
		find.Status = &value
	default:
		return nil, connectErrorInvalid("unknown memory status %d", status)
	}
	if subject := strings.TrimSpace(req.Msg.GetSubject()); subject != "" {
		find.Subject = &subject
	}
	messages, err := s.store.ListQualityMemories(ctx, find)
	if err != nil {
		return nil, err
	}
	messages, nextPageToken, err := paginate(messages, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityMemoriesResponse{NextPageToken: nextPageToken}
	for _, message := range messages {
		response.QualityMemories = append(response.QualityMemories, convertQualityMemoryToV1(workspaceID, message))
	}
	return connect.NewResponse(response), nil
}

// ConfirmQualityMemory confirms or rejects a PROPOSED memory.
func (s *DataQualityService) ConfirmQualityMemory(ctx context.Context, req *connect.Request[v1pb.ConfirmQualityMemoryRequest]) (*connect.Response[v1pb.QualityMemory], error) {
	workspaceID, resourceID, err := s.qualityMemoryResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	status := storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED
	switch req.Msg.GetDecision() {
	case v1pb.ConfirmQualityMemoryRequest_DECISION_CONFIRM:
	case v1pb.ConfirmQualityMemoryRequest_DECISION_REJECT:
		status = storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_REJECTED
	default:
		return nil, connectErrorInvalid("decision must be CONFIRM or REJECT")
	}
	message, err := s.store.ConfirmQualityMemory(ctx, resourceID, status, principalIDPointer(currentUserID(ctx)))
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAborted, err)
		}
		if common.ErrorCode(err) == common.NotFound {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, err
	}
	return connect.NewResponse(convertQualityMemoryToV1(workspaceID, message)), nil
}

// DeleteQualityMemory soft-deletes a memory; administrator cleanup.
func (s *DataQualityService) DeleteQualityMemory(ctx context.Context, req *connect.Request[v1pb.DeleteQualityMemoryRequest]) (*connect.Response[emptypb.Empty], error) {
	_, resourceID, err := s.qualityMemoryResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteQualityMemory(ctx, resourceID); err != nil {
		if common.ErrorCode(err) == common.NotFound {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, err
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// validateQualityMemoryRequest validates the invariants: a known kind, a subject, a
// statement, at least one evidence reference, bounded text and a future expiry.
func validateQualityMemoryRequest(request *v1pb.CreateQualityMemoryRequest, source storepb.QualityMemorySource, status storepb.QualityMemoryStatus, confirmedBy *int32) (*store.CreateQualityMemoryInput, error) {
	if _, err := qualityMemoryAction(storepb.QualityMemoryKind(request.GetKind())); err != nil {
		return nil, err
	}
	subject := strings.TrimSpace(request.GetSubject())
	if subject == "" || len(subject) > maxMemorySubject {
		return nil, connectErrorInvalid("a memory needs a subject (max %d bytes)", maxMemorySubject)
	}
	statement := strings.TrimSpace(request.GetStatement())
	if statement == "" || len(statement) > maxMemoryStatement {
		return nil, connectErrorInvalid("a memory needs a statement (max %d bytes)", maxMemoryStatement)
	}
	domainRef := strings.TrimSpace(request.GetDomainRef())
	if len(domainRef) > maxMemoryDomainRef {
		return nil, connectErrorInvalid("domain_ref is too long (max %d bytes)", maxMemoryDomainRef)
	}
	if len(request.GetEvidence()) == 0 || len(request.GetEvidence()) > maxMemoryEvidence {
		return nil, connectErrorInvalid("a memory needs between 1 and %d evidence references", maxMemoryEvidence)
	}
	evidence := &storepb.QualityMemoryEvidenceList{}
	for _, reference := range request.GetEvidence() {
		kind := strings.ToUpper(strings.TrimSpace(reference.GetKind()))
		value := strings.TrimSpace(reference.GetReference())
		note := strings.TrimSpace(reference.GetNote())
		if kind == "" || value == "" || len(kind) > 64 || len(value) > maxMemoryEvidenceText {
			return nil, connectErrorInvalid("an evidence reference needs a kind and a reference (max %d bytes)", maxMemoryEvidenceText)
		}
		if len(note) > maxMemoryEvidenceText {
			return nil, connectErrorInvalid("an evidence note is too long (max %d bytes)", maxMemoryEvidenceText)
		}
		evidence.Evidence = append(evidence.Evidence, &storepb.QualityInvestigationEvidence{
			Kind: kind, Reference: value, Note: note,
		})
	}
	var expiresAt *time.Time
	if timestamp := request.GetExpiresAt(); timestamp != nil {
		if err := timestamp.CheckValid(); err != nil {
			return nil, connectErrorInvalid("expires_at is not a valid timestamp")
		}
		value := timestamp.AsTime()
		if !value.After(time.Now()) {
			return nil, connectErrorInvalid("expires_at must be in the future")
		}
		expiresAt = &value
	}
	return &store.CreateQualityMemoryInput{
		Kind:        storepb.QualityMemoryKind(request.GetKind()),
		Subject:     subject,
		Statement:   statement,
		Source:      source,
		Status:      status,
		Evidence:    evidence,
		DomainRef:   domainRef,
		ExpiresAt:   expiresAt,
		ConfirmedBy: confirmedBy,
	}, nil
}

// qualityMemoryResourceID resolves a memory name to its workspace and resource id.
func (s *DataQualityService) qualityMemoryResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityMemoryPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality memory name")
	}
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

// convertQualityMemoryToV1 renders one memory, reading an expired confirmation as
// EXPIRED so a reader never mistakes stale knowledge for current.
func convertQualityMemoryToV1(workspaceID string, message *store.QualityMemoryMessage) *v1pb.QualityMemory {
	if message == nil {
		return nil
	}
	status := v1pb.QualityMemoryStatus(message.Status)
	if message.Status == storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED &&
		message.ExpiresAt != nil && !message.ExpiresAt.After(time.Now()) {
		status = v1pb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_EXPIRED
	}
	converted := &v1pb.QualityMemory{
		Name:         common.FormatWorkspace(workspaceID) + "/" + qualityMemoryPrefix + message.ResourceID,
		Kind:         v1pb.QualityMemoryKind(message.Kind),
		Subject:      message.Subject,
		Statement:    message.Statement,
		Source:       v1pb.QualityMemorySource(message.Source),
		Status:       status,
		DomainRef:    message.DomainRef,
		ExpiresAt:    timestampOrNil(message.ExpiresAt),
		SupersededBy: qualityMemoryName(workspaceID, message.SupersededBy),
		ConfirmedBy:  formatQualityContractPublisher(message.ConfirmedBy),
		ConfirmedAt:  timestampOrNil(message.ConfirmedAt),
		CreatedBy:    formatQualityContractPublisher(message.CreatedBy),
		CreateTime:   timestamppb.New(message.CreatedAt),
	}
	for _, reference := range message.Evidence.GetEvidence() {
		converted.Evidence = append(converted.Evidence, &v1pb.QualityInvestigationEvidence{
			Kind: reference.GetKind(), Reference: reference.GetReference(), Note: reference.GetNote(),
		})
	}
	return converted
}

func qualityMemoryName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	return common.FormatWorkspace(workspaceID) + "/" + qualityMemoryPrefix + resourceID
}
