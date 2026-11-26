package agentrunner

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeStore is the in-memory slice of the store the engine tests need.
type fakeStore struct {
	messages      []*store.QualityAgentTranscriptMessage
	memories      []*store.QualityMemoryMessage
	executions    map[int64]*store.QualityExecutionMessage
	memoryInputs  []*store.CreateQualityMemoryInput
	nextSeq       int64
	systemUpdates int
	contextUsage  []*storepb.QualityAgentSessionContextUsage
	agent         *store.QualityAgentMessage
	openCases     []*store.QualityAgentCaseMessage
	participation *store.QualityAgentParticipationLevels
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		executions: map[int64]*store.QualityExecutionMessage{},
		agent:      &store.QualityAgentMessage{ID: 1, State: store.QualityAgentStateActive},
	}
}

func (*fakeStore) QualityAgentEnrollmentSummary(context.Context, string, int64) ([]*store.QualityAgentDatabaseEnrollmentSummary, error) {
	return nil, nil
}

func (*fakeStore) ListQualityAgentEnrollments(context.Context, *store.FindQualityAgentEnrollmentMessage) ([]*store.QualityAgentEnrollmentMessage, error) {
	return nil, nil
}

func (*fakeStore) ListQualitySuggestions(context.Context, *store.FindQualitySuggestionMessage) ([]*store.QualitySuggestionMessage, error) {
	return nil, nil
}

func (*fakeStore) GetQualityAgentAdoptionStatus(context.Context, int64, time.Time) (*store.QualityAgentAdoptionStatusMessage, error) {
	return &store.QualityAgentAdoptionStatusMessage{}, nil
}

func (f *fakeStore) GetQualityAgentByID(context.Context, int64) (*store.QualityAgentMessage, error) {
	return f.agent, nil
}

// GetUserByID returns the session's agent principal, which the permission gate
// checks a tool against.
func (*fakeStore) GetUserByID(context.Context, int) (*store.UserMessage, error) {
	return &store.UserMessage{ID: 7, Type: storepb.PrincipalType_SERVICE_ACCOUNT}, nil
}

func (*fakeStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return nil, nil
}

func (*fakeStore) GetQualityContract(context.Context, string, storepb.MetaType) (*store.QualityContractMessage, error) {
	return nil, nil
}

// participation is the delegation cascade the prompt and the verbs read.
func (f *fakeStore) GetQualityAgentParticipation(context.Context, int64, string, string) (*store.QualityAgentParticipationLevels, error) {
	if f.participation == nil {
		return &store.QualityAgentParticipationLevels{Agent: store.QualityAgentParticipationRepair}, nil
	}
	return f.participation, nil
}

// openCases is what the wake prompt and the next-wake merge read.
func (f *fakeStore) ListOpenQualityAgentCasesByTarget(context.Context, int64, string, int) ([]*store.QualityAgentCaseMessage, error) {
	return f.openCases, nil
}

func (*fakeStore) ListQualityExecutions(context.Context, *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error) {
	return nil, nil
}

func (f *fakeStore) GetQualityExecution(_ context.Context, id int64) (*store.QualityExecutionMessage, error) {
	return f.executions[id], nil
}

func (f *fakeStore) ListQualityMemories(context.Context, *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error) {
	return f.memories, nil
}

func (f *fakeStore) CreateQualityMemory(_ context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error) {
	f.memoryInputs = append(f.memoryInputs, input)
	memory := &store.QualityMemoryMessage{
		ResourceID: input.ResourceID,
		Subject:    input.Subject,
		Statement:  input.Statement,
		Status:     storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED,
		Evidence:   input.Evidence,
		ExpiresAt:  input.ExpiresAt,
	}
	f.memories = append(f.memories, memory)
	return memory, nil
}

func (f *fakeStore) AppendQualityAgentTranscriptMessage(_ context.Context, input *store.QualityAgentTranscriptMessage) (*store.QualityAgentTranscriptMessage, error) {
	if input.IdempotencyKey != "" {
		for _, existing := range f.messages {
			if existing.SessionID == input.SessionID && existing.IdempotencyKey == input.IdempotencyKey {
				return existing, nil
			}
		}
	}
	f.nextSeq++
	message := *input
	message.ID = f.nextSeq
	message.Seq = f.nextSeq
	f.messages = append(f.messages, &message)
	return &message, nil
}

