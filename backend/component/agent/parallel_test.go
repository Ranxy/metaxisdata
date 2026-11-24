package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

// batchProvider answers the first request with the given tool calls and every
// later request with a plain final answer.
func batchProvider(t *testing.T, calls []llm.ToolCall) *httptest.Server {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) > 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n")
			_, _ = io.WriteString(w, "data: [DONE]\n")
			return
		}
		for index, call := range calls {
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%d,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":\"{}\"}}]}}]}\n",
				index, call.ID, call.Function.Name)
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// call names one tool call.
func call(id, name string) llm.ToolCall {
	var result llm.ToolCall
	result.ID = id
	result.Type = "function"
	result.Function.Name = name
	return result
}

func TestParallelSafeToolCallsOverlap(t *testing.T) {
	t.Parallel()

	server := batchProvider(t, []llm.ToolCall{call("call_1", "read"), call("call_2", "read")})
	var running atomic.Int32
	bothStarted := make(chan struct{})
	var once sync.Once
	cfg := Config{
		Provider:             llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:             3,
		MaxParallelToolCalls: 2,
		ParallelSafe:         func(name string) bool { return name == "read" },
		Executor: func(llm.ToolCall) ([]llm.ToolResult, error) {
			if running.Add(1) == 2 {
				once.Do(func() { close(bothStarted) })
			}
			select {
			case <-bothStarted:
				return []llm.ToolResult{{Content: "read done"}}, nil
			case <-time.After(2 * time.Second):
				return nil, errors.New("the calls did not overlap")
			}
		},
	}
	events := collectEvents(t, RunLoop(context.Background(), cfg))

	turns := 0
	for _, event := range events {
		if event.Type == EventTurnStart {
			turns++
		}
	}
	require.Equal(t, 2, turns)
	require.Equal(t, int32(2), running.Load())
}

// An exclusive call is a barrier: the model's calls are chunked so a read never
// shares a batch with a write. The event stream shows the chunking because a
// batch's starts are emitted before its calls run and its ends after.
func TestExclusiveToolCallsAreBarriers(t *testing.T) {
	t.Parallel()

	server := batchProvider(t, []llm.ToolCall{
		call("call_1", "read"), call("call_2", "write"), call("call_3", "read"),
	})
	cfg := Config{
		Provider:             llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:             3,
		MaxParallelToolCalls: 4,
		ParallelSafe:         func(name string) bool { return name == "read" },
		Executor:             func(llm.ToolCall) ([]llm.ToolResult, error) { return []llm.ToolResult{{Content: "ok"}}, nil },
	}
	events := collectEvents(t, RunLoop(context.Background(), cfg))

	var order []string
	for _, event := range events {
		if event.Type == EventToolStart {
			order = append(order, "start:"+event.ToolCall.ID)
		}
		if event.Type == EventToolEnd {
			order = append(order, "end:"+event.ToolCall.ID)
		}
	}
	require.Equal(t, []string{
		"start:call_1", "end:call_1",
		"start:call_2", "end:call_2",
		"start:call_3", "end:call_3",
	}, order, "an exclusive call is a barrier: reads never share its batch")
}

func TestMaxParallelToolCallsBoundsConcurrency(t *testing.T) {
	t.Parallel()

	server := batchProvider(t, []llm.ToolCall{
		call("call_1", "read"), call("call_2", "read"), call("call_3", "read"),
	})
	var running, peak atomic.Int32
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	// The first batch fills every slot and blocks there; the gate opens only once
	// both slots are occupied, so the overlap is the configured bound rather than a
	// scheduling accident.
	go func() {
		for i := 0; i < 2; i++ {
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				close(release)
				return
			}
		}
		close(release)
	}()
	cfg := Config{
		Provider:             llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns:             3,
		MaxParallelToolCalls: 2,
		ParallelSafe:         func(string) bool { return true },
		Executor: func(llm.ToolCall) ([]llm.ToolResult, error) {
			current := running.Add(1)
			for {
				seen := peak.Load()
				if current <= seen || peak.CompareAndSwap(seen, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			running.Add(-1)
			return []llm.ToolResult{{Content: "ok"}}, nil
		},
	}
	collectEvents(t, RunLoop(context.Background(), cfg))
	require.LessOrEqual(t, peak.Load(), int32(2), "concurrency must respect MaxParallelToolCalls")
	require.Equal(t, int32(2), peak.Load(), "the parallel-safe calls should have overlapped")
}

// A structured tool failure reaches the model with its hint and candidates, not
// just the message.
func TestStructuredToolFailureRendersItsEnvelope(t *testing.T) {
	t.Parallel()

	server := batchProvider(t, []llm.ToolCall{call("call_1", "lookup")})
	cfg := Config{
		Provider: llm.ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key"},
		MaxTurns: 3,
		Executor: func(llm.ToolCall) ([]llm.ToolResult, error) {
			return nil, &tool.Error{
				Code:       tool.CodeNotFound,
				Message:    `no table matches "orders"`,
				Hint:       "search first",
				Candidates: []string{"orders_a"},
			}
		},
	}
	events := collectEvents(t, RunLoop(context.Background(), cfg))

	var content string
	for _, event := range events {
		if event.Type == EventToolEnd {
			content = event.ToolResult
		}
	}
	require.Contains(t, content, `no table matches "orders"`)
	require.Contains(t, content, "hint: search first")
	require.Contains(t, content, "candidates: orders_a")
}
