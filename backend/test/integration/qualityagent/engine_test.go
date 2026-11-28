//go:build integration

package qualityagent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentrunner"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type scriptedTurn struct {
	content   string
	reasoning string
	toolCalls []llm.ToolCall
}

// scriptedChat plays one wake's turns for the role whose system prompt matches. The
// tests know the request ids, so a turn can name the tool argument statically; the
// real agent loop still executes the tool through the real executor and store.
type scriptedChat struct {
	mu      sync.Mutex
	pending map[string][][]scriptedTurn
}

func newScriptedChat() *scriptedChat {
	return &scriptedChat{pending: map[string][][]scriptedTurn{}}
}

func (s *scriptedChat) next(role string, turns ...scriptedTurn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[role] = append(s.pending[role], turns)
}

func (s *scriptedChat) chat(_ context.Context, cfg agent.Config) <-chan agent.Event {
	role := chatRole(cfg)
	s.mu.Lock()
	wakes := s.pending[role]
	var turns []scriptedTurn
	if len(wakes) > 0 {
		turns = wakes[0]
		s.pending[role] = wakes[1:]
	}
	s.mu.Unlock()
	if len(turns) == 0 {
		turns = []scriptedTurn{{content: "done"}}
	}

	events := make(chan agent.Event, 32)
	go func() {
		defer close(events)
		for index, turn := range turns {
			number := index + 1
			events <- agent.Event{Type: agent.EventTurnStart, Turn: number}
			events <- agent.Event{Type: agent.EventUsage, Turn: number,
				Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}}
			assistant := agent.Message{
				Role:      "assistant",
				Content:   turn.content,
				Reasoning: turn.reasoning,
				ToolCalls: turn.toolCalls,
			}
			events <- agent.Event{Type: agent.EventAssistant, Assistant: &assistant, Turn: number}
			for _, call := range turn.toolCalls {
				results, err := cfg.Executor(call)
				content := ""
				if len(results) > 0 {
					content = results[0].Content
				}
				event := agent.Event{Type: agent.EventToolEnd, ToolCall: &call, ToolResult: content, Turn: number}
				if err != nil {
					event.ToolResult = "error: " + err.Error()
					event.ToolError = err.Error()
				}
				events <- event
			}
			if len(turn.toolCalls) == 0 {
				events <- agent.Event{Type: agent.EventEnd, Turn: number}
				return
			}
		}
		events <- agent.Event{Type: agent.EventEnd}
	}()
	return events
}

func chatRole(cfg agent.Config) string {
	for _, message := range cfg.History {
		if message.Role != "system" {
			continue
		}
		switch {
		case strings.Contains(message.Content, "You are the lead of a quality agent"):
			return "lead"
		case strings.Contains(message.Content, "You are a fixed subagent of the quality agent"):
			return "target"
		default:
		}
	}
	return "unknown"
}

func scriptedToolCall(id, name, arguments string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}

func fakeResolveModel(context.Context, *store.QualityAgentMessage) (llm.ResolvedConfig, bool, error) {
	return llm.ResolvedConfig{ModelName: "integration-model", ContextWindow: 128_000, MaxOutputTokens: 8_000}, true, nil
}

func newScriptedEngine(t *testing.T, f *draftAgent) (*agentrunner.Engine, *scriptedChat) {
	t.Helper()
	script := newScriptedChat()
	engine := agentrunner.NewModelEngine(f.env.store, script.chat, fakeResolveModel, nil)
	return engine, script
}

func (f *draftAgent) reloadSession(t *testing.T) *store.QualityAgentSessionMessage {
	t.Helper()

	session, err := f.env.store.GetQualityAgentSession(f.ctx, f.session.ResourceID)
	require.NoError(t, err)
	require.NotNil(t, session)
	return session
}

func (f *draftAgent) onlyRequest(t *testing.T) *store.QualityAgentRequestMessage {
	t.Helper()

	requests := f.activeRequests(t)
	require.Len(t, requests, 1)
	return requests[0]
}

func (f *draftAgent) onlyAsk(t *testing.T) *store.QualityAgentAskMessage {
	t.Helper()

	asks, err := f.env.store.ListQualityAgentAsks(f.ctx, f.agent.ID, nil, 10)
	require.NoError(t, err)
	require.Len(t, asks, 1)
	return asks[0]
}

