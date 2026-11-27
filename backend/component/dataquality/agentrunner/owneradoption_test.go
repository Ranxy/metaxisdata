package agentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The adoption tool is a capability-granted write. Its schema is gated only by the
// capability and the adoption configuration, not by the wake kind, so the tool
// prefix stays stable; the execution branch refuses it unless the wake answers an
// owner question.
func TestAdoptionToolSchemaIsCapabilityGated(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	session := testSession()
	session.Kind = 2
	session.TargetGUID = ""
	agent := capabilityTestAgent("suggest", "adopt")

	set, err := engine.toolSet(session, agent, &wakeControl{})
	require.NoError(t, err)
	require.Contains(t, set.Names(), "adopt_pending_drafts")

	set, err = engine.toolSet(session, capabilityTestAgent("suggest"), &wakeControl{})
	require.NoError(t, err)
	require.NotContains(t, set.Names(), "adopt_pending_drafts", "the adopt capability is required")

	off := capabilityTestAgent("suggest", "adopt")
	off.AdoptionMode = store.QualityAgentAdoptionModeOff
	set, err = engine.toolSet(session, off, &wakeControl{})
	require.NoError(t, err)
	require.NotContains(t, set.Names(), "adopt_pending_drafts", "adoption off removes the tool")
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
		require.NotContains(t, firstPrompt, "session-1")
		require.NotContains(t, firstPrompt, "target-1")
		engine := NewEngine(newFakeStore())
		first, err := engine.toolSet(testSession(), agent, &wakeControl{})
		require.NoError(t, err)
		second, err := engine.toolSet(testSession(), agent, &wakeControl{})
		require.NoError(t, err)
		require.Equal(t, first.Schemas(), second.Schemas(), "the tool schema is deterministic")
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

// An agent that selected adopt but has the mode off must not read an authority it
// does not have: the fragment is dropped, the policy section says unavailable, and
// the tool is absent even on an owner question.
func TestAdoptionUnavailableKeepsTheAuthorityOutOfThePrompt(t *testing.T) {
	t.Parallel()

	agent := capabilityTestAgent("suggest", "adopt")
	agent.AdoptionMode = store.QualityAgentAdoptionModeOff
	prompt := renderSystemPrompt(monitorSpec(), agent, "en-US")
	require.NotContains(t, prompt, "Adoption authority:")
	require.Contains(t, prompt, "Adoption is unavailable")

	engine := NewEngine(newFakeStore())
	set, err := engine.toolSet(testSession(), agent, &wakeControl{})
	require.NoError(t, err)
	require.NotContains(t, set.Names(), "adopt_pending_drafts")
}

// A triage wake that already adopted through the owner-requested tool must not run
// the automatic adoption pass again for the same wake.
func TestWakeSkipsTheAutomaticPassWhenTheToolRan(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.agent = capabilityTestAgent("suggest", "adopt")
	stores.messages = append(stores.messages, &store.QualityAgentTranscriptMessage{
		SessionID: 1,
		Seq:       1,
		Role:      storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:    storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:   "adopt the pending drafts",
	})
	stores.nextSeq = 1
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "adopting", toolCalls: []llm.ToolCall{toolCall("call_1", "adopt_pending_drafts", `{"draft_ids":["draft-1"]}`)}},
		{content: "done"},
	}), resolveToModel, nil)
	engine.ownerAdoption = func(context.Context, dataquality.AgentAdoptionRequest) (dataquality.AgentAdoptionReport, error) {
		return dataquality.AgentAdoptionReport{Adopted: 1}, nil
	}
	autoCalled := false
	engine.SetAdoption(func(context.Context, *store.QualityAgentSessionMessage, time.Time) {
		autoCalled = true
	})
	session := testSession()
	session.Kind = 2
	session.TargetGUID = ""
	session.Mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_UNSPECIFIED

	outcome, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.True(t, outcome.adoptionUsed, "the tool reports that it adopted")
	require.False(t, autoCalled, "the automatic pass is suppressed for this wake")
}

// Two wakes with an unchanged configuration must not rewrite the stored SYSTEM row,
// which is the property that keeps the provider prefix cache valid.
func TestUnchangedConfigurationDoesNotRewriteTheSystemPrompt(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.agent = capabilityTestAgent("suggest", "adopt", "patrol")
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "done"}, {content: "done"}}), resolveToModel, nil)
	session := testSession()

	_, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	firstSystem := systemMessageContent(stores.messages)
	require.NotEmpty(t, firstSystem)

	session.WakeCount = 1
	_, err = engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.Zero(t, stores.systemUpdates, "an unchanged configuration does not rewrite the system row")
	require.Equal(t, firstSystem, systemMessageContent(stores.messages))
}

// systemMessageContent returns the stored system prompt.
func systemMessageContent(messages []*store.QualityAgentTranscriptMessage) string {
	for _, message := range messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM {
			return message.Content
		}
	}
	return ""
}
