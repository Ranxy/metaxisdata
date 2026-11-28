package agentrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func orchestrationStore() (*fakeStore, *store.QualityAgentSessionMessage, *store.QualityAgentSessionMessage) {
	lead := &store.QualityAgentSessionMessage{
		ID: 1, ResourceID: "lead", AgentID: 7, WorkspaceID: "ws",
		Role: storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_LEAD, Kind: 2,
	}
	subagent := &store.QualityAgentSessionMessage{
		ID: 2, ResourceID: "sub", AgentID: 7, WorkspaceID: "ws",
		TargetGUID: "1;shop;;orders", TargetType: storepb.MetaType_TABLE,
		Role: storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT, Kind: 1,
		ParentSessionID: &lead.ID, SubagentName: "orders",
	}
	stores := newFakeStore()
	stores.lead = lead
	stores.subagents = map[string]*store.QualityAgentSessionMessage{"orders": subagent}
	stores.sessionsByID = map[int64]*store.QualityAgentSessionMessage{1: lead, 2: subagent}
	return stores, lead, subagent
}

func TestSendLeadMessageDeliversToTheLead(t *testing.T) {
	t.Parallel()

	stores, lead, subagent := orchestrationStore()
	engine := NewEngine(stores)
	result, err := engine.sendLeadMessage(context.Background(), subagent, `{"message":"the check cannot run","intent":"QUESTION"}`)
	require.NoError(t, err)
	require.Contains(t, result, `"delivered":true`)
	require.Len(t, stores.messages, 1)
	require.Equal(t, lead.ID, stores.messages[0].SessionID)
	require.Equal(t, storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT, stores.messages[0].Origin)
	require.Equal(t, "orders", stores.messages[0].SenderName)
	require.Equal(t, storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL, stores.messages[0].Audience)
}

func TestSendSubagentMessageDeliversToTheNamedChild(t *testing.T) {
	t.Parallel()

	stores, lead, subagent := orchestrationStore()
	engine := NewEngine(stores)
	result, err := engine.sendSubagentMessage(context.Background(), lead, `{"agent":"orders","message":"please investigate","intent":"ASSIGN"}`)
	require.NoError(t, err)
	require.Contains(t, result, `"delivered":true`)
	require.Len(t, stores.messages, 1)
	require.Equal(t, subagent.ID, stores.messages[0].SessionID)
	require.Equal(t, storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ASSIGN, stores.messages[0].Intent)
}

func TestSendSubagentMessageRefusesAnUnknownChild(t *testing.T) {
	t.Parallel()

	stores, lead, _ := orchestrationStore()
	engine := NewEngine(stores)
	_, err := engine.sendSubagentMessage(context.Background(), lead, `{"agent":"missing","message":"hi"}`)
	require.Error(t, err)
	require.Empty(t, stores.messages)
}

// A case-scoped question key must not share the namespace of the case verbs: a key
// named control would otherwise collide with control_check's decision request.
func TestCaseQuestionKeyDoesNotShareTheControlNamespace(t *testing.T) {
	t.Parallel()

	require.Equal(t, "target:t:case:c1:question:control", questionDedupeKey("t", "c1", "control", "Resume the check?"))
	require.Equal(t, "target:t:case:c1:question", questionDedupeKey("t", "c1", "", "Resume the check?"))
	require.NotEqual(t, "target:t:case:c1:control", questionDedupeKey("t", "c1", "control", "Resume the check?"))
}

// An ask carries the choices the owner can click and the question id the answer
// echoes. Without them the browser has nothing to render and the reply is prose.
func TestAskOwnerCarriesOptionsAndTheQuestionID(t *testing.T) {
	t.Parallel()

	stores, lead, _ := orchestrationStore()
	engine := NewEngine(stores)
	result, err := engine.askOwner(context.Background(), lead, `{
		"question": "Keep the check?",
		"header": "Confirm",
		"options": [
			{"label": "Keep it", "description": "Continue watching."},
			{"label": "Pause it", "description": "Stop the failures."}
		],
		"multi_select": false
	}`, nil)
	require.NoError(t, err)
	require.Contains(t, result, `"questions":["q1"]`)
	require.Len(t, stores.asks, 1)
	ask := stores.asks[0]
	require.Equal(t, "Keep the check?", ask.Question)
	require.Len(t, ask.Questions, 1)
	require.Equal(t, "q1", ask.Questions[0].Id)
	require.Len(t, ask.Questions[0].Options, 2)
	require.Equal(t, "Keep it", ask.Questions[0].Options[0].Label)
}

// The option set is part of the ask's identity: the same words with different
// choices are a different decision and must not dedupe onto the old question.
func TestAskDedupeKeyIncludesTheOptions(t *testing.T) {
	t.Parallel()

	keep := []*storepb.QualityAgentAskQuestion{{
		Id: "q1", Question: "Keep the check?",
		Options: []*storepb.QualityAgentAskQuestionOption{{Label: "Keep it"}},
	}}
	pause := []*storepb.QualityAgentAskQuestion{{
		Id: "q1", Question: "Keep the check?",
		Options: []*storepb.QualityAgentAskQuestionOption{{Label: "Pause it"}},
	}}
	require.NotEqual(t, askDedupeKey(keep, nil), askDedupeKey(pause, nil))
	require.Equal(t, askDedupeKey(keep, nil), askDedupeKey(keep, nil))
}