// TestQualityAgentEngineOwnerLoop drives the whole coordination path without HTTP:
// the subagent's model asks, the lead's model forwards, the owner's answer is appended,
// and the lead's model answers the request. Every step executes the real tool and store.
func TestQualityAgentEngineOwnerLoop(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	engine, script := newScriptedEngine(t, f)

	script.next("target",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_question", "send_lead_message",
			`{"intent":"QUESTION","message":"Should we raise the timeout?","question_key":"timeout"}`)}},
		scriptedTurn{content: "asked the lead"},
	)
	_, err := engine.Wake(context.Background(), f.session, agentrunner.Options{})
	require.NoError(t, err)
	request := f.onlyRequest(t)
	require.Equal(t, store.QualityAgentRequestStatusOpen, request.Status)
	require.Equal(t, "timeout", request.QuestionKey)
	require.Len(t, f.notices(t), 1)

	lead := f.reloadLead(t)
	script.next("lead",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_forward", "forward_request",
			`{"requests":["qualityAgentRequests/`+request.ResourceID+`"],"question":"Should we raise the timeout?"}`)}},
		scriptedTurn{content: "forwarded"},
	)
	_, err = engine.Wake(context.Background(), lead, agentrunner.Options{})
	require.NoError(t, err)
	forwarded := f.getRequest(t, request.ResourceID)
	require.Equal(t, store.QualityAgentRequestStatusForwarded, forwarded.Status)
	require.NotNil(t, forwarded.AskID)
	ask := f.onlyAsk(t)
	require.Equal(t, store.QualityAgentAskStatusOpen, ask.Status)
	require.NotNil(t, ask.ConversationSessionID)

	// The owner's reply is an ASK-origin user row; the API writes this shape. The
	// answer seq is recorded on the ask before the lead's wake.
	ownerMessage := &store.QualityAgentTranscriptMessage{
		SessionID:      lead.ID,
		WorkspaceID:    f.env.workspaceID,
		Wake:           lead.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        "Yes, raise it to five minutes.",
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		ReplyTo:        "qualityAgentAsks/" + ask.ResourceID,
		IdempotencyKey: "it-engine-owner-answer",
	}
	appended, err := f.env.store.AppendQualityAgentTranscriptMessage(f.ctx, ownerMessage)
	require.NoError(t, err)
	_, applied, err := f.env.store.MarkQualityAgentAskAnswered(f.ctx, ask.ResourceID, 1, appended.Seq, nowUTC())
	require.NoError(t, err)
	require.True(t, applied)

	lead = f.reloadLead(t)
	script.next("lead",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_answer", "answer_request",
			`{"request":"qualityAgentRequests/`+request.ResourceID+`","answer":"raise it to five minutes"}`)}},
		scriptedTurn{content: "answered"},
	)
	_, err = engine.Wake(context.Background(), lead, agentrunner.Options{})
	require.NoError(t, err)
	answered := f.getRequest(t, request.ResourceID)
	require.Equal(t, store.QualityAgentRequestStatusAnswered, answered.Status)
	require.Equal(t, "raise it to five minutes", answered.Resolution)
	drained, err := f.env.store.GetQualityAgentAskByResourceID(f.ctx, ask.ResourceID)
	require.NoError(t, err)
	require.Equal(t, store.QualityAgentAskStatusResolved, drained.Status)

	answers, err := f.env.store.ListQualityAgentTranscriptMessages(f.ctx, &store.FindQualityAgentTranscriptMessage{
		SessionID: f.session.ID,
		Limit:     50,
	})
	require.NoError(t, err)
	var answer *store.QualityAgentTranscriptMessage
	for _, message := range answers {
		if message.Intent == storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER {
			answer = message
		}
	}
	require.NotNil(t, answer, "the subagent receives the lead's answer")
	require.Contains(t, answer.Content, "raise it to five minutes")
	require.Equal(t, storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL, answer.Audience)
	require.Contains(t, answer.ReplyTo, request.ResourceID)
}

// A retry that only changes whitespace is the same question in the real store, and a
// drifted key is caught by the fingerprint; neither produces a second request.
func TestQualityAgentEngineQuestionIdentity(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	engine, script := newScriptedEngine(t, f)

	script.next("target",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_one", "send_lead_message",
			`{"intent":"QUESTION","message":"Should we pause?","question_key":"pause-check"}`)}},
		scriptedTurn{content: "asked"},
	)
	_, err := engine.Wake(context.Background(), f.session, agentrunner.Options{})
	require.NoError(t, err)
	first := f.onlyRequest(t)

	script.next("target",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_two", "send_lead_message",
			`{"intent":"QUESTION","message":" Should\nwe pause? ","question_key":"pause-check"}`)}},
		scriptedTurn{content: "retried"},
	)
	_, err = engine.Wake(context.Background(), f.reloadSession(t), agentrunner.Options{})
	require.NoError(t, err)
	require.Len(t, f.activeRequests(t), 1)
	require.Equal(t, " Should\nwe pause? ", f.getRequest(t, first.ResourceID).Question)

	script.next("target",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_three", "send_lead_message",
			`{"intent":"QUESTION","message":" Should\nwe pause? ","question_key":"pause-check-drifted"}`)}},
		scriptedTurn{content: "drifted key retried"},
	)
	_, err = engine.Wake(context.Background(), f.reloadSession(t), agentrunner.Options{})
	require.NoError(t, err)
	require.Len(t, f.activeRequests(t), 1, "the fingerprint catches a drifted key")
}

// A case id from another target must not become a request; the real case lookup and
// ownership check run before the write.
func TestQualityAgentEngineRejectsAnotherTargetsCase(t *testing.T) {
	t.Parallel()

	f := newDraftAgent(t)
	otherTarget := f.targetGUID + "-other"
	otherCase, created, err := f.env.store.CreateQualityAgentCase(f.ctx, &store.CreateQualityAgentCaseInput{
		WorkspaceID: f.env.workspaceID, AgentID: f.agent.ID, TargetGUID: otherTarget,
		TargetType: storepb.MetaType_TABLE, Summary: "another target's case",
	})
	require.NoError(t, err)
	require.True(t, created)
	engine, script := newScriptedEngine(t, f)
	script.next("target",
		scriptedTurn{toolCalls: []llm.ToolCall{scriptedToolCall("call_case", "send_lead_message",
			`{"intent":"QUESTION","message":"Can this case move?","case":"`+otherCase.ResourceID+`"}`)}},
		scriptedTurn{content: "the case was refused"},
	)
	_, err = engine.Wake(context.Background(), f.session, agentrunner.Options{})
	require.NoError(t, err, "a tool refusal ends the wake, not the session")
	require.Empty(t, f.activeRequests(t), "the other target's case creates no request")
}
