package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"

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

const qualityChangeRequestPrefix = "qualityChangeRequests/"

// maxQualityChangeRequestItems bounds one request: a batch is reviewable, not a
// backlog dump. The same cap the design gives the approval dialog.
const maxQualityChangeRequestItems = 20

// CreateQualityChangeRequest records a proposed batch of typed changes against one
// target. Creating it changes nothing: it waits for a person's decision.
func (s *DataQualityService) CreateQualityChangeRequest(ctx context.Context, req *connect.Request[v1pb.CreateQualityChangeRequestRequest]) (*connect.Response[v1pb.QualityChangeRequest], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, err := s.authorizeAgentAction(ctx, dataquality.ActionProposePlanChange, len(req.Msg.GetItems()))
	if err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	items := req.Msg.GetItems()
	if len(items) == 0 || len(items) > maxQualityChangeRequestItems {
		return nil, connectErrorInvalid("a change request carries between 1 and %d items", maxQualityChangeRequestItems)
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
	// A retry of the same batch returns the request that already exists, even after
	// its suggestions were decided: look up the fingerprint before validating, or a
	// retry would be refused by the now-decided proposal.
	preliminary, err := qualityChangeRequestItemsForFingerprint(items)
	if err != nil {
		return nil, err
	}
	fingerprint := qualityChangeRequestFingerprint(preliminary, req.Msg.GetBaseVersion())
	source := qualityChangeRequestSource(agent)
	existing, err := s.store.GetQualityChangeRequestByFingerprint(ctx, targetGUID, metaType,
		req.Msg.GetBaseVersion(), source, fingerprint)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return connect.NewResponse(convertQualityChangeRequestToV1(workspaceID, existing)), nil
	}
	storedItems, err := s.validateQualityChangeRequestItems(ctx, targetGUID, metaType, contract, items)
	if err != nil {
		return nil, err
	}
	// A critical target is decided one item at a time: a batch makes a person
	// approve changes they did not read separately.
	if len(storedItems.GetItems()) > 1 && qualityChangeRequestCriticality(contract, rules) == dataquality.CriticalityCritical {
		return nil, connectErrorInvalid("a critical target's changes are decided one item at a time")
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the change request id")
	}
	createdBy := principalIDPointer(currentUserID(ctx))
	message, _, err := s.store.CreateQualityChangeRequest(ctx, &store.CreateQualityChangeRequestInput{
		ResourceID:         resourceID,
		TargetGUID:         targetGUID,
		TargetType:         metaType,
		BaseVersion:        req.Msg.GetBaseVersion(),
		Source:             source,
		Items:              storedItems,
		RequestFingerprint: fingerprint,
		CreatedBy:          createdBy,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityChangeRequestToV1(workspaceID, message)), nil
}

// qualityChangeRequestSource records who proposed the batch: the agent principal
// when an agent writes it, a person otherwise.
func qualityChangeRequestSource(agent *store.QualityAgentMessage) storepb.QualityChangeRequestSource {
	if agent != nil {
		return storepb.QualityChangeRequestSource_QUALITY_CHANGE_REQUEST_SOURCE_AGENT
	}
	return storepb.QualityChangeRequestSource_QUALITY_CHANGE_REQUEST_SOURCE_HUMAN
}

// GetQualityChangeRequest reads one request with its items and per-item results.
func (s *DataQualityService) GetQualityChangeRequest(ctx context.Context, req *connect.Request[v1pb.GetQualityChangeRequestRequest]) (*connect.Response[v1pb.QualityChangeRequest], error) {
	workspaceID, resourceID, err := s.qualityChangeRequestResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	message, err := s.fetchQualityChangeRequest(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityChangeRequestToV1(workspaceID, message)), nil
}

// ListQualityChangeRequests lists requests newest first.
func (s *DataQualityService) ListQualityChangeRequests(ctx context.Context, req *connect.Request[v1pb.ListQualityChangeRequestsRequest]) (*connect.Response[v1pb.ListQualityChangeRequestsResponse], error) {
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
	limitPlusOne := offset.limit + 1
	find := &store.FindQualityChangeRequestMessage{
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	switch state := req.Msg.GetState(); state {
	case v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_UNSPECIFIED:
	case v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_PROPOSED,
		v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPROVED,
		v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPLIED,
		v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_REJECTED,
		v1pb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_EXPIRED:
		find.States = []storepb.QualityChangeRequestState{storepb.QualityChangeRequestState(state)}
	default:
		return nil, connectErrorInvalid("unknown change request state %d", state)
	}
	messages, err := s.store.ListQualityChangeRequests(ctx, find)
	if err != nil {
		return nil, err
	}
	messages, nextPageToken, err := paginate(messages, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityChangeRequestsResponse{NextPageToken: nextPageToken}
	for _, message := range messages {
		response.QualityChangeRequests = append(response.QualityChangeRequests, convertQualityChangeRequestToV1(workspaceID, message))
	}
	return connect.NewResponse(response), nil
}

// DecideQualityChangeRequest approves (applying every item through its existing
// path) or rejects one request. A stale base expires it instead of rebasing.
func (s *DataQualityService) DecideQualityChangeRequest(ctx context.Context, req *connect.Request[v1pb.DecideQualityChangeRequestRequest]) (*connect.Response[v1pb.QualityChangeRequest], error) {
	workspaceID, resourceID, err := s.qualityChangeRequestResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	decision := req.Msg.GetDecision()
	if decision != v1pb.DecideQualityChangeRequestRequest_DECISION_APPROVE &&
		decision != v1pb.DecideQualityChangeRequestRequest_DECISION_REJECT {
		return nil, connectErrorInvalid("decision must be APPROVE or REJECT")
	}
	reasons, err := qualityChangeRequestRejectionReasons(req.Msg.GetRejectionReasons())
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(req.Msg.GetDecisionNote())
	if len(note) > maxQualityContractNoteBytes {
		return nil, connectErrorInvalid("the decision note is too long (max %d bytes)", maxQualityContractNoteBytes)
	}
	message, err := s.fetchQualityChangeRequest(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if message.State == storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPLIED ||
		message.State == storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_REJECTED ||
		message.State == storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_EXPIRED {
		return nil, connect.NewError(connect.CodeAborted,
			errors.Errorf("quality change request %q is already %s", resourceID, message.State))
	}
	decidedBy := principalIDPointer(currentUserID(ctx))
	if decision == v1pb.DecideQualityChangeRequestRequest_DECISION_REJECT {
		// A request whose items all failed has nothing applied yet, so it can still be
		// refused; one with applied items cannot.
		if message.State != storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_PROPOSED &&
			(message.State != storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPROVED ||
				qualityChangeRequestHasAppliedItem(message.ItemResults)) {
			return nil, connect.NewError(connect.CodeAborted,
				errors.New("a request whose items already applied cannot be rejected"))
		}
		if len(reasons) == 0 {
			return nil, connectErrorInvalid("a rejection needs at least one reason")
		}
		updated, err := s.store.DecideQualityChangeRequest(ctx, &store.DecisionQualityChangeRequestInput{
			ResourceID:          resourceID,
			ExpectedState:       message.State,
			ExpectedItemResults: message.ItemResults,
			State:               storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_REJECTED,
			ItemResults:         message.ItemResults,
			RejectionReasons:    reasons,
			DecisionNote:        note,
			DecidedBy:           decidedBy,
		})
		if err != nil {
			return nil, qualityChangeRequestDecisionError(err)
		}
		return connect.NewResponse(convertQualityChangeRequestToV1(workspaceID, updated)), nil
	}

	// Approval requires the version the request was written against. A decision on a
	// stale base expires the request: silently rebasing would apply changes to a
	// version the approver never saw.
	contract, err := s.store.GetQualityContract(ctx, message.TargetGUID, message.TargetType)
	if err != nil {
		return nil, err
	}
	if message.BaseVersion != qualityContractPublishedVersion(contract) {
		expired, err := s.store.DecideQualityChangeRequest(ctx, &store.DecisionQualityChangeRequestInput{
			ResourceID:          resourceID,
			ExpectedState:       message.State,
			ExpectedItemResults: message.ItemResults,
			State:               storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_EXPIRED,
			ItemResults:         message.ItemResults,
			DecisionNote:        "the published version it was written against is gone",
			DecidedBy:           decidedBy,
		})
		if err != nil {
			return nil, qualityChangeRequestDecisionError(err)
		}
		return connect.NewResponse(convertQualityChangeRequestToV1(workspaceID, expired)), nil
	}
	results, err := s.applyQualityChangeRequestItems(ctx, workspaceID, message)
	if err != nil {
		return nil, err
	}
	state := storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPLIED
	for _, result := range results.GetResults() {
		if result.GetStatus() != storepb.QualityChangeRequestItemResult_STATUS_APPLIED &&
			result.GetStatus() != storepb.QualityChangeRequestItemResult_STATUS_SKIPPED {
			state = storepb.QualityChangeRequestState_QUALITY_CHANGE_REQUEST_STATE_APPROVED
			break
		}
	}
	updated, err := s.store.DecideQualityChangeRequest(ctx, &store.DecisionQualityChangeRequestInput{
		ResourceID:          resourceID,
		ExpectedState:       message.State,
		ExpectedItemResults: message.ItemResults,
		State:               state,
		ItemResults:         results,
		DecisionNote:        note,
		DecidedBy:           decidedBy,
	})
	if err != nil {
		return nil, qualityChangeRequestDecisionError(err)
	}
	return connect.NewResponse(convertQualityChangeRequestToV1(workspaceID, updated)), nil
}

// applyQualityChangeRequestItems applies every item that is not already applied.
// Each item reports its own outcome: one failure never hides the others.
func (s *DataQualityService) applyQualityChangeRequestItems(ctx context.Context, workspaceID string, message *store.QualityChangeRequestMessage) (*storepb.QualityChangeRequestItemResults, error) {
	results := make(map[int32]*storepb.QualityChangeRequestItemResult, len(message.Items.GetItems()))
	for _, previous := range message.ItemResults.GetResults() {
		if previous.GetStatus() == storepb.QualityChangeRequestItemResult_STATUS_APPLIED ||
			previous.GetStatus() == storepb.QualityChangeRequestItemResult_STATUS_SKIPPED {
			results[previous.GetIndex()] = previous
		}
	}
	// Declarations are applied in one write: they merge into the same declaration
	// list, so applying them one by one would race its compare-and-set.
	coverageItems := map[int32]*storepb.QualityChangeRequestItem{}
	for index, item := range message.Items.GetItems() {
		if item.GetKind() != storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE {
			continue
		}
		if _, done := results[int32(index)]; done {
			continue
		}
		coverageItems[int32(index)] = item
	}
	if len(coverageItems) > 0 {
		coverageResults, err := s.applyQualityChangeRequestCoverage(ctx, message, coverageItems)
		if err != nil {
			return nil, err
		}
		for index, result := range coverageResults {
			results[index] = result
		}
	}
	for index, item := range message.Items.GetItems() {
		if item.GetKind() != storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF {
			continue
		}
		if _, done := results[int32(index)]; done {
			continue
		}
		results[int32(index)] = s.applyQualityChangeRequestSuggestion(ctx, workspaceID, message, int32(index), item)
	}
	ordered := &storepb.QualityChangeRequestItemResults{}
	for index := range message.Items.GetItems() {
		result := results[int32(index)]
		if result == nil {
			result = &storepb.QualityChangeRequestItemResult{
				Index:  int32(index),
				Status: storepb.QualityChangeRequestItemResult_STATUS_FAILED,
				Detail: "the item was not applied",
			}
		}
		ordered.Results = append(ordered.Results, result)
	}
	return ordered, nil
}

// applyQualityChangeRequestCoverage merges the coverage items into the target's
// declaration and writes it once under the store's compare-and-set.
func (s *DataQualityService) applyQualityChangeRequestCoverage(ctx context.Context, message *store.QualityChangeRequestMessage, items map[int32]*storepb.QualityChangeRequestItem) (map[int32]*storepb.QualityChangeRequestItemResult, error) {
	results := make(map[int32]*storepb.QualityChangeRequestItemResult, len(items))
	fail := func(detail string) map[int32]*storepb.QualityChangeRequestItemResult {
		for index := range items {
			results[index] = &storepb.QualityChangeRequestItemResult{
				Index:  index,
				Status: storepb.QualityChangeRequestItemResult_STATUS_FAILED,
				Detail: detail,
			}
		}
		return results
	}
	contract, err := s.store.GetQualityContract(ctx, message.TargetGUID, message.TargetType)
	if err != nil {
		return nil, err
	}
	if contract == nil {
		return fail("the target has no contract to declare coverage on"), nil
	}
	// The publish may have landed between the decision's staleness check and this
	// fresh read. Applying on top of a newer version would silently rebase the change
	// the EXPIRED rule exists to prevent, and it would regress the declaration's base.
	if message.BaseVersion != qualityContractPublishedVersion(contract) {
		return fail("the published version changed; the request is stale"), nil
	}
	rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &message.TargetGUID})
	if err != nil {
		return nil, err
	}
	working := qualityContractWorkingPlan(contract.Settings, rules, contract, nil).GetCoverage()
	applied := false
	for index, item := range items {
		property := item.GetProperty()
		column := strings.TrimSpace(item.GetColumn())
		minChecks := item.GetMinChecks()
		if minChecks <= 0 {
			minChecks = 1
		}
		if minChecks > maxQualityPlanMinChecks {
			results[index] = &storepb.QualityChangeRequestItemResult{
				Index: index, Status: storepb.QualityChangeRequestItemResult_STATUS_FAILED,
				Detail: "a declaration needs between 1 and 10 checks",
			}
			continue
		}
		position := slices.IndexFunc(working, func(declaration *storepb.QualityContractPlanCoverage) bool {
			return declaration.GetProperty() == property && declaration.GetColumn() == column
		})
		switch {
		case item.GetRemove():
			if position < 0 {
				results[index] = &storepb.QualityChangeRequestItemResult{
					Index: index, Status: storepb.QualityChangeRequestItemResult_STATUS_SKIPPED,
					Detail: "the declaration was not present",
				}
				continue
			}
			working = append(working[:position], working[position+1:]...)
			applied = true
			results[index] = &storepb.QualityChangeRequestItemResult{
				Index: index, Status: storepb.QualityChangeRequestItemResult_STATUS_APPLIED,
			}
		default:
			if position >= 0 && working[position].GetMinChecks() == minChecks {
				results[index] = &storepb.QualityChangeRequestItemResult{
					Index: index, Status: storepb.QualityChangeRequestItemResult_STATUS_SKIPPED,
					Detail: "the declaration already matched",
				}
				continue
			}
			updated := &storepb.QualityContractPlanCoverage{
				Property:  property,
				Column:    column,
				MinChecks: minChecks,
				Source:    storepb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_INTENT,
			}
			if position >= 0 {
				working[position] = updated
			} else {
				working = append(working, updated)
			}
			applied = true
			results[index] = &storepb.QualityChangeRequestItemResult{
				Index: index, Status: storepb.QualityChangeRequestItemResult_STATUS_APPLIED,
			}
		}
	}
	if !applied {
		return results, nil
	}
	if len(working) > maxQualityPlanCoverage {
		return fail("the plan declaration would exceed its size limit"), nil
	}
	if err := dataquality.ValidatePlanCoverage(ctx, s.store, message.TargetGUID, message.TargetType, working); err != nil {
		return fail(qualityChangeRequestDetail(err)), nil
	}
	if _, err := s.store.SetQualityContractStaged(ctx, &store.SetQualityContractStagedInput{
		TargetGUID:          message.TargetGUID,
		TargetType:          message.TargetType,
		Plan:                &storepb.QualityContractPlan{Coverage: working},
		BaseVersion:         message.BaseVersion,
		UpdatedBy:           principalIDPointer(currentUserID(ctx)),
		ExpectedStaged:      contract.Staged,
		ExpectedBaseVersion: contract.StagedBaseVersion,
	}); err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return fail("the plan declaration changed; the request is stale"), nil
		}
		return nil, err
	}
	return results, nil
}

