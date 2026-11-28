package agentrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestConversationWakeForwardsRequestsWithoutAModel(t *testing.T) {
	t.Parallel()

	stores, lead, _ := orchestrationStore()
	stores.requests = []*store.QualityAgentRequestMessage{{
		ResourceID: "req-1", AgentID: 7, WorkspaceID: "ws",
		TargetGUID: "1;shop;;orders", Kind: store.QualityAgentRequestKindDecision,
		Status: store.QualityAgentRequestStatusOpen, Question: "pause the check?",
	}}
	engine := NewEngine(stores)
	outcome, err := engine.conversationWake(context.Background(), lead, 1, nil)
	require.NoError(t, err)
	require.Contains(t, outcome.Summary, "forwarded")
	require.Len(t, stores.messages, 1)
	require.Equal(t, storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER, stores.messages[0].Audience)
	require.Equal(t, storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION, stores.messages[0].Intent)
	require.Contains(t, stores.messages[0].Content, "1;shop;;orders")
}
