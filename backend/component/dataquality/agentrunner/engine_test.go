package agentrunner

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeStore is the in-memory slice of the store the engine tests need.
type fakeStore struct {
	messages       []*store.QualityAgentTranscriptMessage
	memories       []*store.QualityMemoryMessage
	executions     map[int64]*store.QualityExecutionMessage
	investigations []*store.CreateQualityInvestigationInput
	memoryInputs   []*store.CreateQualityMemoryInput
	nextSeq        int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{executions: map[int64]*store.QualityExecutionMessage{}}
}

func (*fakeStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return nil, nil
}

func (*fakeStore) GetQualityContract(context.Context, string, storepb.MetaType) (*store.QualityContractMessage, error) {
	return nil, nil
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

func (f *fakeStore) CreateQualityInvestigation(_ context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, bool, error) {
	f.investigations = append(f.investigations, input)
	return &store.QualityInvestigationMessage{ResourceID: input.ResourceID}, true, nil
}

func (f *fakeStore) CreateQualityMemory(_ context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error) {
	f.memoryInputs = append(f.memoryInputs, input)
	memory := &store.QualityMemoryMessage{
		ResourceID: input.ResourceID,
		Subject:    input.Subject,
		Statement:  input.Statement,
		Status:     input.Status,
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

func (f *fakeStore) MaxQualityAgentTranscriptSeq(context.Context, int64) (int64, error) {
	return f.nextSeq, nil
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
	toolCalls []llm.ToolCall
}

// scriptedChat plays a fixed sequence of LLM turns and executes their tool calls
// through the wake's executor, like the real loop does.
func scriptedChat(turns []scriptedTurn) Chat {
	return func(_ context.Context, cfg llm.AgentConfig) <-chan llm.AgentEvent {
		events := make(chan llm.AgentEvent, 32)
		go func() {
			defer close(events)
			for i, turn := range turns {
				number := i + 1
				events <- llm.AgentEvent{Type: llm.AgentEventTurnStart, Turn: number}
				events <- llm.AgentEvent{Type: llm.AgentEventUsage, Turn: number,
					Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}}
				assistant := llm.AgentMessage{Role: "assistant", Content: turn.content, ToolCalls: turn.toolCalls}
				events <- llm.AgentEvent{Type: llm.AgentEventAssistant, Assistant: &assistant, Turn: number}
				for _, call := range turn.toolCalls {
					results, err := cfg.Executor(call)
					content := ""
					if len(results) > 0 {
						content = results[0].Content
					}
					event := llm.AgentEvent{Type: llm.AgentEventToolEnd, ToolCall: &call, ToolResult: content, Turn: number}
					if err != nil {
						event.ToolResult = "error: " + err.Error()
						event.ToolError = err.Error()
					}
					events <- event
				}
				if len(turn.toolCalls) == 0 {
					events <- llm.AgentEvent{Type: llm.AgentEventAgentEnd, Turn: number}
					return
				}
			}
			events <- llm.AgentEvent{Type: llm.AgentEventAgentEnd}
		}()
		return events
	}
}

func resolveToModel(context.Context) (llm.ResolvedConfig, bool, error) {
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
		{content: "let me look", toolCalls: []llm.ToolCall{toolCall("call_1", "get_target_summary", "{}")}},
		{content: "nothing needs attention"},
	}), resolveToModel, nil)

	outcome, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.False(t, outcome.Blocked)
	require.Nil(t, outcome.NextWakeAt)
	require.Equal(t, int64(220), outcome.TokensUsed)
	require.Equal(t, int32(100), outcome.ContextTokens)
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
	require.Equal(t, "survey", outcome.EngineState.GetPhase())
}

// An event with an execution reference is investigated deterministically.
func TestWakeInvestigatesAFailedExecutionWithoutAModel(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.executions[7] = &store.QualityExecutionMessage{
		ID:         7,
		TargetGUID: "target-1",
		TargetType: storepb.MetaType(1),
		Status:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
		FailedRows: 12,
	}
	message, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
		Content: "execution 7 failed", Reference: "qualityExecutions/7",
		IdempotencyKey: "event:execution:7",
	})
	require.NoError(t, err)
	session := testSession()
	session.ProcessedSeq = message.Seq - 1

	outcome, err := NewEngine(stores).Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.False(t, outcome.Blocked)
	require.Contains(t, outcome.Summary, "investigated 1 event(s)")
	require.Len(t, stores.investigations, 1)
	require.Equal(t, storepb.QualityInvestigationTrigger_QUALITY_INVESTIGATION_TRIGGER_EXECUTION,
		stores.investigations[0].Trigger)
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