// applyQualityChangeRequestSuggestion accepts one pending proposal through the
// existing path: the draft is claimed, the rule is created from it, and the claim is
// rolled back if the write fails.
func (s *DataQualityService) applyQualityChangeRequestSuggestion(ctx context.Context, workspaceID string, message *store.QualityChangeRequestMessage, index int32, item *storepb.QualityChangeRequestItem) *storepb.QualityChangeRequestItemResult {
	result := &storepb.QualityChangeRequestItemResult{Index: index}
	resourceID := strings.TrimSpace(item.GetSuggestionResourceId())
	if resourceID == "" {
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = "a suggestion item must name a suggestion"
		return result
	}
	suggestion, err := s.fetchQualitySuggestion(ctx, resourceID)
	if err != nil {
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = qualityChangeRequestDetail(err)
		return result
	}
	if suggestion.TargetGUID != message.TargetGUID || suggestion.TargetType != message.TargetType {
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = "the suggestion belongs to another target"
		return result
	}
	if suggestion.Kind != storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT {
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = "the suggestion is not a rule draft"
		return result
	}
	if suggestion.Status != storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING {
		// Only an accepted proposal means the change is already true. A rejected or
		// superseded one was never applied, so the item failed.
		if suggestion.Status == storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED {
			result.Status = storepb.QualityChangeRequestItemResult_STATUS_SKIPPED
			result.Detail = "the suggestion is already accepted"
			return result
		}
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = "the suggestion was decided before the request was approved"
		return result
	}
	rule, err := qualityRuleFromDraft(suggestion, storepb.QualityDimension(item.GetDimension()))
	if err != nil {
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = qualityChangeRequestDetail(err)
		return result
	}
	created, err := s.createQualityRuleFromRequest(ctx, workspaceID, rule, formatQualitySuggestionName(workspaceID, resourceID))
	if err != nil {
		result.Status = storepb.QualityChangeRequestItemResult_STATUS_FAILED
		result.Detail = qualityChangeRequestDetail(err)
		return result
	}
	result.Status = storepb.QualityChangeRequestItemResult_STATUS_APPLIED
	result.AppliedResourceId = created.GetName()
	return result
}