func (f *fakeStore) UpdateQualityAgentSystemMessage(_ context.Context, sessionID int64, content string) (bool, error) {
	for _, message := range f.messages {
		if message.SessionID == sessionID && message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM {
			message.Content = content
			f.systemUpdates++
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) UpdateQualityAgentSessionContextUsage(_ context.Context, _ string, usage *storepb.QualityAgentSessionContextUsage) (bool, error) {
	f.contextUsage = append(f.contextUsage, usage)
	return true, nil
}

func (f *fakeStore) ListQualityAgentTranscriptMessages(_ context.Context, find *store.FindQualityAgentTranscriptMessage) ([]*store.QualityAgentTranscriptMessage, error) {
	var matches []*store.QualityAgentTranscriptMessage
	for _, message := range f.messages {
		if message.SessionID != find.SessionID || message.Seq <= find.AfterSeq {
			continue
		}
		if find.ExcludeEngine && message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE {
			continue
		}
		if find.Role != nil && message.Role != *find.Role {
			continue
		}
		if find.InputsOnly {
			isUser := message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER
			isEvent := message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT
			isAsk := message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK
			if !isUser || (!isEvent && !isAsk) {
				continue
			}
		}
		matches = append(matches, message)
	}
	slices.SortFunc(matches, func(a, b *store.QualityAgentTranscriptMessage) int {
		switch {
		case a.Seq < b.Seq:
			return -1
		case a.Seq > b.Seq:
			return 1
		default:
			return 0
		}
	})
	if find.NewestFirst {
		slices.Reverse(matches)
	}
	if find.Offset > 0 && find.Offset < len(matches) {
		matches = matches[find.Offset:]
	}
	limit := find.Limit
	if limit <= 0 {
		limit = 200
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (*fakeStore) UpdateQualityAgentSessionCompaction(context.Context, string, string, int64) (bool, error) {
	return true, nil
}

func toolCall(id, name, arguments string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}

// scriptedTurn is one LLM turn a scripted provider plays.
type scriptedTurn struct {
	content   string
	reasoning string
	toolCalls []llm.ToolCall
}

// scriptedChat plays a fixed sequence of LLM turns and executes their tool calls
// through the wake's executor, like the real loop does.
func scriptedChat(turns []scriptedTurn) Chat {
	return func(_ context.Context, cfg agent.Config) <-chan agent.Event {
		events := make(chan agent.Event, 32)
		go func() {
			defer close(events)
			for i, turn := range turns {
				number := i + 1
				events <- agent.Event{Type: agent.EventTurnStart, Turn: number}
				events <- agent.Event{Type: agent.EventUsage, Turn: number,
					Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}}
				assistant := agent.Message{Role: "assistant", Content: turn.content, Reasoning: turn.reasoning, ToolCalls: turn.toolCalls}
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
}

func resolveToModel(context.Context, *store.QualityAgentMessage) (llm.ResolvedConfig, bool, error) {
	return llm.ResolvedConfig{ModelName: "test-model"}, true, nil
}

func testSession() *store.QualityAgentSessionMessage {
	return &store.QualityAgentSessionMessage{
		ID:               1,
		ResourceID:       "session-1",
		WorkspaceID:      "ws",
		TargetGUID:       "target-1",
		TargetType:       storepb.MetaType(1),
		AgentPrincipalID: 7,
		State:            storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
		EngineState:      &storepb.QualityAgentSessionEngineState{},
	}
}

// engineNoteContent joins the runner notes of a transcript, which is where a
// no-model wake records what it reviewed.
func engineNoteContent(messages []*store.QualityAgentTranscriptMessage) string {
	var parts []string
	for _, message := range messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE {
			parts = append(parts, message.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func roles(messages []*store.QualityAgentTranscriptMessage) []storepb.QualityAgentMessageRole {
	result := make([]storepb.QualityAgentMessageRole, 0, len(messages))
	for _, message := range messages {
		result = append(result, message.Role)
	}
	return result
}

// One wake must append the wake message, the assistant turns, the tool result and the
// engine notes in order, so the transcript is the conversation the model saw.
func TestWakeRecordsTheConversation(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "let me look", reasoning: "check the plan first", toolCalls: []llm.ToolCall{toolCall("call_1", "get_target_summary", "{}")}},
		{content: "nothing needs attention"},
	}), resolveToModel, nil)

	outcome, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.False(t, outcome.Blocked)
	require.Nil(t, outcome.NextWakeAt)
	require.Zero(t, outcome.ProcessedSeq, "assistant/tool/engine rows are not inputs")
	require.Contains(t, outcome.Summary, "wake finished")

	require.Equal(t, []storepb.QualityAgentMessageRole{
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE,
	}, roles(stores.messages))

	assistant := stores.messages[3]
	require.Equal(t, "let me look", assistant.Content)
	require.Equal(t, "check the plan first", assistant.Reasoning)
	require.Len(t, assistant.ToolCalls.GetToolCalls(), 1)
	require.Equal(t, "get_target_summary", assistant.ToolCalls.GetToolCalls()[0].GetName())
	require.Equal(t, int32(110), assistant.Tokens)

	require.Equal(t, "call_1", stores.messages[4].ToolCallID)
	require.Contains(t, stores.messages[4].Content, "gap")
}

// schedule_wake moves the next wake earlier without ending the wake by itself.
func TestWakeSchedulesAnEarlierWake(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "more work to do", toolCalls: []llm.ToolCall{toolCall("call_1", "schedule_wake", `{"after_minutes":5,"reason":"wait for the next sync"}`)}},
		{content: "stopping for now"},
	}), resolveToModel, nil)

	before := time.Now()
	outcome, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.NotNil(t, outcome.NextWakeAt)
	require.WithinDuration(t, before.Add(5*time.Minute), *outcome.NextWakeAt, time.Minute)
	require.Contains(t, outcome.Summary, "scheduled an earlier wake")
}

// The same blocker has to persist for three wakes before the session parks for a person.
func TestWakeParksTheSessionAfterRepeatedBlocker(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	session := testSession()
	session.EngineState = &storepb.QualityAgentSessionEngineState{BlockedCode: "needs-data", BlockedRounds: 2}
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "blocked", toolCalls: []llm.ToolCall{toolCall("call_1", "block_session", `{"code":"needs-data","reason":"the source table is empty"}`)}},
		{content: "stopping"},
	}), resolveToModel, nil)

	outcome, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.True(t, outcome.Blocked)
	require.Equal(t, "needs-data", outcome.BlockedCode)
	require.Contains(t, outcome.BlockedReason, "source table is empty")
	require.Equal(t, int32(3), outcome.EngineState.GetBlockedRounds())
}

