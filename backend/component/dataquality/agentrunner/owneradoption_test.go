package agentrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The adoption tool is a capability-granted write, and the runtime only exposes it
// while answering an owner's question with adoption configured. A scheduled or
// event wake must never find it in the schema.
func TestAdoptionToolIsExposedOnlyOnOwnerAsk(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	session := testSession()
	agent := capabilityTestAgent("suggest", "adopt")

	set, err := engine.toolSet(session, agent, &wakeControl{})
	require.NoError(t, err)
	require.NotContains(t, set.Names(), "adopt_pending_drafts")

	set, err = engine.toolSet(session, agent, &wakeControl{ownerAsked: true})
	require.NoError(t, err)
	require.Contains(t, set.Names(), "adopt_pending_drafts")

	set, err = engine.toolSet(session, capabilityTestAgent("suggest"), &wakeControl{ownerAsked: true})
	require.NoError(t, err)
	require.NotContains(t, set.Names(), "adopt_pending_drafts", "the adopt capability is required")
}

// The execution branch refuses a call that bypasses the schema, and runs the
// wired owner-adoption function with the session target and the requested ids.
func TestAdoptionToolExecutesTheOwnerRequest(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	session := testSession()
	agent := capabilityTestAgent("suggest", "adopt")
	var got dataquality.AgentAdoptionRequest
	engine.ownerAdoption = func(_ context.Context, request dataquality.AgentAdoptionRequest) (dataquality.AgentAdoptionReport, error) {
		got = request
		return dataquality.AgentAdoptionReport{
			Adopted: 1,
			Items:   []dataquality.AgentAdoptionItem{{ResourceID: "draft-1", Title: "id unique", Outcome: "adopted"}},
		}, nil
	}

	control := &wakeControl{ownerAsked: true, wake: 4}
	result, err := engine.executeTool(context.Background(), session, agent, "adopt_pending_drafts", `{"draft_ids":["draft-1"]}`, control)
	require.NoError(t, err)
	require.Equal(t, session, got.Session)
	require.Equal(t, []string{"draft-1"}, got.DraftIDs)
	require.Equal(t, int32(4), got.Wake)
	require.Contains(t, result, `"adopted":1`)
	require.True(t, control.adoptionUsed)
}

func TestAdoptionToolRefusesWithoutAnOwnerQuestion(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	agent := capabilityTestAgent("suggest", "adopt")
	called := false
	engine.ownerAdoption = func(context.Context, dataquality.AgentAdoptionRequest) (dataquality.AgentAdoptionReport, error) {
		called = true
		return dataquality.AgentAdoptionReport{}, nil
	}

	_, err := engine.executeTool(context.Background(), testSession(), agent,
		"adopt_pending_drafts", `{"draft_ids":["draft-1"]}`, &wakeControl{})
	require.Error(t, err)
	require.False(t, called, "the tool must not reach the adoption pass without an owner question")
}

// A configuration change rewrites the stored system prompt once and appends one
// notice; the next wake is byte-stable and does not repeat the notice.
func TestConfigurationChangeNoticeIsInjectedOnce(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.agent = capabilityTestAgent("suggest")
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "done"}, {content: "done"}, {content: "done"}}), resolveToModel, nil)
	session := testSession()

	_, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)

	session.WakeCount = 1
	stores.agent.Capabilities = []string{"suggest", "adopt"}
	_, err = engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.Equal(t, 1, stores.systemUpdates)
	require.Contains(t, lastWakeInstruction(stores.messages), "[configuration change]")
	require.Contains(t, lastWakeInstruction(stores.messages), "adopt")

	session.WakeCount = 2
	_, err = engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.NotContains(t, lastWakeInstruction(stores.messages), "[configuration change]")
}

// The system prompt and the tool list are pure functions of the product, the
// capability set and the adoption configuration: no per-wake data may leak into
// them, or provider prefix caching would miss on every wake.
func TestPromptAndToolsAreStableBetweenWakes(t *testing.T) {
	t.Parallel()

	agent := capabilityTestAgent("suggest", "adopt", "patrol")
	for _, spec := range []*Spec{monitorSpec(), triageSpec(), conversationProduct} {
		firstPrompt := renderSystemPrompt(spec, agent, "en-US")
		secondPrompt := renderSystemPrompt(spec, agent, "en-US")
		require.Equal(t, firstPrompt, secondPrompt)
		require.Equal(t, toolSpecNames(toolsFor(spec, agent)), toolSpecNames(toolsFor(spec, agent)))
		require.NotContains(t, firstPrompt, "session-1")
		require.NotContains(t, firstPrompt, "target-1")
	}
}

// lastWakeInstruction returns the newest wake instruction in the transcript.
func lastWakeInstruction(messages []*store.QualityAgentTranscriptMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER &&
			message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE {
			return message.Content
		}
	}
	return ""
}