// validateQualityChangeRequestItems renders the public items into the stored shape
// and rejects what a request must not carry: unknown kinds, red-line actions, a
// suggestion that is not a pending draft of this target, or a duplicate declaration.
func (s *DataQualityService) validateQualityChangeRequestItems(ctx context.Context, targetGUID string, metaType storepb.MetaType, contract *store.QualityContractMessage, items []*v1pb.QualityChangeRequestItem) (*storepb.QualityChangeRequestItems, error) {
	stored := &storepb.QualityChangeRequestItems{Items: make([]*storepb.QualityChangeRequestItem, 0, len(items))}
	seenCoverage := make(map[string]struct{}, len(items))
	hasCoverage := false
	for _, item := range items {
		switch item.GetKind() {
		case v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF:
			resourceID, err := qualitySuggestionResourceIDFromName(item.GetSuggestion())
			if err != nil {
				return nil, err
			}
			suggestion, err := s.fetchQualitySuggestion(ctx, resourceID)
			if err != nil {
				return nil, err
			}
			if suggestion.TargetGUID != targetGUID || suggestion.TargetType != metaType {
				return nil, connectErrorInvalid("suggestion %q belongs to another target", resourceID)
			}
			if suggestion.Kind != storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT {
				return nil, connectErrorInvalid("suggestion %q is not a rule draft", resourceID)
			}
			if suggestion.Status != storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING {
				return nil, connect.NewError(connect.CodeAborted,
					errors.Errorf("suggestion %q is already decided", resourceID))
			}
			dimension := storepb.QualityDimension(item.GetDimension())
			if dimension == storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
				return nil, connectErrorInvalid("a suggestion item needs the dimension the created rule carries")
			}
			stored.Items = append(stored.Items, &storepb.QualityChangeRequestItem{
				Kind:                 storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF,
				SuggestionResourceId: resourceID,
				Dimension:            dimension,
			})
		case v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE:
			hasCoverage = true
			if err := validateQualityCoverageProperty(item.GetProperty()); err != nil {
				return nil, err
			}
			column := strings.TrimSpace(item.GetColumn())
			minChecks := item.GetMinChecks()
			if !item.GetRemove() {
				if minChecks < 0 || minChecks > maxQualityPlanMinChecks {
					return nil, connectErrorInvalid("a declaration needs between 1 and %d checks", maxQualityPlanMinChecks)
				}
				if minChecks == 0 {
					minChecks = 1
				}
			}
			key := strings.TrimSpace(item.GetProperty().String()) + "|" + column
			if _, ok := seenCoverage[key]; ok {
				return nil, connectErrorInvalid("a change request declares %q for %s twice", column, item.GetProperty())
			}
			seenCoverage[key] = struct{}{}
			stored.Items = append(stored.Items, &storepb.QualityChangeRequestItem{
				Kind:      storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
				Property:  storepb.QualityCoverageProperty(item.GetProperty()),
				Column:    column,
				MinChecks: minChecks,
				Remove:    item.GetRemove(),
			})
		default:
			return nil, connectErrorInvalid("unknown change request item kind %d", item.GetKind())
		}
	}
	if hasCoverage && contract == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the target has no contract; publish one before declaring coverage"))
	}
	return stored, nil
}