// Without a model the wake still surveys the target and writes a note to the transcript.
func TestWakeFallsBackToTheDeterministicSurvey(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewEngine(stores)

	outcome, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.False(t, outcome.Blocked)
	require.NotNil(t, outcome.NextWakeAt)
	require.WithinDuration(t, time.Now().Add(maintainWake), *outcome.NextWakeAt, time.Minute)
	require.Contains(t, outcome.Summary, "surveyed")
}

// An event whose execution failed or errored is reviewed deterministically and its
// facts stay in the transcript. An error is as much something the agent should look
// at as a FAIL; without a model there is no root cause to conclude, so no knowledge
// object is written.
func TestWakeReviewsFailedAndErroredExecutionsWithoutAModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		executionID int64
		status      storepb.QualityStatus
		failedRows  int64
		errorCode   string
		errorReason string
		wantNote    string
	}{
		{
			name:        "failed execution",
			executionID: 7,
			status:      storepb.QualityStatus_QUALITY_STATUS_FAIL,
			failedRows:  12,
			wantNote:    "execution qualityExecutions/7 finished QUALITY_STATUS_FAIL with 12 failed rows",
		},
		{
			name:        "errored execution",
			executionID: 9,
			status:      storepb.QualityStatus_QUALITY_STATUS_ERROR,
			errorCode:   "NO_READ_ONLY_SOURCE",
			errorReason: "the source is not readable",
			wantNote:    "execution qualityExecutions/9 ended NO_READ_ONLY_SOURCE: the source is not readable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stores := newFakeStore()
			stores.executions[test.executionID] = &store.QualityExecutionMessage{
				ID:           test.executionID,
				TargetGUID:   "target-1",
				TargetType:   storepb.MetaType(1),
				Status:       test.status,
				FailedRows:   test.failedRows,
				ErrorCode:    test.errorCode,
				ErrorMessage: test.errorReason,
			}
			reference := fmt.Sprintf("qualityExecutions/%d", test.executionID)
			message, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
				SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
				Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
				Content: "execution event", Reference: reference,
				IdempotencyKey: fmt.Sprintf("event:execution:%d", test.executionID),
			})
			require.NoError(t, err)
			session := testSession()
			session.ProcessedSeq = message.Seq - 1

			outcome, err := NewEngine(stores).Wake(context.Background(), session, Options{})
			require.NoError(t, err)
			require.False(t, outcome.Blocked)
			require.Contains(t, outcome.Summary, "reviewed 1 event(s)")
			require.Empty(t, stores.memoryInputs, "a deterministic review writes no knowledge object")
			require.Contains(t, engineNoteContent(stores.messages), test.wantNote)
		})
	}
}

