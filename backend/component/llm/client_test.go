package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// capturedRequest records the last request body a fake provider received, so a
// test can assert what the client actually sent instead of what it meant to send.
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

// requestOptionsConfig is the config whose new fields the request-body tests pin.
func requestOptionsConfig(t *testing.T, baseURL string) AgentConfig {
	t.Helper()
	return AgentConfig{
		Provider:       ResolvedConfig{BaseURL: baseURL, ModelName: "test-model", APIKey: "key"},
		Temperature:    proto.Float64(0.2),
		MaxTokens:      512,
		ResponseFormat: JSONObjectResponseFormat(),
		IncludeUsage:   true,
	}
}

// The new request fields reach the provider with the OpenAI wire names, and the
// usage opt-in is a request field too.
func TestStreamRawSendsRequestOptions(t *testing.T) {
	t.Parallel()

	capture := &capturedRequest{}
	server := optionProvider(t, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n", capture)

	cfg := requestOptionsConfig(t, server.URL)
	drainStream(streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))

	body, path := capture.snapshot(t)
	require.Equal(t, "/v1/chat/completions", path)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	require.Equal(t, "test-model", request["model"])
	require.Equal(t, true, request["stream"])
	require.InDelta(t, 0.2, request["temperature"], 1e-9)
	require.InDelta(t, 512, request["max_tokens"], 1e-9)
	responseFormat, ok := request["response_format"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, ResponseFormatJSONObject, responseFormat["type"])
	streamOptions, ok := request["stream_options"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, streamOptions["include_usage"])
}

// A caller that sets nothing sends exactly the request it sent before the
// options existed: the fields are omitted, not zeroed.
func TestStreamRawOmitsUnsetRequestOptions(t *testing.T) {
	t.Parallel()

	capture := &capturedRequest{}
	server := optionProvider(t, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n", capture)

	cfg := AgentConfig{Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"}}
	drainStream(streamRaw(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}))

	body, _ := capture.snapshot(t)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	for _, absent := range []string{"temperature", "max_tokens", "response_format", "stream_options", "tools"} {
		require.NotContains(t, request, absent)
	}
}

// AgentConfig is the only seam the agent loop has: what it carries is what the
// provider receives, on every turn.
func TestAgentConfigForwardsRequestOptions(t *testing.T) {
	t.Parallel()

	capture := &capturedRequest{}
	server := optionProvider(t, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n", capture)

	cfg := requestOptionsConfig(t, server.URL)
	events := collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))
	require.NotEmpty(t, events)

	body, _ := capture.snapshot(t)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	require.Contains(t, request, "temperature")
	require.Contains(t, request, "max_tokens")
	require.Contains(t, request, "response_format")
	require.Contains(t, request, "stream_options")
}

// Usage arrives in a chunk of its own after the choice that carries the finish
// reason, so the parser must keep reading past finish_reason.
func TestParseStreamParsesUsage(t *testing.T) {
	t.Parallel()

	chunks, err := collectStream(t, `data: {"choices":[{"delta":{"content":"Hi"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}
data: [DONE]
`)
	require.NoError(t, err)

	var usage *Usage
	done := false
	for _, chunk := range chunks {
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Done {
			done = true
		}
	}
	require.True(t, done)
	require.NotNil(t, usage)
	require.Equal(t, int64(11), usage.PromptTokens)
	require.Equal(t, int64(7), usage.CompletionTokens)
	require.Equal(t, int64(18), usage.TotalTokens)
}

