//go:build integration

package qualityagent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/Ranxy/metaxisdata/backend/api/v1"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// denyingAuthorizer refuses every target permission. A service path that skips the
// scope check would withdraw a draft the real authorizer would not let it touch.
type denyingAuthorizer struct{}

func (denyingAuthorizer) Check(context.Context, *store.UserMessage, permission.Permission, resource.Ref) (bool, error) {
	return false, nil
}

func (denyingAuthorizer) Scopes(context.Context, *store.UserMessage, permission.Permission) (resource.ScopeSet, error) {
	return resource.ScopeSet{}, nil
}

// The service is where ownership, resource-name normalization and the approval-ask
// drain meet; the store test proves the CAS, but not that the caller runs it and closes
// the card afterwards.
func TestWithdrawAgentDraftClosesTheApprovalAsk(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	sequence := fixtureSequence.Add(1)
	first := queueDraft(t, f, fmt.Sprintf("itsvc%d-a", sequence), "id")
	second := queueDraft(t, f, fmt.Sprintf("itsvc%d-b", sequence), "email")
	ask := seedProposalAsk(t, f)
	service := apiv1.NewDataQualityService(testEnv.Store, nil, nil, nil, nil, nil, nil, nil, nil)

	// The workspace-qualified name the UI uses is accepted, not only the
	// qualitySuggestions/<id> the drafting tool returns.
	result, err := service.WithdrawAgentDraft(f.ctx, int32(f.agent.PrincipalID),
		"workspaces/"+f.env.workspaceID+"/qualitySuggestions/"+first.ResourceID,
		"the primary key already makes this unique")
	require.NoError(t, err)
	require.Equal(t, "WITHDRAWN", result.State)
	require.Equal(t, "the primary key already makes this unique", result.Reason)
	stored, err := testEnv.Store.GetQualitySuggestion(f.ctx, &store.FindQualitySuggestionMessage{
		ResourceID: &first.ResourceID,
	})
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_WITHDRAWN, stored.Status)
	require.Nil(t, stored.DecidedBy)

	// One draft remains, so the approval card stays open.
	stillOpen, err := testEnv.Store.GetQualityAgentAskByResourceID(f.ctx, ask.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusOpen, stillOpen.Status)

	// Retrying the same withdrawal is an idempotent success: a model may retry after a
	// transport error and must not be told the draft is undecided or refused.
	again, err := service.WithdrawAgentDraft(f.ctx, int32(f.agent.PrincipalID),
		first.ResourceID, "the primary key already makes this unique")
	require.NoError(t, err)
	require.Equal(t, "WITHDRAWN", again.State)
	require.Equal(t, "the primary key already makes this unique", again.Reason)

	// The last withdrawal drains the target's approval ask.
	_, err = service.WithdrawAgentDraft(f.ctx, int32(f.agent.PrincipalID),
		second.ResourceID, "the ETL empties this table nightly")
	require.NoError(t, err)
	drained, err := testEnv.Store.GetQualityAgentAskByResourceID(f.ctx, ask.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusResolved, drained.Status)
	open, err := testEnv.Store.ListQualityAgentAsks(f.ctx, f.agent.ID,
		[]store.QualityAgentAskStatus{store.QualityAgentAskStatusOpen}, 10)
	require.NoError(t, err)
	require.Empty(t, open, "the owner's to-do list no longer carries the target")

	// A decided draft is a failed precondition, and a withdrawn one the agent may not
	// reach was never its proposal to begin with.
	third := queueDraft(t, f, fmt.Sprintf("itsvc%d-c", sequence), "created_at")
	_, err = testEnv.Store.DecideQualitySuggestion(f.ctx, third.ResourceID,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED, 1, "noise", time.Now().UTC())
	require.NoError(t, err)
	_, err = service.WithdrawAgentDraft(f.ctx, int32(f.agent.PrincipalID), third.ResourceID, "too late")
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

// The delegation check is part of the withdrawal, not an accident of the store query:
// a denial must leave the draft pending so the person still sees it.
func TestWithdrawAgentDraftRequiresTheTargetDelegation(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	draft := queueDraft(t, f, fmt.Sprintf("itscope%d", fixtureSequence.Add(1)), "id")
	service := apiv1.NewDataQualityService(testEnv.Store, nil, nil, nil, nil, nil, nil, nil, nil)
	service.SetAuthorizer(denyingAuthorizer{})

	_, err := service.WithdrawAgentDraft(f.ctx, int32(f.agent.PrincipalID), draft.ResourceID,
		"the target is emptied by the ETL")
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	stored, err := testEnv.Store.GetQualitySuggestion(f.ctx, &store.FindQualitySuggestionMessage{
		ResourceID: &draft.ResourceID,
	})
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, stored.Status)
}
