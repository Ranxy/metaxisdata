package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// AgentWithdrawResult is what one draft withdrawal recorded.
type AgentWithdrawResult struct {
	Draft  string `json:"draft"`
	Reason string `json:"reason"`
	State  string `json:"state"`
}

// maxDraftWithdrawalBytes bounds one withdrawal reason: it is a sentence about why the
// proposal was taken back, not a report.
const maxDraftWithdrawalBytes = 1000

// WithdrawAgentDraft retracts a pending draft the agent queued. It is the author's
// exit from the review queue, and it is deliberately not a rejection: a person never
// decided the proposal, so no decider is recorded and the same identity may be queued
// again with a corrected definition. The write also resolves the target's approval
// ask once no pending draft is left, which is why a bad draft no longer keeps the
// owner's to-do card open until someone rejects it by hand.
//
// Only the proposing agent may withdraw: the draft's agent_id must be this agent, and
// the target must still be inside its delegated scope. A draft another pass or a
// person produced is not the agent's to take back.
func (s *DataQualityService) WithdrawAgentDraft(ctx context.Context, agentPrincipalID int32, draftResourceID, reason string) (*AgentWithdrawResult, error) {
	resourceID := draftResourceID
	if index := strings.LastIndex(resourceID, "/"); index >= 0 {
		resourceID = resourceID[index+1:]
	}
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, connectErrorInvalid("a draft resource name is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, connectErrorInvalid("a withdrawal needs a reason: say why the draft should leave the queue")
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
	if err := requireWithdrawableDraft(agent, suggestion); err != nil {
		return nil, err
	}
	if err := s.checkAgentTargetPermission(ctx, agent, suggestion.TargetGUID, permission.DataQualitySuggest); err != nil {
		return nil, err
	}
	withdrawn, applied, err := s.store.WithdrawQualitySuggestion(ctx, resourceID, agent.ID,
		common.TruncateUTF8Bytes(reason, maxDraftWithdrawalBytes), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !applied {
		return s.withdrawNotApplied(ctx, resourceID, agent.ID)
	}
	// The approval ask is a projection of the queue: the last pending draft leaving it
	// closes the card. A failure here is logged inside the helper, because the draft is
	// already withdrawn and the next adoption pass will drain the ask anyway.
	s.resolveProposalAskForSuggestion(ctx, withdrawn)
	return &AgentWithdrawResult{
		Draft:  "qualitySuggestions/" + withdrawn.ResourceID,
		Reason: withdrawn.DecisionNote,
		State:  "WITHDRAWN",
	}, nil
}

// withdrawNotApplied explains a withdrawal that wrote nothing. Re-reading is what
// separates a retry of the agent's own withdrawal (a success, so a transient failure
// can be retried safely) from a reviewer's decision that landed first (a failed
// precondition) and from another agent's draft (a permission error).
func (s *DataQualityService) withdrawNotApplied(ctx context.Context, resourceID string, agentID int64) (*AgentWithdrawResult, error) {
	current, err := s.store.GetQualitySuggestion(ctx, &store.FindQualitySuggestionMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to re-read the draft")
	}
	if current == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("draft %q not found", resourceID))
	}
	if current.AgentID == nil || *current.AgentID != agentID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("the draft belongs to another principal"))
	}
	if current.Status == storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_WITHDRAWN {
		return &AgentWithdrawResult{
			Draft:  "qualitySuggestions/" + current.ResourceID,
			Reason: current.DecisionNote,
			State:  "WITHDRAWN",
		}, nil
	}
	return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
		"draft %q is already %s; only a pending draft can be withdrawn",
		resourceID, strings.TrimPrefix(current.Status.String(), "QUALITY_SUGGESTION_STATUS_")))
}

// requireWithdrawableDraft proves the proposal is the agent's own check draft. A nil
// agent_id is a human or external proposal, which no agent may take back; a label
// suggestion is not a check draft and has no approval ask behind it.
func requireWithdrawableDraft(agent *store.QualityAgentMessage, suggestion *store.QualitySuggestionMessage) error {
	if agent == nil || suggestion == nil || suggestion.AgentID == nil || *suggestion.AgentID != agent.ID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("the draft belongs to another principal"))
	}
	if suggestion.Kind != storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("only a check draft can be withdrawn"))
	}
	return nil
}
