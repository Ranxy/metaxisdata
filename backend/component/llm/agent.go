package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

const (
	// llmResponseHeaderTimeout bounds the wait for the response headers.
	llmResponseHeaderTimeout = 30 * time.Second

	// llmIdleReadTimeout bounds the gap between two reads once the response has
	// started. There is deliberately no total timeout: a long answer must not
	// be cut off mid-stream, only a stalled one.
	llmIdleReadTimeout = 60 * time.Second

	// llmMaxResponseBytes caps one streaming response. Exceeding it is an error,
	// never a silent truncation.
	llmMaxResponseBytes = 32 << 20

	// llmMaxSSELineBytes allows large tool-call argument deltas.
	llmMaxSSELineBytes = 8 << 20

	// llmDebugBodyLimit caps only the copy of the response kept for the debug
	// log; the stream itself is bounded by llmMaxResponseBytes.
	llmDebugBodyLimit = 1 << 20

	// DefaultMaxTurns is how many LLM calls one agent loop may make before it
	// gives up. Reaching it is an error, never a silent partial answer.
	DefaultMaxTurns = 6

	// DefaultMaxConversationBytes caps the conversation the loop keeps in memory
	// and resends on every turn. A single tool result can be arbitrarily large,
	// so without a cap the context grows by that much per turn.
	DefaultMaxConversationBytes = 4 << 20
)

// conversationBudget tracks the size of the conversation in memory.
type conversationBudget struct {
	limit int
	used  int
}

// add records size more bytes and reports whether they fit. Nothing is recorded
// for an addition that does not fit, so the overflow is reported once.
func (b *conversationBudget) add(size int) bool {
	if b.limit > 0 && b.used+size > b.limit {
		return false
	}
	b.used += size
	return true
}

// errResponseTooLarge is returned when a single response exceeds the cap.
var errResponseTooLarge = errors.New("LLM response exceeded the size limit")

// llmHTTPClient is shared across requests so connections are pooled, instead of
// building a fresh client per call on top of http.DefaultTransport.
var llmHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: llmResponseHeaderTimeout,
	},
}

// ---- Agent Loop ----

// RunAgentLoop runs the agent loop: LLM ↔ tool calls until completion.
// It emits events to the channel and closes it when done.
func RunAgentLoop(ctx context.Context, cfg AgentConfig) <-chan AgentEvent {
	ch := make(chan AgentEvent, 32)

	go func() {
		defer close(ch)
		run(ctx, cfg, ch)
	}()

	return ch
}

// sendEvent delivers evt, or gives up when ctx is cancelled. It reports false
// when the loop must stop, which happens when the consumer went away (it
// stopped ranging and the buffer filled) or the request was cancelled. Without
// the ctx branch a producer blocked on a full channel leaks forever.
func sendEvent(ctx context.Context, ch chan<- AgentEvent, evt AgentEvent) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendRaw(ctx context.Context, ch chan<- rawStreamChunk, chunk rawStreamChunk) bool {
	select {
	case ch <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

func run(ctx context.Context, cfg AgentConfig, ch chan<- AgentEvent) {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	maxConversationBytes := cfg.MaxConversationBytes
	if maxConversationBytes <= 0 {
		maxConversationBytes = DefaultMaxConversationBytes
	}
	budget := &conversationBudget{limit: maxConversationBytes}
	budget.add(len(cfg.SystemPrompt))
	budget.add(len(cfg.UserPrompt))

	messages := []AgentMessage{
		{Role: "system", Content: cfg.SystemPrompt},
		{Role: "user", Content: cfg.UserPrompt},
	}

	// appendMessage keeps the conversation inside its budget: every turn resends
	// the whole conversation, so an oversized tool result would blow up both the
	// request and the process's memory.
	appendMessage := func(msg AgentMessage) error {
		if !budget.add(len(msg.Content)) {
			return errors.Errorf("LLM conversation exceeded %d bytes", maxConversationBytes)
		}
		messages = append(messages, msg)
		return nil
	}

	for turn := 1; turn <= maxTurns; turn++ {
		if ctx.Err() != nil {
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: ctx.Err()})
			return
		}

		if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventTurnStart, Turn: turn}) {
			return
		}

		// 1. Call LLM — one turn.
		llmMsgs := ConvertToLlm(messages)
		assistantMsg, err := streamOneTurn(ctx, cfg, llmMsgs, ch)
		if err != nil {
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: err})
			return
		}
		// An empty completion (an error page from a proxy, an empty content
		// field) must not be returned or cached as an explanation.
		if len(assistantMsg.ToolCalls) == 0 && strings.TrimSpace(assistantMsg.Content) == "" {
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: errors.New("LLM returned an empty response")})
			return
		}
		if err := appendMessage(assistantMsg); err != nil {
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: err})
			return
		}

		// 2. No tool calls → agent is done.
		if len(assistantMsg.ToolCalls) == 0 {
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventAgentEnd})
			return
		}

		// 3. Execute tools sequentially.
		for _, tc := range assistantMsg.ToolCalls {
			if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventToolStart, ToolCall: &tc, Turn: turn}) {
				return
			}

			// Execute.
			results, execErr := cfg.Executor(tc)
			var content string
			if execErr != nil {
				content = fmt.Sprintf("error: %s", execErr.Error())
			} else if len(results) > 0 {
				content = results[0].Content
			}

			evt := AgentEvent{Type: AgentEventToolEnd, ToolCall: &tc, ToolResult: content, Turn: turn}
			if execErr != nil {
				evt.ToolError = execErr.Error()
			}
			if !sendEvent(ctx, ch, evt) {
				return
			}

			if err := appendMessage(AgentMessage{
				Role: "toolResult", ToolCallID: tc.ID,
				ToolName: tc.Function.Name,
				Content:  content,
			}); err != nil {
				sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: err})
				return
			}
		}
	}

	// Reaching here means every turn requested tool calls, so the last batch of
	// tool results was never sent back to the model: the answer is incomplete
	// and must not be presented or cached as final.
	sendEvent(ctx, ch, AgentEvent{
		Type:  AgentEventError,
		Error: errors.Errorf("LLM agent reached the maximum of %d turns without producing a final answer", maxTurns),
	})
}

