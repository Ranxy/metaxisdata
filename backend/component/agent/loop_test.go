package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

func TestSendEventStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// No receiver and an unbuffered channel: the old code blocked here forever.
	require.False(t, sendEvent(ctx, make(chan Event), Event{}))
}

// The conversation the loop keeps is bounded: every turn resends it, so an
// oversized tool result must fail the explanation instead of growing memory and
// request size without limit.
func TestConversationBudget(t *testing.T) {
	t.Parallel()

	budget := &conversationBudget{limit: 10}
	require.True(t, budget.add(4))
	require.True(t, budget.add(6))
	require.False(t, budget.add(1), "the conversation may not exceed its limit")
	require.Equal(t, 10, budget.used, "a rejected addition is not recorded")

	// A non-positive limit means unlimited, which is what an unset config falls
	// back to only through DefaultMaxConversationBytes.
	unlimited := &conversationBudget{}
	require.True(t, unlimited.add(1<<30))
}

// collectEvents drains the agent loop.

func collectEvents(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var collected []Event
	for evt := range events {
		collected = append(collected, evt)
	}
	return collected
}

// toolCallingProvider always answers with the same tool call, so the loop only
// stops when a limit stops it.

func toolCallingProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// MaxTurns has to be the loop's real bound: a provider that always asks for a
// tool must not keep the agent running forever.
func TestRunLoopEnforcesMaxTurns(t *testing.T) {
	t.Parallel()

	server := toolCallingProvider(t)
	cfg := Config{
		Provider:    llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:    3,
		Tools:       []llm.ToolDef{{Type: "function"}},
		Executor:    func(llm.ToolCall) ([]llm.ToolResult, error) { return []llm.ToolResult{{Content: "result"}}, nil },
		DebugLogger: nil,
	}

	events := collectEvents(t, RunLoop(context.Background(), cfg))

	turns := 0
	var lastErr error
	for _, evt := range events {
		switch evt.Type {
		case EventTurnStart:
			turns++
		case EventError:
			lastErr = evt.Error
		default:
		}
	}
	require.Equal(t, 3, turns)
	require.ErrorContains(t, lastErr, "maximum of 3 turns")
}

// A tool result larger than the conversation budget stops the loop with a clear
// error instead of growing until the process runs out of memory.
func TestRunLoopEnforcesConversationBudget(t *testing.T) {
	t.Parallel()

	server := toolCallingProvider(t)
	cfg := Config{
		Provider:             llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:             5,
		MaxConversationBytes: 64,
		Tools:                []llm.ToolDef{{Type: "function"}},
		Executor: func(llm.ToolCall) ([]llm.ToolResult, error) {
			return []llm.ToolResult{{Content: strings.Repeat("x", 1024)}}, nil
		},
	}

	events := collectEvents(t, RunLoop(context.Background(), cfg))

	var lastErr error
	for _, evt := range events {
		if evt.Type == EventError {
			lastErr = evt.Error
		}
	}
	require.ErrorContains(t, lastErr, "LLM conversation exceeded 64 bytes")
}

// A session wake continues the stored conversation: the caller's History must be
// the exact request prefix, and the finished assistant message is reported as one
// event so a recorder can persist it without replaying deltas.
func TestRunLoopContinuesTheHistory(t *testing.T) {
	t.Parallel()

	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)

	history := []Message{
		{Role: "system", Content: "session system prompt"},
		{Role: "user", Content: "earlier wake"},
		{Role: "assistant", Content: "previous answer"},
		{Role: "toolResult", ToolCallID: "call_1", ToolName: "lookup", Content: "previous tool result"},
		{Role: "user", Content: "new event"},
	}
	events := collectEvents(t, RunLoop(context.Background(), Config{
		Provider:    llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		History:     history,
		MaxTurns:    1,
		Executor:    func(llm.ToolCall) ([]llm.ToolResult, error) { return nil, nil },
		Tools:       nil,
		MaxTokens:   128,
		Metrics:     nil,
		DebugLogger: nil,
	}))

	var assistant *Message
	var loopErr error
	for _, evt := range events {
		switch evt.Type {
		case EventAssistant:
			assistant = evt.Assistant
		case EventError:
			loopErr = evt.Error
		default:
		}
	}
	require.NoError(t, loopErr)
	require.NotNil(t, assistant)
	require.Equal(t, "done", assistant.Content)

	request := <-received
	require.Contains(t, request, "session system prompt")
	require.Contains(t, request, "earlier wake")
	require.Contains(t, request, "previous answer")
	require.Contains(t, request, "previous tool result")
	require.Contains(t, request, "new event")
}

// The one-shot callers keep working: without History the two prompt fields seed the
// conversation exactly as before.
func TestRunLoopSeedsFromThePromptFieldsWithoutHistory(t *testing.T) {
	t.Parallel()

	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)

	events := collectEvents(t, RunLoop(context.Background(), Config{
		Provider:     llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		SystemPrompt: "single-shot system",
		UserPrompt:   "single-shot user",
		MaxTurns:     1,
	}))
	for _, evt := range events {
		require.NotEqual(t, EventError, evt.Type)
	}
	request := <-received
	require.Contains(t, request, "single-shot system")
	require.Contains(t, request, "single-shot user")
}