// qualityChangeRequestCriticality derives the target's plan criticality the same
// way the plan card does.
func qualityChangeRequestCriticality(contract *store.QualityContractMessage, rules []*store.QualityRuleMessage) dataquality.AssetCriticality {
	var intent *storepb.QualityContractIntent
	if contract != nil {
		intent = dataquality.ActiveIntent(contract.Settings.GetIntent(), time.Now())
	}
	return dataquality.DerivePlan(contract != nil, intent, qualityRuleShapes(rules)).Criticality
}

// qualityRuleFromDraft renders a pending rule draft as the rule the accept path
// creates. The draft's target is the suggestion's target, never a request field.
func qualityRuleFromDraft(suggestion *store.QualitySuggestionMessage, dimension storepb.QualityDimension) (*v1pb.QualityRule, error) {
	draft := suggestion.Payload.GetRule()
	if draft == nil {
		return nil, errors.New("the suggestion carries no rule draft")
	}
	ruleType, ok := dataquality.RuleTypeForConfig(draft.GetConfig())
	if !ok {
		return nil, errors.New("the suggestion's rule config is not usable")
	}
	return &v1pb.QualityRule{
		Title:       draft.GetTitle(),
		Dimension:   v1pb.QualityDimension(dimension),
		Description: draft.GetRationale(),
		RuleType:    v1pb.QualityRuleType(ruleType),
		Severity:    v1pb.QualitySeverity(draft.GetSeverity()),
		Config:      convertQualityRuleConfigToV1(draft.GetConfig()),
		Target:      &v1pb.QualityTarget{MetaGuid: suggestion.TargetGUID, MetaType: v1pb.MetaType(suggestion.TargetType)},
	}, nil
}

