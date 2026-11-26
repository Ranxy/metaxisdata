package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// AgentFlagResult is what one flagged draft recorded.
type AgentFlagResult struct {
	Draft   string `json:"draft"`
	Concern string `json:"concern"`
	State   string `json:"state"`
}

// maxDraftConcernBytes bounds one concern: it is a sentence about one proposal, not a report.
const maxDraftConcernBytes = 1000

// FlagAgentProposal records the agent's own objection to a draft it queued. It is how the model's
// doubt reaches the adoption gate, which cannot see it any other way: the allowlist, the confidence
// and the criticality switch judge the proposal's shape, not whether the model that drafted it
// believes it.
//
// A flagged draft is never adopted automatically. It stays pending for a person, who reads the
// concern beside the draft, and it can still be adopted deliberately — by the owner in the review
// queue or by the owner's explicit request — because a flagged draft is a doubt, not a refusal.
func (s *DataQualityService) FlagAgentProposal(ctx context.Context, agentPrincipalID int32, draftResourceID, concern string) (*AgentFlagResult, error) {
	resourceID := strings.TrimSpace(draftResourceID)
	if index := strings.LastIndex(resourceID, "/"); index >= 0 {
		resourceID = resourceID[index+1:]
	}
	if resourceID == "" {
		return nil, connectErrorInvalid("a draft resource name is required")
	}
	concern = strings.TrimSpace(concern)
	if concern == "" {
		return nil, connectErrorInvalid("a concern is required: say what the draft gets wrong")
	}
	agent, err := s.store.GetQualityAgentByPrincipalID(ctx, int(agentPrincipalID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the agent")
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("the caller is not a quality agent"))
	}
	suggestions, err := s.store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		ResourceID: &resourceID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the draft")
	}
	if len(suggestions) == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("draft %q not found", resourceID))
	}
	suggestion := suggestions[0]
	if suggestion.AgentID == nil || *suggestion.AgentID != agent.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("the draft belongs to another principal"))
	}
	if err := s.checkAgentTargetPermission(ctx, agent, suggestion.TargetGUID, permission.DataQualitySuggest); err != nil {
		return nil, err
	}
	flagged, applied, err := s.store.SetQualitySuggestionConcern(ctx, resourceID,
		common.TruncateUTF8Bytes(concern, maxDraftConcernBytes))
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the draft is already decided, so there is nothing left to flag"))
	}
	// The draft's state is PENDING by construction: the update refuses a decided row.
	return &AgentFlagResult{
		Draft:   "qualitySuggestions/" + flagged.ResourceID,
		Concern: flagged.Concern,
		State:   "PENDING",
	}, nil
}