// Tool-call arguments travel in the conversation on every turn, so an oversized
// argument set must be held to the same conversation budget as a text message.
func TestRunLoopEnforcesConversationBudgetOnToolArguments(t *testing.T) {
	t.Parallel()

	var call llm.ToolCall
	call.ID = "call_1"
	call.Type = "function"
	call.Function.Name = "lookup"
	call.Function.Arguments = strings.Repeat("a", 1024)
	cfg := Config{
		Provider:             llm.ResolvedConfig{ModelName: "test-model"},
		History:              []Message{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}},
		MaxConversationBytes: 100,
	}
	events := collectEvents(t, RunLoop(context.Background(), cfg))
	require.Len(t, events, 1)
	require.Equal(t, EventError, events[0].Type)
	require.Contains(t, events[0].Error.Error(), "exceeded")
}

// capturedRequest records the last request body a fake provider received, so a
// test can assert what the loop actually sent.
type capturedRequest struct {
	mu   sync.Mutex
	body []byte
	path string
}

func (c *capturedRequest) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.body = body
	c.path = r.URL.Path
}

func (c *capturedRequest) snapshot(t *testing.T) ([]byte, string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.body, "the provider received no request body")
	return c.body, c.path
}

// optionProvider answers with one fixed SSE body and records the request.
func optionProvider(t *testing.T, sseBody string, capture *capturedRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody)
	}))
	t.Cleanup(server.Close)
	return server
}

// requestOptionsConfig is the config whose optional fields the loop forwards.
func requestOptionsConfig(t *testing.T, baseURL string) Config {
	t.Helper()
	return Config{
		Provider:       llm.ResolvedConfig{BaseURL: baseURL, ModelName: "test-model", APIKey: "key"},
		Temperature:    proto.Float64(0.2),
		MaxTokens:      512,
		ResponseFormat: llm.JSONObjectResponseFormat(),
		IncludeUsage:   true,
	}
}

// Config is the only seam the agent loop has: what it carries is what the
// provider receives, on every turn.
func TestConfigForwardsRequestOptions(t *testing.T) {
	t.Parallel()

	capture := &capturedRequest{}
	server := optionProvider(t, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n", capture)

	cfg := requestOptionsConfig(t, server.URL)
	events := collectEvents(t, RunLoop(context.Background(), cfg))
	require.NotEmpty(t, events)

	body, _ := capture.snapshot(t)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	require.Contains(t, request, "temperature")
	require.Contains(t, request, "max_tokens")
	require.Contains(t, request, "response_format")
	require.Contains(t, request, "stream_options")
}

// The agent loop reports one usage event per turn and the run's total on the end
// event, so a caller can record cost without guessing.
func TestRunLoopReportsUsage(t *testing.T) {
	t.Parallel()

	server := optionProvider(t, `data: {"choices":[{"delta":{"content":"ok"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}
data: [DONE]
`, &capturedRequest{})

	cfg := Config{Provider: llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"}, IncludeUsage: true}
	events := collectEvents(t, RunLoop(context.Background(), cfg))

	var usageEvents []Event
	var end *Event
	for index := range events {
		evt := events[index]
		if evt.Type == EventUsage {
			usageEvents = append(usageEvents, evt)
		}
		if evt.Type == EventEnd {
			end = &events[index]
		}
	}
	require.Len(t, usageEvents, 1)
	require.Equal(t, 1, usageEvents[0].Turn)
	require.NotNil(t, usageEvents[0].Usage)
	require.Equal(t, int64(12), usageEvents[0].Usage.TotalTokens)
	require.NotNil(t, end)
	require.NotNil(t, end.Usage)
	require.Equal(t, int64(9), end.Usage.PromptTokens)
	require.Equal(t, int64(3), end.Usage.CompletionTokens)
	require.Equal(t, int64(12), end.Usage.TotalTokens)
}

// usageAcrossTurnsProvider reports a tool call with usage on the first call and a
// final answer with usage on the second.

func usageAcrossTurnsProvider(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n")
			_, _ = io.WriteString(w, "data: [DONE]\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":5,\"total_tokens\":25}}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRunLoopAggregatesUsageAcrossTurns(t *testing.T) {
	t.Parallel()

	server := usageAcrossTurnsProvider(t)
	cfg := Config{
		Provider:     llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:     3,
		Tools:        []llm.ToolDef{{Type: "function"}},
		Executor:     func(llm.ToolCall) ([]llm.ToolResult, error) { return []llm.ToolResult{{Content: "result"}}, nil },
		IncludeUsage: true,
	}
	events := collectEvents(t, RunLoop(context.Background(), cfg))

	usageEvents := 0
	var end *Event
	for index := range events {
		evt := events[index]
		if evt.Type == EventUsage {
			usageEvents++
		}
		if evt.Type == EventEnd {
			end = &events[index]
		}
	}
	require.Equal(t, 2, usageEvents, "one usage event per turn")
	require.NotNil(t, end)
	require.NotNil(t, end.Usage)
	require.Equal(t, int64(30), end.Usage.PromptTokens)
	require.Equal(t, int64(7), end.Usage.CompletionTokens)
	require.Equal(t, int64(37), end.Usage.TotalTokens)
}

// A provider that ignores stream=true still completes through the agent loop.
func TestRunLoopAcceptsANonStreamingProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"plain answer"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	cfg := Config{Provider: llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"}}
	events := collectEvents(t, RunLoop(context.Background(), cfg))

	var content string
	var ended bool
	for _, evt := range events {
		if evt.Type == EventContent {
			content += evt.Content
		}
		if evt.Type == EventEnd {
			ended = true
		}
		require.NotEqual(t, EventError, evt.Type, "no error: %v", evt.Error)
	}
	require.Equal(t, "plain answer", content)
	require.True(t, ended)
}