// A person's question cannot be answered without a model, so the session parks instead
// of silently doing nothing.
func TestWakeBlocksAQuestionWithoutAModel(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	message, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content: "why did the row count drop?", IdempotencyKey: "ask:1",
	})
	require.NoError(t, err)
	session := testSession()
	session.ProcessedSeq = message.Seq - 1

	outcome, err := NewEngine(stores).Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.True(t, outcome.Blocked)
	require.Equal(t, "needs-human", outcome.BlockedCode)
	require.Contains(t, outcome.BlockedReason, "needs the model")
}

// The wake message is idempotent: a retried wake reuses the stored user message instead
// of adding a second one.
func TestWakeReusesTheStoredWakeMessage(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	session := testSession()
	_, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Wake: 1,
		Role:    storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE,
		Content: "retry", IdempotencyKey: "wake:1",
	})
	require.NoError(t, err)
	before := len(stores.messages)

	outcome, err := NewEngine(stores).Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.Zero(t, outcome.ProcessedSeq)
	userMessages := 0
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER {
			userMessages++
		}
	}
	require.Equal(t, 1, userMessages)
	require.Greater(t, len(stores.messages), before)
}

// The watermark counts only external inputs: the assistant/tool/engine rows a wake
// writes must not make the session look due again (the review's hot-loop blocker).
func TestWakeWatermarkCountsOnlyExternalInputs(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	session := testSession()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "nothing to do"}}), resolveToModel, nil)
	outcome, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.Zero(t, outcome.ProcessedSeq)
	require.Greater(t, len(stores.messages), 3, "assistant/engine rows were written")

	// A pending event is consumed by the model wake (it is in the rebuilt context).
	event, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
		Content: "execution 1 failed", Reference: "qualityExecutions/1", IdempotencyKey: "event:execution:1",
	})
	require.NoError(t, err)
	outcome, err = engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.Equal(t, event.Seq, outcome.ProcessedSeq)
}

// The no-model path reviews every pending event it can up to the per-wake bound, not
// just the first; the rest stay above the watermark so the runner picks them up on the
// next wake.
func TestDeterministicWakeReviewsPendingEventsUpToTheBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		events       int
		wantReviewed int
	}{
		{name: "every pending event when under the bound", events: 2, wantReviewed: 2},
		{name: "the bound leaves the rest pending", events: maxDeterministicEvents + 1, wantReviewed: maxDeterministicEvents},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stores := newFakeStore()
			for id := int64(1); id <= int64(test.events); id++ {
				stores.executions[id] = &store.QualityExecutionMessage{
					ID: id, TargetGUID: "target-1", TargetType: storepb.MetaType(1),
					Status: storepb.QualityStatus_QUALITY_STATUS_FAIL,
				}
				_, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
					SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
					Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
					Content: "event", Reference: fmt.Sprintf("qualityExecutions/%d", id),
					IdempotencyKey: fmt.Sprintf("event:%d", id),
				})
				require.NoError(t, err)
			}

			outcome, err := NewEngine(stores).Wake(context.Background(), testSession(), Options{})
			require.NoError(t, err)
			require.Equal(t, test.wantReviewed, strings.Count(engineNoteContent(stores.messages), "deterministic event review"))
			// The events are the session's first rows, so the watermark is the seq of the
			// last reviewed event.
			require.Equal(t, int64(test.wantReviewed), outcome.ProcessedSeq)
			if test.wantReviewed < test.events {
				require.Less(t, outcome.ProcessedSeq, int64(test.events), "the last event is still pending")
			}
		})
	}
}

// A health event names a check, not an execution, and there is no verdict to review
// without a model. The wake must record it and move on: parking the session would
// silence the target exactly when it is reporting that it never concludes.
func TestDeterministicWakeReviewsAHealthEventWithoutParking(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	_, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
		Content: "a check ran and produced no verdict", Reference: "qualityRules/r460",
		IdempotencyKey: "health:NEVER_JUDGED:460:2026-11-26",
	})
	require.NoError(t, err)

	outcome, err := NewEngine(stores).Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.False(t, outcome.Blocked, "a health event must not park the session")
	require.Contains(t, engineNoteContent(stores.messages), "deterministic health review")
	require.Equal(t, int64(1), outcome.ProcessedSeq)
}