// An event whose execution ended ERROR is investigated too: an error is as much
// something the agent should look at as a FAIL.
func TestWakeInvestigatesAnErroredExecutionWithoutAModel(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.executions[9] = &store.QualityExecutionMessage{
		ID:         9,
		TargetGUID: "target-1",
		TargetType: storepb.MetaType(1),
		Status:     storepb.QualityStatus_QUALITY_STATUS_ERROR,
		ErrorCode:  "NO_READ_ONLY_SOURCE",
	}
	message, err := stores.AppendQualityAgentTranscriptMessage(context.Background(), &store.QualityAgentTranscriptMessage{
		SessionID: 1, WorkspaceID: "ws", Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:  storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
		Content: "execution 9 errored", Reference: "qualityExecutions/9",
		IdempotencyKey: "event:execution:9",
	})
	require.NoError(t, err)
	session := testSession()
	session.ProcessedSeq = message.Seq - 1

	outcome, err := NewEngine(stores).Wake(context.Background(), session, Options{})
	require.NoError(t, err)
	require.False(t, outcome.Blocked)
	require.Contains(t, outcome.Summary, "investigated 1 event(s)")
	require.Len(t, stores.investigations, 1)
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

// The no-model path investigates every pending event it can, not just the first.
func TestDeterministicWakeInvestigatesEveryPendingEvent(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	for id := int64(1); id <= 2; id++ {
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
	last := stores.messages[len(stores.messages)-1]

	outcome, err := NewEngine(stores).Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	require.Len(t, stores.investigations, 2)
	require.Equal(t, last.Seq, outcome.ProcessedSeq)
}

// More events than one deterministic wake handles stay above the watermark so the
// runner picks them up on the next wake.
func TestDeterministicWakeLeavesTheRestPending(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	for id := int64(1); id <= maxDeterministicEvents+1; id++ {
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
	require.Len(t, stores.investigations, maxDeterministicEvents)
	require.Less(t, outcome.ProcessedSeq, stores.nextSeq, "the last event is still pending")
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

// Several distinct decisions can be recorded in one session; only an identical
// statement is deduplicated.
func TestRecordDecisionKeepsDistinctDecisions(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	turns := []scriptedTurn{
		{content: "first", toolCalls: []llm.ToolCall{toolCall("call_1", "record_decision", `{"statement":"the drop is a real data issue"}`)}},
		{content: "second", toolCalls: []llm.ToolCall{toolCall("call_2", "record_decision", `{"statement":"the check threshold is too strict"}`)}},
		{content: "done"},
	}
	engine := NewModelEngine(stores, scriptedChat(turns), resolveToModel, nil)
	_, err := engine.Wake(context.Background(), testSession(), Options{})
	require.NoError(t, err)
	decisions := 0
	for _, input := range stores.memoryInputs {
		if input.Kind == storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_DECISION {
			decisions++
		}
	}
	require.Equal(t, 2, decisions)
}

// A provider context-overflow rejection forces a compaction and retries the wake once
// inside the same wake, instead of parking the session for a retry cycle.
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
	chat := func(_ context.Context, _ llm.AgentConfig) <-chan llm.AgentEvent {
		calls++
		events := make(chan llm.AgentEvent, 8)
		go func() {
			defer close(events)
			switch calls {
			case 1:
				events <- llm.AgentEvent{Type: llm.AgentEventError,
					Error: &llm.ResponseError{Code: llm.FailureContextOverflow}}
			case 2:
				events <- llm.AgentEvent{Type: llm.AgentEventAssistant,
					Assistant: &llm.AgentMessage{Role: "assistant", Content: "## Next Step\n- (none)"}}
				events <- llm.AgentEvent{Type: llm.AgentEventAgentEnd}
			default:
				events <- llm.AgentEvent{Type: llm.AgentEventTurnStart, Turn: 1}
				events <- llm.AgentEvent{Type: llm.AgentEventUsage, Turn: 1,
					Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}}
				events <- llm.AgentEvent{Type: llm.AgentEventAssistant,
					Assistant: &llm.AgentMessage{Role: "assistant", Content: "recovered"}}
				events <- llm.AgentEvent{Type: llm.AgentEventAgentEnd}
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
}