// qualityChangeRequestFingerprint is the idempotency key's content: the same items
// against the same version cannot become two requests.
func qualityChangeRequestFingerprint(items *storepb.QualityChangeRequestItems, baseVersion int32) string {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(items)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("v"+strconv.Itoa(int(baseVersion))+"|"), payload...))
	return hex.EncodeToString(sum[:])
}

// qualityChangeRequestRejectionReasons validates the rejection taxonomy.
func qualityChangeRequestRejectionReasons(reasons []v1pb.QualityChangeRejectionReason) ([]storepb.QualityChangeRejectionReason, error) {
	stored := make([]storepb.QualityChangeRejectionReason, 0, len(reasons))
	for _, reason := range reasons {
		converted := storepb.QualityChangeRejectionReason(reason)
		switch converted {
		case storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_NOT_IMPORTANT,
			storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_TOO_TIGHT,
			storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_NOISE,
			storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_COVERED_UPSTREAM,
			storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_WRONG_COLUMN,
			storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_DEFER,
			storepb.QualityChangeRejectionReason_QUALITY_CHANGE_REJECTION_REASON_OTHER:
			stored = append(stored, converted)
		default:
			return nil, connectErrorInvalid("unknown rejection reason %d", reason)
		}
	}
	return stored, nil
}