// streamOneTurn calls the LLM once and returns the full assistant message.
func streamOneTurn(ctx context.Context, cfg AgentConfig, llmMsgs []Message, ch chan<- AgentEvent) (AgentMessage, error) {
	var (
		fullContent strings.Builder
		toolCalls   []ToolCall
	)

	streamCh := streamRaw(ctx, cfg, llmMsgs)
	for chunk := range streamCh {
		if chunk.Error != nil {
			return AgentMessage{}, chunk.Error
		}
		if chunk.Content != "" {
			_, _ = fullContent.WriteString(chunk.Content)
			if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventContent, Content: chunk.Content}) {
				return AgentMessage{}, ctx.Err()
			}
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		if chunk.Done {
			break
		}
	}

	return AgentMessage{
		Role:      "assistant",
		Content:   fullContent.String(),
		ToolCalls: toolCalls,
	}, nil
}

// ---- Raw LLM Streaming (one request, no tool recursion) ----

type rawStreamChunk struct {
	Content   string
	ToolCalls []ToolCall
	Done      bool
	Error     error
}

func streamRaw(ctx context.Context, cfg AgentConfig, messages []Message) <-chan rawStreamChunk {
	ch := make(chan rawStreamChunk, 16)

	go func() {
		defer close(ch)
		if err := ValidateBaseURL(cfg.Provider.BaseURL); err != nil {
			sendRaw(ctx, ch, rawStreamChunk{Error: err})
			return
		}
		endpoint := strings.TrimRight(cfg.Provider.BaseURL, "/") + "/v1/chat/completions"

		body := chatRequest{
			Model:    cfg.Provider.ModelName,
			Messages: messages,
			Stream:   true,
		}
		if len(cfg.Tools) > 0 {
			body.Tools = cfg.Tools
		}

		reqBytes, err := json.Marshal(body)
		if err != nil {
			sendRaw(ctx, ch, rawStreamChunk{Error: errors.Wrap(err, "marshal request")})
			return
		}

		reqCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
		if err != nil {
			sendRaw(ctx, ch, rawStreamChunk{Error: errors.Wrap(err, "create request")})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Provider.APIKey)

		resp, err := llmHTTPClient.Do(httpReq)
		if err != nil {
			sendRaw(ctx, ch, rawStreamChunk{Error: errors.Wrap(err, "LLM request failed")})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			// The body is the provider's, and may echo request details; keep it
			// in the server log and return only the status to the caller.
			slog.Error("LLM provider returned a non-OK status", "status", resp.StatusCode, "body", string(errBody))
			sendRaw(ctx, ch, rawStreamChunk{Error: fmt.Errorf("LLM provider returned status %d", resp.StatusCode)})
			return
		}

		// Stream the body as it arrives. The previous implementation buffered
		// the entire response with io.ReadAll before parsing, so nothing reached
		// the caller until the model had finished and any response over 1MiB was
		// silently truncated, dropping trailing tool-call arguments.
		idle := newIdleTimeoutReader(resp.Body, llmIdleReadTimeout, cancel)
		defer idle.stop()

		var debugBuf boundedBuffer
		stream := io.TeeReader(&maxBytesReader{r: idle, remaining: llmMaxResponseBytes}, &debugBuf)
		parseErr := parseStream(reqCtx, stream, ch)

		if cfg.DebugLogger != nil {
			cfg.DebugLogger(string(reqBytes), debugBuf.String())
		}
		if parseErr == nil {
			return
		}
		if idle.timedOut() {
			sendRaw(ctx, ch, rawStreamChunk{Error: errors.Errorf("LLM response stalled: no data received for %s", llmIdleReadTimeout)})
			return
		}
		sendRaw(ctx, ch, rawStreamChunk{Error: parseErr})
	}()

	return ch
}