// A provider that reports no usage produces no usage chunk at all: absent is not
// zero.
func TestParseStreamWithoutUsage(t *testing.T) {
	t.Parallel()

	chunks, err := collectStream(t, `data: {"choices":[{"delta":{"content":"Hi"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)
	require.NoError(t, err)
	for _, chunk := range chunks {
		require.Nil(t, chunk.Usage)
	}
	require.True(t, chunks[len(chunks)-1].Done)
}

// A gateway that ignores stream=true answers with one completion object. It used
// to be reported as a dropped stream.
func TestParseStreamParsesNonStreamingCompletion(t *testing.T) {
	t.Parallel()

	chunks, err := collectStream(t, `{"choices":[{"message":{"content":"one answer"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	require.NoError(t, err)
	require.Len(t, chunks, 3)
	require.Equal(t, "one answer", chunks[0].Content)
	require.NotNil(t, chunks[1].Usage)
	require.Equal(t, int64(7), chunks[1].Usage.TotalTokens)
	require.True(t, chunks[2].Done)
}

// A non-streaming response carries complete tool calls, so one chunk is enough.
func TestParseStreamParsesNonStreamingToolCalls(t *testing.T) {
	t.Parallel()

	chunks, err := collectStream(t, `{"choices":[{"message":{"content":"","tool_calls":[
		{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]},
		"finish_reason":"tool_calls"}]}`)
	require.NoError(t, err)

	var calls []ToolCall
	for _, chunk := range chunks {
		if len(chunk.ToolCalls) > 0 {
			calls = chunk.ToolCalls
		}
	}
	require.Len(t, calls, 1)
	require.Equal(t, "lookup", calls[0].Function.Name)
	require.Equal(t, `{"x":1}`, calls[0].Function.Arguments)
	require.True(t, chunks[len(chunks)-1].Done)
}

// A non-streaming body that is not a completion (an error object) is an error,
// not an empty answer.
func TestParseStreamRejectsANonStreamingErrorBody(t *testing.T) {
	t.Parallel()

	_, err := collectStream(t, `{"error":{"message":"rate limited"}}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no choices")
}

// The agent loop reports one usage event per turn and the run's total on the end
// event, so a caller can record cost without guessing.
func TestRunAgentLoopReportsUsage(t *testing.T) {
	t.Parallel()

	server := optionProvider(t, `data: {"choices":[{"delta":{"content":"ok"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}
data: [DONE]
`, &capturedRequest{})

	cfg := AgentConfig{Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"}, IncludeUsage: true}
	events := collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	var usageEvents []AgentEvent
	var end *AgentEvent
	for index := range events {
		evt := events[index]
		if evt.Type == AgentEventUsage {
			usageEvents = append(usageEvents, evt)
		}
		if evt.Type == AgentEventAgentEnd {
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

func TestRunAgentLoopAggregatesUsageAcrossTurns(t *testing.T) {
	t.Parallel()

	server := usageAcrossTurnsProvider(t)
	cfg := AgentConfig{
		Provider:     ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:     3,
		Tools:        []ToolDef{{Type: "function"}},
		Executor:     func(ToolCall) ([]ToolResult, error) { return []ToolResult{{Content: "result"}}, nil },
		IncludeUsage: true,
	}
	events := collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	usageEvents := 0
	var end *AgentEvent
	for index := range events {
		evt := events[index]
		if evt.Type == AgentEventUsage {
			usageEvents++
		}
		if evt.Type == AgentEventAgentEnd {
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
func TestRunAgentLoopAcceptsANonStreamingProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"plain answer"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	cfg := AgentConfig{Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"}}
	events := collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	var content string
	var ended bool
	for _, evt := range events {
		if evt.Type == AgentEventContent {
			content += evt.Content
		}
		if evt.Type == AgentEventAgentEnd {
			ended = true
		}
		require.NotEqual(t, AgentEventError, evt.Type, "no error: %v", evt.Error)
	}
	require.Equal(t, "plain answer", content)
	require.True(t, ended)
}

// The usage total is derived when a provider reports only the components, so a
// caller that records the total still gets a number.
func TestUsageAddDerivesAMissingTotal(t *testing.T) {
	t.Parallel()

	var total Usage
	total.add(&Usage{PromptTokens: 4, CompletionTokens: 6})
	require.Equal(t, int64(10), total.TotalTokens)
	total.add(&Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 5})
	require.Equal(t, int64(15), total.TotalTokens)

	// A nil side is a no-op, never a panic.
	total.add(nil)
	require.Equal(t, int64(5), total.PromptTokens, "a nil side leaves the running total alone")
	var nilUsage *Usage
	require.NotPanics(t, func() { nilUsage.add(&Usage{PromptTokens: 1}) })
	require.Nil(t, nilUsage)
}

// drainStream consumes a stream so its request completes; the assertions in the
// callers are about the recorded request, not the events.
func drainStream[T any](stream <-chan T) {
	for event := range stream {
		_ = event
	}
}
