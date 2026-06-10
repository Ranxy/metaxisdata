package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pkg/errors"
)

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

func run(ctx context.Context, cfg AgentConfig, ch chan<- AgentEvent) {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 6
	}

	messages := []AgentMessage{
		{Role: "system", Content: cfg.SystemPrompt},
		{Role: "user", Content: cfg.UserPrompt},
	}

	for turn := 1; turn <= maxTurns; turn++ {
		select {
		case <-ctx.Done():
			ch <- AgentEvent{Type: AgentEventError, Error: ctx.Err()}
			return
		default:
		}

		ch <- AgentEvent{Type: AgentEventTurnStart, Turn: turn}

		// 1. Call LLM — one turn.
		llmMsgs := ConvertToLlm(messages)
		assistantMsg, err := streamOneTurn(ctx, cfg, llmMsgs, ch)
		if err != nil {
			ch <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}
		messages = append(messages, assistantMsg)

		// 2. No tool calls → agent is done.
		if len(assistantMsg.ToolCalls) == 0 {
			ch <- AgentEvent{Type: AgentEventAgentEnd, Done: true}
			return
		}

		// 3. Execute tools sequentially.
		for _, tc := range assistantMsg.ToolCalls {
			ch <- AgentEvent{Type: AgentEventToolStart, ToolCall: &tc, Turn: turn}

			// Before hook.
			if cfg.Hooks.BeforeToolCall != nil {
				if reason := cfg.Hooks.BeforeToolCall(tc); reason != "" {
					ch <- AgentEvent{Type: AgentEventToolEnd, ToolCall: &tc, ToolError: reason, Turn: turn}
					messages = append(messages, AgentMessage{
						Role: "toolResult", ToolCallID: tc.ID,
						ToolName: tc.Function.Name,
						Content:  fmt.Sprintf("blocked: %s", reason),
					})
					continue
				}
			}

			// Execute.
			results, execErr := cfg.Executor(tc)
			var content string
			if execErr != nil {
				content = fmt.Sprintf("error: %s", execErr.Error())
			} else if len(results) > 0 {
				content = results[0].Content
			}

			// After hook.
			if cfg.Hooks.AfterToolCall != nil {
				content = cfg.Hooks.AfterToolCall(tc, content, execErr)
			}

			if execErr != nil {
				ch <- AgentEvent{Type: AgentEventToolEnd, ToolCall: &tc, ToolError: execErr.Error(), ToolResult: content, Turn: turn}
			} else {
				ch <- AgentEvent{Type: AgentEventToolEnd, ToolCall: &tc, ToolResult: content, Turn: turn}
			}

			messages = append(messages, AgentMessage{
				Role: "toolResult", ToolCallID: tc.ID,
				ToolName: tc.Function.Name,
				Content:  content,
			})
		}

		ch <- AgentEvent{Type: AgentEventTurnEnd, Turn: turn}
	}

	ch <- AgentEvent{Type: AgentEventAgentEnd, Done: true}
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
			ch <- AgentEvent{Type: AgentEventContent, Content: chunk.Content}
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
			ch <- rawStreamChunk{Error: errors.Wrap(err, "marshal request")}
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
		if err != nil {
			ch <- rawStreamChunk{Error: errors.Wrap(err, "create request")}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Provider.APIKey)

		resp, err := httpClient().Do(httpReq)
		if err != nil {
			ch <- rawStreamChunk{Error: errors.Wrap(err, "LLM request failed")}
			return
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		respStr := string(bodyBytes)

		if cfg.DebugLogger != nil {
			cfg.DebugLogger(string(reqBytes), respStr)
		}

		if resp.StatusCode != http.StatusOK {
			ch <- rawStreamChunk{Error: fmt.Errorf("LLM status %d: %.4000s", resp.StatusCode, respStr)}
			return
		}

		parseStream(respStr, ch)
	}()

	return ch
}

func httpClient() *http.Client {
	return &http.Client{Timeout: httpClientTimeout}
}

func parseStream(body string, ch chan<- rawStreamChunk) {
	toolCallBufs := make(map[int]*toolCallAccum)

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				ch <- rawStreamChunk{Content: choice.Delta.Content}
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

			if choice.FinishReason != nil {
				switch *choice.FinishReason {
				case "tool_calls":
					calls := buildAccumulatedToolCalls(toolCallBufs)
					ch <- rawStreamChunk{ToolCalls: calls}
					ch <- rawStreamChunk{Done: true}
					return
				case "stop":
					ch <- rawStreamChunk{Done: true}
					return
				default:
					ch <- rawStreamChunk{Done: true}
					return
				}
			}
		}
	}

	ch <- rawStreamChunk{Done: true}
}

func buildAccumulatedToolCalls(bufs map[int]*toolCallAccum) []ToolCall {
	var calls []ToolCall
	for i := 0; i < len(bufs); i++ {
		acc := bufs[i]
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