func TestSendLeadMessageStoresTheOfferedOptions(t *testing.T) {
	t.Parallel()

	stores, lead, subagent := orchestrationStore()
	engine := NewEngine(stores)
	result, err := engine.sendLeadMessage(context.Background(), subagent, `{
		"intent": "QUESTION",
		"message": "Keep the check?",
		"options": [
			{"label": "Keep it", "description": "Continue watching."},
			{"label": "Pause it", "description": "Stop the failures."}
		],
		"multi_select": true
	}`)
	require.NoError(t, err)
	require.Contains(t, result, `"delivered":true`)
	require.Len(t, stores.requests, 1)
	request := stores.requests[0]
	require.Equal(t, subagent.ID, *request.OriginSessionID)
	require.Len(t, request.Options, 2)
	require.Equal(t, "Keep it", request.Options[0].Label)
	require.True(t, request.MultiSelect)

	listed, err := engine.listRequests(context.Background(), lead, `{}`)
	require.NoError(t, err)
	require.Contains(t, listed, `"options"`)
	require.Contains(t, listed, "Keep it")
	require.Contains(t, listed, `"multi_select":true`)
}

func TestForwardRequestCopiesTheSingleRequestsOptions(t *testing.T) {
	t.Parallel()

	stores, _, subagent := orchestrationStore()
	request := &store.QualityAgentRequestMessage{
		ID: 11, ResourceID: "r1", WorkspaceID: "ws", AgentID: 7,
		Status: store.QualityAgentRequestStatusOpen, Question: "Keep the check?",
		OriginSessionID: &subagent.ID,
		Options: []*storepb.QualityAgentAskQuestionOption{
			{Label: "Keep it", Description: "Continue watching."},
		},
		MultiSelect: true,
	}
	stores.requestByID = map[string]*store.QualityAgentRequestMessage{"r1": request}
	stores.requests = []*store.QualityAgentRequestMessage{request}
	engine := NewEngine(stores)
	_, err := engine.forwardRequest(context.Background(), stores.lead, `{
		"requests": ["qualityAgentRequests/r1"],
		"questions": [{"question": "Keep the check?"}]
	}`, nil)
	require.NoError(t, err)
	require.Len(t, stores.asks, 1)
	require.Len(t, stores.asks[0].Questions, 1)
	require.Len(t, stores.asks[0].Questions[0].Options, 1)
	require.Equal(t, "Keep it", stores.asks[0].Questions[0].Options[0].Label)
	require.True(t, stores.asks[0].Questions[0].MultiSelect)
}

func TestAskOwnerBatchNamesDisjointRequestSubsets(t *testing.T) {
	t.Parallel()

	stores, lead, subagent := orchestrationStore()
	first := &store.QualityAgentRequestMessage{
		ID: 11, ResourceID: "r1", WorkspaceID: "ws", AgentID: 7,
		Status: store.QualityAgentRequestStatusOpen, Question: "First?",
		OriginSessionID: &subagent.ID,
	}
	second := &store.QualityAgentRequestMessage{
		ID: 12, ResourceID: "r2", WorkspaceID: "ws", AgentID: 7,
		Status: store.QualityAgentRequestStatusOpen, Question: "Second?",
		OriginSessionID: &subagent.ID,
	}
	stores.requestByID = map[string]*store.QualityAgentRequestMessage{"r1": first, "r2": second}
	stores.requests = []*store.QualityAgentRequestMessage{first, second}
	engine := NewEngine(stores)
	_, err := engine.askOwner(context.Background(), lead, `{
		"requests": ["qualityAgentRequests/r1", "qualityAgentRequests/r2"],
		"questions": [
			{"id": "first", "question": "First?", "requests": ["qualityAgentRequests/r1"]},
			{"id": "second", "question": "Second?", "requests": ["qualityAgentRequests/r2"]}
		]
	}`, nil)
	require.NoError(t, err)
	require.Len(t, stores.asks, 1)
	questions := stores.asks[0].Questions
	require.Len(t, questions, 2)
	require.Equal(t, []string{"workspaces/ws/qualityAgentRequests/r1"}, questions[0].Requests)
	require.Equal(t, []string{"workspaces/ws/qualityAgentRequests/r2"}, questions[1].Requests)
}

func TestAskOwnerBatchRejectsOverlappingRequests(t *testing.T) {
	t.Parallel()

	stores, lead, subagent := orchestrationStore()
	request := &store.QualityAgentRequestMessage{
		ID: 11, ResourceID: "r1", WorkspaceID: "ws", AgentID: 7,
		Status: store.QualityAgentRequestStatusOpen, Question: "First?",
		OriginSessionID: &subagent.ID,
	}
	stores.requestByID = map[string]*store.QualityAgentRequestMessage{"r1": request}
	stores.requests = []*store.QualityAgentRequestMessage{request}
	engine := NewEngine(stores)
	_, err := engine.askOwner(context.Background(), lead, `{
		"requests": ["qualityAgentRequests/r1"],
		"questions": [
			{"id": "first", "question": "First?", "requests": ["qualityAgentRequests/r1"]},
			{"id": "second", "question": "Second?", "requests": ["qualityAgentRequests/r1"]}
		]
	}`, nil)
	require.ErrorContains(t, err, "decided by both")
	require.Empty(t, stores.asks)
}