// A case is the agent's queue, so the wake tells it what is queued and the session wakes
// when the case is due instead of sleeping until the idle cadence. Without both halves, a
// case opened by the runner would wait a day for a wake that never mentioned it.
func TestWakeRendersOpenCasesAndWakesForThem(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	dueAt := time.Now().UTC().Add(5 * time.Minute)
	stores.openCases = []*store.QualityAgentCaseMessage{{
		ResourceID:   "c1",
		FailureClass: "CONFIG_VALUE_INVALID",
		State:        store.QualityAgentCaseStateDiagnosed,
		Attempts:     1,
		MaxAttempts:  2,
		Summary:      "the target database refused a bound value",
		NextActionAt: ptrTime(dueAt),
	}}

	session := testSession()
	session.AgentID = 1
	outcome, err := NewEngine(stores).Wake(context.Background(), session, Options{})
	require.NoError(t, err)

	var wake string
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER &&
			message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE {
			wake = message.Content
			break
		}
	}
	require.Contains(t, wake, "Open work items")
	require.Contains(t, wake, "CONFIG_VALUE_INVALID")
	require.Contains(t, wake, "DIAGNOSED")
	require.Contains(t, wake, "the target database refused a bound value")

	require.NotNil(t, outcome.NextWakeAt)
	require.WithinDuration(t, dueAt, *outcome.NextWakeAt, time.Minute)
}

// The wake states the authority the target runs under, in the level the cascade resolved.
// The sentence is the prompt's view of the same table the verbs enforce; a model that reads
// it should not have to discover the boundary by being refused.
func TestWakeStatesTheParticipationBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		level store.QualityAgentParticipation
		want  string
	}{
		{store.QualityAgentParticipationObserve, "OBSERVE"},
		{store.QualityAgentParticipationRepair, "REPAIR"},
		{store.QualityAgentParticipationTune, "TUNE"},
	} {
		stores := newFakeStore()
		stores.participation = &store.QualityAgentParticipationLevels{Agent: test.level}
		session := testSession()
		session.AgentID = 1
		_, err := NewEngine(stores).Wake(context.Background(), session, Options{})
		require.NoError(t, err)

		var wake string
		for _, message := range stores.messages {
			if message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE {
				wake = message.Content
				break
			}
		}
		require.Contains(t, wake, "Participation for this target: "+test.want)
	}
}

// The work list is bounded: a target may hold a hundred broken checks, and the prompt must
// not grow with them.
func TestOpenCaseBlockIsBounded(t *testing.T) {
	t.Parallel()

	cases := make([]*store.QualityAgentCaseMessage, 0, maxWakeCases+2)
	for i := 0; i < maxWakeCases+2; i++ {
		cases = append(cases, &store.QualityAgentCaseMessage{
			ResourceID:   fmt.Sprintf("c%d", i),
			FailureClass: "NEVER_JUDGED",
			State:        store.QualityAgentCaseStateOpen,
			Summary:      "a check ran without concluding",
		})
	}
	block := openCaseBlock(cases)
	require.Equal(t, maxWakeCases, strings.Count(block, "qualityAgentCases/"))
	require.Empty(t, openCaseBlock(nil))
}

// A crash between the assistant message and its tool result is repaired before the next
// model request, so the stored transcript stays legal.
func TestWakeRepairsADanglingToolCall(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewEngine(stores)
	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	dangling, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Content:   "calling a tool",
		ToolCalls: &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{{Id: "call_crash", Name: "get_target_summary"}}},
	})
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT, dangling.Role)

	_, err = engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	found := false
	for _, message := range stores.messages {
		if message.ToolCallID == "call_crash" {
			found = true
			require.Contains(t, message.Content, "restarted")
			require.Equal(t, "interrupted", message.Error)
		}
	}
	require.True(t, found, "the dangling call got a synthetic result")
}

// A model wake records what it sent: an engine estimate before the first call, then
// the provider's prompt count after the call. The sample carries the capacities it was
// measured against, so the session detail can render used/window without resolving the
// model again.
func TestModelWakeRecordsTheContextItSent(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "done"}}), resolveToModel, nil)
	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(stores.contextUsage), 2)

	estimate := stores.contextUsage[0]
	require.True(t, estimate.GetEstimated())
	require.Positive(t, estimate.GetUsedTokens())
	require.Equal(t, int32(llm.DefaultContextWindow), estimate.GetContextWindow())
	require.Positive(t, estimate.GetCompactionTriggerTokens())
	require.Equal(t, "test-model", estimate.GetModel())
	require.Equal(t, int32(1), estimate.GetWake())
	require.Equal(t, int32(0), estimate.GetTurn())
	require.NotNil(t, estimate.GetMeasuredAt())

	provider := stores.contextUsage[len(stores.contextUsage)-1]
	require.False(t, provider.GetEstimated())
	require.Equal(t, int32(100), provider.GetUsedTokens(), "the provider's prompt count wins over the estimate")
	require.Equal(t, int32(1), provider.GetTurn())
}

