package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// collectStream drains parseStream into a slice. The channel is buffered well
// past what each test produces, so parseStream never blocks.
func collectStream(t *testing.T, body string) ([]rawStreamChunk, error) {
	t.Helper()
	ch := make(chan rawStreamChunk, 64)
	err := parseStream(context.Background(), strings.NewReader(body), ch)
	close(ch)

	var chunks []rawStreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	return chunks, err
}

func TestParseStreamEmitsContentAndDone(t *testing.T) {
	t.Parallel()

	chunks, err := collectStream(t, `data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
`)
	require.NoError(t, err)
	require.Len(t, chunks, 3)
	require.Equal(t, "Hello", chunks[0].Content)
	require.Equal(t, " world", chunks[1].Content)
	require.True(t, chunks[2].Done)
}

func TestParseStreamKeepsToolCallsWithNonContiguousIndexes(t *testing.T) {
	t.Parallel()

	// Providers are not required to start at index 0 or keep indexes
	// contiguous; the old 0..len-1 walk dropped these.
	chunks, err := collectStream(t, `data: {"choices":[{"delta":{"tool_calls":[{"index":2,"id":"call_b","type":"function","function":{"name":"second","arguments":"{}"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"first","arguments":"{\"x\":"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}
`)
	require.NoError(t, err)

	var calls []ToolCall
	done := false
	for _, chunk := range chunks {
		if len(chunk.ToolCalls) > 0 {
			calls = chunk.ToolCalls
		}
		if chunk.Done {
			done = true
		}
	}
	require.True(t, done)
	require.Len(t, calls, 2)
	require.Equal(t, "first", calls[0].Function.Name)
	require.Equal(t, `{"x":1}`, calls[0].Function.Arguments)
	require.Equal(t, "second", calls[1].Function.Name)
}

func TestParseStreamAcceptsDoneWithoutFinishReason(t *testing.T) {
	t.Parallel()

	// Some OpenAI-compatible providers end with [DONE] and never set a
	// finish_reason; that is an explicit end, not a dropped stream.
	chunks, err := collectStream(t, `data: {"choices":[{"delta":{"content":"Hi"}}]}
data: [DONE]
`)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, "Hi", chunks[0].Content)
	require.True(t, chunks[1].Done)
}

func TestParseStreamRejectsTruncatedAndIncompleteResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "finish_reason length",
			body: "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n",
			want: "truncated",
		},
		{
			name: "no finish reason",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n",
			want: "without a finish_reason",
		}, {
			name: "malformed chunk",
			body: "data: {not json}\n",
			want: "malformed SSE",
		},
		{
			name: "unexpected finish reason",
			body: "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"content_filter\"}]}\n",
			want: "unexpected finish_reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := collectStream(t, tt.body)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestParseStreamPropagatesReadErrors(t *testing.T) {
	t.Parallel()

	ch := make(chan rawStreamChunk, 8)
	err := parseStream(context.Background(), &maxBytesReader{r: strings.NewReader("0123456789"), remaining: 4}, ch)
	require.ErrorContains(t, err, "LLM response exceeded")
}

func TestSendEventStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// No receiver and an unbuffered channel: the old code blocked here forever.
	require.False(t, sendEvent(ctx, make(chan AgentEvent), AgentEvent{}))
}

func TestMaxBytesReaderFailsInsteadOfTruncating(t *testing.T) {
	t.Parallel()

	r := &maxBytesReader{r: strings.NewReader("0123456789"), remaining: 4}
	buf := make([]byte, 10)

	n, err := r.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Equal(t, "0123", string(buf[:n]))

	_, err = r.Read(buf)
	require.ErrorIs(t, err, errResponseTooLarge)
}

func TestBoundedBufferCapsDebugCopyOnly(t *testing.T) {
	t.Parallel()

	b := &boundedBuffer{limit: 4}
	n, err := b.Write([]byte("0123456789"))
	require.NoError(t, err)
	require.Equal(t, 10, n, "a short return value would surface as an io error to TeeReader")
	require.Equal(t, "0123", b.String())
}

func TestIdleTimeoutReaderCancelsAStalledStream(t *testing.T) {
	t.Parallel()

	cancelled := make(chan struct{})
	ir := newIdleTimeoutReader(strings.NewReader(""), 10*time.Millisecond, func() { close(cancelled) })
	defer ir.stop()

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("idle timeout did not cancel the request")
	}
	require.True(t, ir.timedOut())
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

// collectAgentEvents drains the agent loop.
func collectAgentEvents(t *testing.T, events <-chan AgentEvent) []AgentEvent {
	t.Helper()
	var collected []AgentEvent
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
func TestRunAgentLoopEnforcesMaxTurns(t *testing.T) {
	t.Parallel()

	server := toolCallingProvider(t)
	cfg := AgentConfig{
		Provider:    ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:    3,
		Tools:       []ToolDef{{Type: "function"}},
		Executor:    func(ToolCall) ([]ToolResult, error) { return []ToolResult{{Content: "result"}}, nil },
		DebugLogger: nil,
	}

	events := collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	turns := 0
	var lastErr error
	for _, evt := range events {
		switch evt.Type {
		case AgentEventTurnStart:
			turns++
		case AgentEventError:
			lastErr = evt.Error
		default:
		}
	}
	require.Equal(t, 3, turns)
	require.ErrorContains(t, lastErr, "maximum of 3 turns")
}

// A tool result larger than the conversation budget stops the loop with a clear
// error instead of growing until the process runs out of memory.
func TestRunAgentLoopEnforcesConversationBudget(t *testing.T) {
	t.Parallel()

	server := toolCallingProvider(t)
	cfg := AgentConfig{
		Provider:             ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:             5,
		MaxConversationBytes: 64,
		Tools:                []ToolDef{{Type: "function"}},
		Executor: func(ToolCall) ([]ToolResult, error) {
			return []ToolResult{{Content: strings.Repeat("x", 1024)}}, nil
		},
	}

	events := collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	var lastErr error
	for _, evt := range events {
		if evt.Type == AgentEventError {
			lastErr = evt.Error
		}
	}
	require.ErrorContains(t, lastErr, "LLM conversation exceeded 64 bytes")
}
