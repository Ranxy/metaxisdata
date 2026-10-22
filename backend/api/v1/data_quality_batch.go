package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// qualityBatchRequestLimit caps one batch RPC. Batches are not atomic, so this is
// the largest unit a caller can retry and report on; it matches the manual-run
// selection limit.
const qualityBatchRequestLimit = 200

// validateQualityBatchSize refuses an empty or oversized batch before any item is
// attempted: an empty batch is a caller mistake, and an unbounded one would hold
// a request open for an unbounded time.
func validateQualityBatchSize(count int) error {
	switch {
	case count == 0:
		return connectErrorInvalid("at least one request is required")
	case count > qualityBatchRequestLimit:
		return connectErrorInvalid("at most %d requests are allowed", qualityBatchRequestLimit)
	default:
		return nil
	}
}

// qualityBatchItemParentAcceptable reports whether a batch item may name this
// parent: empty means "the batch's parent", anything else must name it exactly.
func qualityBatchItemParentAcceptable(batchParent, itemParent string) bool {
	itemParent = strings.TrimSpace(itemParent)
	return itemParent == "" || itemParent == strings.TrimSpace(batchParent)
}

// BatchCreateQualityRules creates many rules in one call, optionally from the
// suggestions they came from. Each item is validated and stored independently:
// a failure is reported in its own result and does not abort the rest.
func (s *DataQualityService) BatchCreateQualityRules(ctx context.Context, req *connect.Request[v1pb.BatchCreateQualityRulesRequest]) (*connect.Response[v1pb.BatchCreateQualityRulesResponse], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	if err := validateQualityBatchSize(len(req.Msg.GetRequests())); err != nil {
		return nil, err
	}

	batchParent := strings.TrimSpace(req.Msg.GetParent())
	results := make([]*v1pb.BatchCreateQualityRuleResult, 0, len(req.Msg.GetRequests()))
	for _, item := range req.Msg.GetRequests() {
		result := &v1pb.BatchCreateQualityRuleResult{}
		if item == nil {
			result.Error = connectErrorInvalid("the rule is required").Error()
			results = append(results, result)
			continue
		}
		if !qualityBatchItemParentAcceptable(batchParent, item.GetParent()) {
			result.Error = connectErrorInvalid("the rule parent must match the batch parent").Error()
			results = append(results, result)
			continue
		}
		result.Suggestion = strings.TrimSpace(item.GetSuggestion())
		created, err := s.createQualityRuleFromRequest(ctx, workspaceID, item.GetRule(), item.GetSuggestion())
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Rule = created
		}
		results = append(results, result)
	}
	return connect.NewResponse(&v1pb.BatchCreateQualityRulesResponse{Results: results}), nil
}

// BatchUpdateQualitySuggestions accepts or rejects many queued suggestions in one
// call. Each decision is independent: a failure (including an already decided
// suggestion) is reported in its own result and does not abort the rest.
func (s *DataQualityService) BatchUpdateQualitySuggestions(ctx context.Context, req *connect.Request[v1pb.BatchUpdateQualitySuggestionsRequest]) (*connect.Response[v1pb.BatchUpdateQualitySuggestionsResponse], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	if err := validateQualityBatchSize(len(req.Msg.GetRequests())); err != nil {
		return nil, err
	}

	results := make([]*v1pb.BatchUpdateQualitySuggestionResult, 0, len(req.Msg.GetRequests()))
	for _, item := range req.Msg.GetRequests() {
		result := &v1pb.BatchUpdateQualitySuggestionResult{}
		if item == nil {
			result.Error = connectErrorInvalid("a decision is required").Error()
			results = append(results, result)
			continue
		}
		updated, err := s.decideQualitySuggestion(ctx, item.GetName(), item.GetDecision(), item.GetDecisionNote())
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Suggestion = updated
		}
		result.Name = strings.TrimSpace(item.GetName())
		results = append(results, result)
	}
	return connect.NewResponse(&v1pb.BatchUpdateQualitySuggestionsResponse{Results: results}), nil
}