// A provider that reports no usage still leaves a usable sample: the pre-call estimate
// stays the last measurement instead of the meter going blank.
func TestModelWakeKeepsTheEstimateWithoutProviderUsage(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	chat := func(_ context.Context, _ agent.Config) <-chan agent.Event {
		events := make(chan agent.Event, 4)
		go func() {
			defer close(events)
			events <- agent.Event{Type: agent.EventTurnStart, Turn: 1}
			assistant := agent.Message{Role: "assistant", Content: "done"}
			events <- agent.Event{Type: agent.EventAssistant, Assistant: &assistant, Turn: 1}
			events <- agent.Event{Type: agent.EventEnd, Turn: 1}
		}()
		return events
	}
	engine := NewModelEngine(stores, chat, resolveToModel, nil)
	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.Len(t, stores.contextUsage, 1)
	require.True(t, stores.contextUsage[0].GetEstimated())
	require.Positive(t, stores.contextUsage[0].GetUsedTokens())
}

// The deterministic survey never builds a model input, so it must not overwrite the
// last model sample with an empty or fabricated one.
func TestDeterministicWakeLeavesTheContextSampleAlone(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	_, err := NewEngine(stores).Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.Empty(t, stores.contextUsage)
}

// Distinct asset facts can be recorded in one session; an identical statement is
// deduplicated against the confirmed fact already stored for the target.
func TestRecordAssetFactKeepsDistinctFacts(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	turns := []scriptedTurn{
		{content: "first", toolCalls: []llm.ToolCall{toolCall("call_1", "record_asset_fact", `{"statement":"the table has 12 rows"}`)}},
		{content: "second", toolCalls: []llm.ToolCall{toolCall("call_2", "record_asset_fact", `{"statement":"the owner column is nullable"}`)}},
		{content: "third", toolCalls: []llm.ToolCall{toolCall("call_3", "record_asset_fact", `{"statement":"the table has 12 rows"}`)}},
		{content: "done"},
	}
	engine := NewModelEngine(stores, scriptedChat(turns), resolveToModel, nil)
	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.Len(t, stores.memoryInputs, 2, "an identical statement is not recorded twice")
	statements := []string{stores.memoryInputs[0].Statement, stores.memoryInputs[1].Statement}
	require.ElementsMatch(t, []string{"the table has 12 rows", "the owner column is nullable"}, statements)
	require.NotNil(t, stores.memoryInputs[0].Evidence)
	require.Equal(t, "MODEL", stores.memoryInputs[0].Evidence.GetEvidence()[0].GetKind())
}

// A provider context-overflow rejection forces a compaction and retries the wake once
// inside the same wake, instead of parking the session for a retry cycle. The retried
// estimate reflects the compacted conversation, so the session rail does not stay
// critical until the next wake.
func TestWakeCompactsAndRetriesAfterAContextOverflow(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	session := testSession()
	for index := 0; index < 12; index++ {
		_, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
			SessionID: 1, WorkspaceID: "ws",
			Role:    storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
			Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_WAKE,
			Content: strings.Repeat("earlier conversation. ", 200),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	chat := func(_ context.Context, _ agent.Config) <-chan agent.Event {
		calls++
		events := make(chan agent.Event, 8)
		go func() {
			defer close(events)
			switch calls {
			case 1:
				events <- agent.Event{Type: agent.EventError,
					Error: &llm.ResponseError{Code: llm.FailureContextOverflow}}
			case 2:
				events <- agent.Event{Type: agent.EventAssistant,
					Assistant: &agent.Message{Role: "assistant", Content: "## Next Step\n- (none)"}}
				events <- agent.Event{Type: agent.EventEnd}
			default:
				events <- agent.Event{Type: agent.EventTurnStart, Turn: 1}
				events <- agent.Event{Type: agent.EventAssistant,
					Assistant: &agent.Message{Role: "assistant", Content: "recovered"}}
				events <- agent.Event{Type: agent.EventEnd}
			}
		}()
		return events
	}
	engine := NewModelEngine(stores, chat, resolveToModel, nil)
	outcome, err := engine.Wake(context.Background(), session, Options{CompactionTriggerTokens: 1_000_000})
	require.NoError(t, err)
	require.False(t, outcome.Blocked)
	require.Contains(t, outcome.Summary, "wake finished")
	require.NotEmpty(t, session.Summary, "the overflow forced a compaction")
	require.Equal(t, 3, calls, "overflow -> summary -> retried wake")
	found := false
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT && message.Content == "recovered" {
			found = true
		}
	}
	require.True(t, found)
	// The provider reported no usage on the retry, so the compressed estimate must be the
	// last sample: leaving the pre-compaction one would keep the session rail critical.
	require.Len(t, stores.contextUsage, 2, "one estimate before each model call")
	require.True(t, stores.contextUsage[0].GetEstimated())
	require.True(t, stores.contextUsage[1].GetEstimated())
	require.Less(t, stores.contextUsage[1].GetUsedTokens(), stores.contextUsage[0].GetUsedTokens(),
		"the retried estimate reflects the compacted conversation")
}