// qualityChangeRequestResourceID resolves a request name to its workspace and id.
func (s *DataQualityService) qualityChangeRequestResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityChangeRequestPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality change request name")
	}
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

// fetchQualityChangeRequest reads one request, reporting a missing row as NotFound.
func (s *DataQualityService) fetchQualityChangeRequest(ctx context.Context, resourceID string) (*store.QualityChangeRequestMessage, error) {
	message, err := s.store.GetQualityChangeRequest(ctx, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality change request")
	}
	if message == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality change request %q not found", resourceID))
	}
	return message, nil
}

// qualityChangeRequestDecisionError maps a lost compare-and-set to Aborted.
func qualityChangeRequestDecisionError(err error) error {
	if common.ErrorCode(err) == common.Conflict {
		return connect.NewError(connect.CodeAborted, err)
	}
	return err
}

// qualityChangeRequestDetail renders an item failure for a reviewer. API errors keep
// their message; anything else (a store or database error) becomes a fixed detail so
// internal text never lands in the durable item_results payload.
func qualityChangeRequestDetail(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}
	if common.ErrorCode(err) == common.Invalid {
		// Validation messages are written for the reviewer and carry no payload.
		return err.Error()
	}
	return "the item could not be applied"
}

// qualityChangeRequestHasAppliedItem reports whether any item already applied, which
// is what makes approving irreversible.
func qualityChangeRequestHasAppliedItem(results *storepb.QualityChangeRequestItemResults) bool {
	for _, result := range results.GetResults() {
		if result.GetStatus() == storepb.QualityChangeRequestItemResult_STATUS_APPLIED {
			return true
		}
	}
	return false
}