// parseStream decodes an OpenAI-compatible SSE stream as it is read. It returns
// an error for a malformed chunk, a truncated response (finish_reason=length),
// a stream that ends without a finish_reason, and a read failure.
func parseStream(ctx context.Context, body io.Reader, ch chan<- rawStreamChunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), llmMaxSSELineBytes)

	toolCallBufs := make(map[int]*toolCallAccum)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// "[DONE]" is an explicit end of stream: the provider finished, even if
		// it never set a finish_reason. An EOF without it is a dropped stream.
		if line == "data: [DONE]" {
			sendRaw(ctx, ch, rawStreamChunk{Done: true})
			return nil
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
			return errors.Wrap(err, "malformed SSE data chunk")
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if !sendRaw(ctx, ch, rawStreamChunk{Content: choice.Delta.Content}) {
					return ctx.Err()
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				acc := toolCallBufs[tc.Index]
				if acc == nil {
					acc = &toolCallAccum{}
					toolCallBufs[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.ID = tc.ID
					acc.Type = tc.Type
				}
				if tc.Function.Name != "" {
					acc.Name = tc.Function.Name
				}
				_, _ = acc.ArgsBuf.WriteString(tc.Function.Arguments)
			}

			if choice.FinishReason == nil {
				continue
			}
			switch *choice.FinishReason {
			case "tool_calls":
				if !sendRaw(ctx, ch, rawStreamChunk{ToolCalls: buildAccumulatedToolCalls(toolCallBufs)}) {
					return ctx.Err()
				}
				sendRaw(ctx, ch, rawStreamChunk{Done: true})
				return nil
			case "stop":
				sendRaw(ctx, ch, rawStreamChunk{Done: true})
				return nil
			case "length":
				return errors.New("LLM response was truncated by the provider (finish_reason=length)")
			default:
				return errors.Errorf("unexpected finish_reason %q from the LLM provider", *choice.FinishReason)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return errors.Errorf("LLM response exceeded %d bytes", llmMaxResponseBytes)
		}
		return errors.Wrap(err, "failed to read the LLM response stream")
	}
	return errors.New("LLM response stream ended without a finish_reason or [DONE]")
}

// buildAccumulatedToolCalls returns the accumulated tool calls ordered by their
// stream index. Providers are not required to start at index 0 or to keep the
// indexes contiguous, so the map keys are sorted rather than walked 0..len-1.
func buildAccumulatedToolCalls(bufs map[int]*toolCallAccum) []ToolCall {
	indexes := make([]int, 0, len(bufs))
	for index := range bufs {
		indexes = append(indexes, index)
	}
	slices.Sort(indexes)

	calls := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		acc := bufs[index]
		if acc == nil {
			continue
		}
		calls = append(calls, ToolCall{
			ID:   acc.ID,
			Type: acc.Type,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      acc.Name,
				Arguments: acc.ArgsBuf.String(),
			},
		})
	}
	return calls
}

// toolCallAccum accumulates incremental tool call arguments across SSE chunks.
type toolCallAccum struct {
	ID      string
	Type    string
	Name    string
	ArgsBuf strings.Builder
}

// idleTimeoutReader cancels the request when no bytes arrive for the timeout.
// It bounds a stalled stream without capping the total generation time.
type idleTimeoutReader struct {
	r       io.Reader
	timeout time.Duration
	timer   *time.Timer
	expired atomic.Bool
}

func newIdleTimeoutReader(r io.Reader, timeout time.Duration, cancel context.CancelFunc) *idleTimeoutReader {
	ir := &idleTimeoutReader{r: r, timeout: timeout}
	ir.timer = time.AfterFunc(timeout, func() {
		ir.expired.Store(true)
		cancel()
	})
	return ir
}

func (ir *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if err == nil {
		ir.timer.Reset(ir.timeout)
	}
	return n, err
}

func (ir *idleTimeoutReader) stop()          { ir.timer.Stop() }
func (ir *idleTimeoutReader) timedOut() bool { return ir.expired.Load() }

// maxBytesReader fails instead of truncating once the cap is reached.
type maxBytesReader struct {
	r         io.Reader
	remaining int64
}

func (m *maxBytesReader) Read(p []byte) (int, error) {
	if m.remaining <= 0 {
		return 0, errResponseTooLarge
	}
	if int64(len(p)) > m.remaining {
		p = p[:m.remaining]
	}
	n, err := m.r.Read(p)
	m.remaining -= int64(n)
	return n, err
}

// boundedBuffer keeps at most limit bytes. It only backs the debug log copy.
type boundedBuffer struct {
	buf   strings.Builder
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	total := len(p)
	limit := b.limit
	if limit <= 0 {
		limit = llmDebugBodyLimit
	}
	if b.buf.Len() < limit {
		room := limit - b.buf.Len()
		if len(p) > room {
			p = p[:room]
		}
		_, _ = b.buf.Write(p)
	}
	return total, nil
}

func (b *boundedBuffer) String() string { return b.buf.String() }