// denyingChecker refuses every permission and records which half of the scoped
// checker the engine asked.
type denyingChecker struct {
	asked           []permission.Permission
	capabilityCalls int
	resourceCalls   int
	ref             resource.Ref
}

func (c *denyingChecker) CheckPermission(_ context.Context, perm permission.Permission, _ *store.UserMessage) (bool, error) {
	c.capabilityCalls++
	c.asked = append(c.asked, perm)
	return false, nil
}

// CheckResource makes the fake a target-aware checker: a target session asks the
// resource-scoped half, which the engine requires.
func (c *denyingChecker) CheckResource(_ context.Context, perm permission.Permission, _ *store.UserMessage, ref resource.Ref) (bool, error) {
	c.resourceCalls++
	c.ref = ref
	c.asked = append(c.asked, perm)
	return false, nil
}

// A tool call runs under the session's agent principal, and its declared IAM
// permission is checked before the body runs. A target session checks the permission
// on the session target, so the agent's derived database scope cannot be used to reach
// a sibling target; a conversation session has no target and keeps the capability
// check. A refusal is a tool error the wake survives, not a wake failure.
func TestWakeChecksEachToolAgainstTheAgentPrincipal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		tool              string
		conversation      bool
		wantPermission    permission.Permission
		wantResourceCheck bool
	}{
		// The evidence tool declares rules.evidence: a baseline permission such as
		// rules.list is granted to every principal on every target, so it could not
		// scope the read to the agent's databases.
		{name: "target session checks the session target", tool: "list_recent_executions", wantPermission: permission.DataQualityRulesEvidence, wantResourceCheck: true},
		{name: "conversation session keeps the capability check", tool: "get_scope_summary", wantPermission: permission.DataQualityRulesList, conversation: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			session := testSession()
			if test.conversation {
				session.Kind = 2
				session.TargetGUID = ""
			}
			stores := newFakeStore()
			checker := &denyingChecker{}
			engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
				{content: "checking", toolCalls: []llm.ToolCall{toolCall("call_1", test.tool, "{}")}},
				{content: "done"},
			}), resolveToModel, nil, WithPermissionChecker(checker))

			outcome, err := engine.Wake(context.Background(), session, Options{})
			require.NoError(t, err)
			require.False(t, outcome.Blocked)
			require.Equal(t, []permission.Permission{test.wantPermission}, checker.asked)
			if test.wantResourceCheck {
				require.Equal(t, 1, checker.resourceCalls)
				require.Zero(t, checker.capabilityCalls)
				require.Equal(t, resource.NewRef(resource.TypeTable, "target-1"), checker.ref)
			} else {
				require.Equal(t, 1, checker.capabilityCalls)
				require.Zero(t, checker.resourceCalls)
			}

			var toolRow *store.QualityAgentTranscriptMessage
			for _, message := range stores.messages {
				if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL {
					toolRow = message
				}
			}
			require.NotNil(t, toolRow)
			require.Contains(t, toolRow.Content, "does not hold "+string(test.wantPermission))
			require.NotEmpty(t, toolRow.Error)
		})
	}
}

// Every wake records one agent tool metric per executed tool, labelled with the
// product spec and the tool name.
func TestWakeRecordsToolMetrics(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "looking", toolCalls: []llm.ToolCall{toolCall("call_1", "get_target_summary", "{}")}},
		{content: "done"},
	}), resolveToModel, m)

	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)

	families, err := registry.Gather()
	require.NoError(t, err)
	found := false
	for _, family := range families {
		if family.GetName() != "metaxisdata_agent_tool_calls_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			require.Equal(t, "quality-monitor", labels["product"])
			require.Equal(t, "get_target_summary", labels["tool"])
			require.Equal(t, "success", labels["outcome"])
			require.Equal(t, float64(1), metric.GetCounter().GetValue())
			found = true
		}
	}
	require.True(t, found, "the tool call metric was not recorded")
}

// fakeToolAudit collects the tool audit entries a wake reports.
type fakeToolAudit struct {
	entries []tool.AuditEntry
}

func (s *fakeToolAudit) RecordToolCall(_ context.Context, entry tool.AuditEntry) {
	s.entries = append(s.entries, entry)
}