// qualityChangeRequestItemsForFingerprint renders the items enough to compute the
// idempotency fingerprint before the expensive per-item validation.
func qualityChangeRequestItemsForFingerprint(items []*v1pb.QualityChangeRequestItem) (*storepb.QualityChangeRequestItems, error) {
	stored := &storepb.QualityChangeRequestItems{Items: make([]*storepb.QualityChangeRequestItem, 0, len(items))}
	for _, item := range items {
		switch item.GetKind() {
		case v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF:
			stored.Items = append(stored.Items, &storepb.QualityChangeRequestItem{
				Kind:                 storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_SUGGESTION_REF,
				SuggestionResourceId: strings.TrimSpace(item.GetSuggestion()),
				Dimension:            storepb.QualityDimension(item.GetDimension()),
			})
		case v1pb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE:
			stored.Items = append(stored.Items, &storepb.QualityChangeRequestItem{
				Kind:      storepb.QualityChangeRequestItemKind_QUALITY_CHANGE_REQUEST_ITEM_KIND_COVERAGE_CHANGE,
				Property:  storepb.QualityCoverageProperty(item.GetProperty()),
				Column:    strings.TrimSpace(item.GetColumn()),
				MinChecks: item.GetMinChecks(),
				Remove:    item.GetRemove(),
			})
		default:
			return nil, connectErrorInvalid("unknown change request item kind %d", item.GetKind())
		}
	}
	return stored, nil
}

// qualitySuggestionResourceIDFromName extracts a suggestion id from a resource name.
func qualitySuggestionResourceIDFromName(name string) (string, error) {
	_, resourceID, err := parseQualitySuggestionName(strings.TrimSpace(name))
	if err != nil {
		return "", connectErrorInvalid("%v", err)
	}
	return resourceID, nil
}

// convertQualityChangeRequestToV1 renders one request.
func convertQualityChangeRequestToV1(workspaceID string, message *store.QualityChangeRequestMessage) *v1pb.QualityChangeRequest {
	if message == nil {
		return nil
	}
	converted := &v1pb.QualityChangeRequest{
		Name:         common.FormatWorkspace(workspaceID) + "/" + qualityChangeRequestPrefix + message.ResourceID,
		Target:       &v1pb.QualityTarget{MetaGuid: message.TargetGUID, MetaType: v1pb.MetaType(message.TargetType)},
		State:        v1pb.QualityChangeRequestState(message.State),
		BaseVersion:  message.BaseVersion,
		Source:       v1pb.QualityChangeRequestSource(message.Source),
		DecisionNote: message.DecisionNote,
		CreatedBy:    formatQualityContractPublisher(message.CreatedBy),
		DecidedBy:    formatQualityContractPublisher(message.DecidedBy),
		CreatedAt:    timestamppb.New(message.CreatedAt),
	}
	if message.DecidedAt != nil {
		converted.DecidedAt = timestamppb.New(*message.DecidedAt)
	}
	for _, item := range message.Items.GetItems() {
		rendered := &v1pb.QualityChangeRequestItem{
			Kind:      v1pb.QualityChangeRequestItemKind(item.GetKind()),
			Property:  v1pb.QualityCoverageProperty(item.GetProperty()),
			Column:    item.GetColumn(),
			MinChecks: item.GetMinChecks(),
			Remove:    item.GetRemove(),
			Dimension: v1pb.QualityDimension(item.GetDimension()),
		}
		// The suggestion field is SUGGESTION_REF-only; a coverage item must not render
		// a bogus empty suggestion name.
		if resourceID := item.GetSuggestionResourceId(); resourceID != "" {
			rendered.Suggestion = formatQualitySuggestionName(workspaceID, resourceID)
		}
		converted.Items = append(converted.Items, rendered)
	}
	for _, result := range message.ItemResults.GetResults() {
		converted.ItemResults = append(converted.ItemResults, &v1pb.QualityChangeRequestItemResult{
			Index:             result.GetIndex(),
			Status:            v1pb.QualityChangeRequestItemResult_Status(result.GetStatus()),
			Detail:            result.GetDetail(),
			AppliedResourceId: result.GetAppliedResourceId(),
		})
	}
	for _, reason := range message.RejectionReasons {
		converted.RejectionReasons = append(converted.RejectionReasons, v1pb.QualityChangeRejectionReason(reason))
	}
	return converted
}
