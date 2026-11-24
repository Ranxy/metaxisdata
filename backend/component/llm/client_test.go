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

// optionProvider answers one plain completion and records the request.
func optionProvider(t *testing.T, capture *capturedRequest) *httptest.Server {
	t.Helper()
	const sseBody = "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody)
	}))
	t.Cleanup(server.Close)
	return server
}

// requestOptionsConfig is the config whose new fields the request-body tests pin.
func requestOptionsConfig(t *testing.T, baseURL string) StreamRequest {
	t.Helper()
	return StreamRequest{
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
	server := optionProvider(t, capture)

	cfg := requestOptionsConfig(t, server.URL)
	drainStream(Stream(context.Background(), streamRequestForTest(cfg, []Message{{Role: "user", Content: "hi"}})))

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

// The output ceiling belongs to the model unless a caller sets a smaller cap, in
// which case the caller's cap wins.
func TestStreamRawResolvesTheOutputBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maxTokens int
		want      int
	}{
		{name: "the model budget when the caller sets no cap", want: 4096},
		{name: "the caller cap when it is smaller", maxTokens: 512, want: 512},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			capture := &capturedRequest{}
			server := optionProvider(t, capture)

			cfg := StreamRequest{
				Provider:  ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key", MaxOutputTokens: 4096},
				MaxTokens: test.maxTokens,
			}
			drainStream(Stream(context.Background(), streamRequestForTest(cfg, []Message{{Role: "user", Content: "hi"}})))

			body, _ := capture.snapshot(t)
			var request map[string]any
			require.NoError(t, json.Unmarshal(body, &request))
			require.InDelta(t, test.want, request["max_tokens"], 1e-9)
		})
	}
}

// A caller that sets nothing sends exactly the request it sent before the
// options existed: the fields are omitted, not zeroed.
func TestStreamRawOmitsUnsetRequestOptions(t *testing.T) {
	t.Parallel()

	capture := &capturedRequest{}
	server := optionProvider(t, capture)

	cfg := StreamRequest{Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"}}
	drainStream(Stream(context.Background(), streamRequestForTest(cfg, []Message{{Role: "user", Content: "hi"}})))

	body, _ := capture.snapshot(t)
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	for _, absent := range []string{"temperature", "max_tokens", "response_format", "stream_options", "tools"} {
		require.NotContains(t, request, absent)
	}
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

// The usage total is derived when a provider reports only the components, so a
// caller that records the total still gets a number.
func TestUsageAddDerivesAMissingTotal(t *testing.T) {
	t.Parallel()

	var total Usage
	total.Add(&Usage{PromptTokens: 4, CompletionTokens: 6})
	require.Equal(t, int64(10), total.TotalTokens)
	total.Add(&Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 5})
	require.Equal(t, int64(15), total.TotalTokens)

	// A nil side is a no-op, never a panic.
	total.Add(nil)
	require.Equal(t, int64(5), total.PromptTokens, "a nil side leaves the running total alone")
	var nilUsage *Usage
	require.NotPanics(t, func() { nilUsage.Add(&Usage{PromptTokens: 1}) })
	require.Nil(t, nilUsage)
}

// drainStream consumes a stream so its request completes; the assertions in the
// callers are about the recorded request, not the events.
func drainStream[T any](stream <-chan T) {
	for event := range stream {
		_ = event
	}
}

// A base URL written the way the settings form suggests (.../v1) must still
// reach the chat completions path without duplicating /v1.
func TestStreamAcceptsAnSDKStyleBaseURL(t *testing.T) {
	t.Parallel()

	capture := &capturedRequest{}
	server := optionProvider(t, capture)

	cfg := requestOptionsConfig(t, server.URL+"/v1")
	drainStream(Stream(context.Background(), streamRequestForTest(cfg, []Message{{Role: "user", Content: "hi"}})))

	_, path := capture.snapshot(t)
	require.Equal(t, "/v1/chat/completions", path)
}