// Side-effecting tool calls run under the agent principal and are reported to the
// ledger; reads are not audited.
func TestWakeAuditsProposeTools(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	sink := &fakeToolAudit{}
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "checking", toolCalls: []llm.ToolCall{
			toolCall("call_1", "get_target_summary", "{}"),
			toolCall("call_2", "record_asset_fact", `{"statement":"the table has 12 rows"}`),
		}},
		{content: "done"},
	}), resolveToModel, nil, WithToolAudit(sink))

	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.Len(t, sink.entries, 1)
	require.Equal(t, "record_asset_fact", sink.entries[0].Tool)
	require.Equal(t, 7, sink.entries[0].Principal.ID)
	require.Nil(t, sink.entries[0].Failure)
}

// A language change rewrites the existing system row instead of appending a
// second system prompt, and the wake carries a short reminder for models that
// mirror long English instructions.
func TestWakeUpdatesTheSystemPromptLanguage(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.agent.Language = "zh-CN"
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "no work"}}), resolveToModel, nil)
	session := testSession()

	_, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	system := stores.messages[0]
	require.Contains(t, system.Content, "Simplified Chinese (zh-CN)")
	require.Contains(t, stores.messages[1].Content, "Owner-facing language: Simplified Chinese (zh-CN)")

	stores.agent.Language = "en-US"
	_, err = engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	systemCount := 0
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM {
			systemCount++
			require.Contains(t, message.Content, "English (en-US)")
			require.NotContains(t, message.Content, "Simplified Chinese (zh-CN)")
		}
	}
	require.Equal(t, 1, systemCount, "the system prompt is updated in place")
	require.Equal(t, 1, stores.systemUpdates, "the stored system row is rewritten")
}

// The adoption hook must receive the wake's start, not the moment the model loop
// ended: eligibility is computed against it, and a draft from an earlier wake must
// not slip in because the pass ran late.
func TestWakePassesTheWakeStartToAdoption(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "done"}}), resolveToModel, nil)
	fixed := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return fixed }
	var got time.Time
	engine.SetAdoption(func(_ context.Context, _ *store.QualityAgentSessionMessage, since time.Time) {
		got = since
	})
	for _, mode := range []storepb.QualityAgentSessionMode{
		storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE,
		storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR,
	} {
		// A monitoring wake drafts only when its check cannot express the intent, and the same
		// guardrails decide whether the replacement is created; withholding the pass would leave
		// that draft waiting for a person on the one path meant to avoid one.
		got = time.Time{}
		session := testSession()
		session.Mode = mode
		_, err := engine.Wake(context.Background(), session, Options{})
		require.NoError(t, err)
		require.Equal(t, fixed, got, "the hook sees the wake start for %s", mode)
	}
}

// agentIDCheckedStore returns the agent only for the requested id, so a test proves the
// engine looks the agent up by the session's AgentID instead of reusing a row.
type agentIDCheckedStore struct {
	*fakeStore
	want int64
}

func (s agentIDCheckedStore) GetQualityAgentByID(ctx context.Context, id int64) (*store.QualityAgentMessage, error) {
	if id != s.want {
		return nil, nil
	}
	return s.fakeStore.GetQualityAgentByID(ctx, id)
}

// The model resolver decides per agent, so a wake must hand it the session's own
// agent, not a workspace-wide default.
func TestWakeResolvesTheSessionsAgentProvider(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.agent = &store.QualityAgentMessage{
		ID:              7,
		State:           store.QualityAgentStateActive,
		ProviderProfile: "llm-provider-profiles/main",
		ProviderModel:   "reasoner",
	}
	session := testSession()
	session.AgentID = 7
	var seen *store.QualityAgentMessage
	engine := NewModelEngine(agentIDCheckedStore{fakeStore: stores, want: 7},
		scriptedChat([]scriptedTurn{{content: "nothing to do"}}),
		func(_ context.Context, agent *store.QualityAgentMessage) (llm.ResolvedConfig, bool, error) {
			seen = agent
			return llm.ResolvedConfig{ModelName: "reasoner"}, true, nil
		}, nil)

	_, err := engine.Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.NotNil(t, seen)
	require.Equal(t, int64(7), seen.ID)
	require.Equal(t, "llm-provider-profiles/main", seen.ProviderProfile)
	require.Equal(t, "reasoner", seen.ProviderModel)
}

// A session whose agent row disappeared still resolves; the resolver reports "no
// model" and the deterministic survey keeps the front office usable.
func TestWakePassesAMissingAgentToTheResolver(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.agent = nil
	called := false
	engine := NewModelEngine(stores, scriptedChat(nil),
		func(_ context.Context, agent *store.QualityAgentMessage) (llm.ResolvedConfig, bool, error) {
			called = true
			require.Nil(t, agent)
			return llm.ResolvedConfig{}, false, nil
		}, nil)

	outcome, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.True(t, called)
	require.False(t, outcome.Blocked)
	require.Contains(t, outcome.Summary, "surveyed")
}
