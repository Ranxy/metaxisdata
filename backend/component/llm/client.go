// Package llm provides LLM client capabilities.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a function call requested by the LLM.
type ToolCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Function  struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolDef defines a tool available to the LLM.
type ToolDef struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// ToolResult is the result of executing a tool call.
type ToolResult struct {
	ToolCallID string
	Content    string
}

// ToolExecutor executes tool calls and returns results.
type ToolExecutor func(toolCall ToolCall) ([]ToolResult, error)

// StreamChunk is a chunk from the streaming response.
type StreamChunk struct {
	Content string // text delta
	Done    bool
	Error   error
}

// chatRequest is the JSON body sent to the chat completion API.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

// chatStreamDelta is one SSE data event from the chat completion stream.
type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamChat sends messages to the LLM and returns a channel of stream chunks.
// It handles tool calling internally — the caller only sees text content chunks.
func StreamChat(ctx context.Context, config ResolvedConfig, messages []Message, tools []ToolDef, executor ToolExecutor) <-chan StreamChunk {
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)
		streamChatLoop(ctx, config, messages, tools, executor, 0, ch)
	}()

	return ch
}

func streamChatLoop(ctx context.Context, config ResolvedConfig, messages []Message, tools []ToolDef, executor ToolExecutor, round int, ch chan<- StreamChunk) {
	const maxRounds = 6

	if round >= maxRounds {
		ch <- StreamChunk{Error: errors.New("too many tool call rounds")}
		return
	}

	endpoint := strings.TrimRight(config.BaseURL, "/") + "/v1/chat/completions"

	body := chatRequest{
		Model:    config.ModelName,
		Messages: messages,
		Stream:   true,
	}
	if len(tools) > 0 && executor != nil {
		body.Tools = tools
	}

	reqBytes, err := json.Marshal(body)
	if err != nil {
		ch <- StreamChunk{Error: errors.Wrap(err, "failed to marshal request")}
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		ch <- StreamChunk{Error: errors.Wrap(err, "failed to create HTTP request")}
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.APIKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		ch <- StreamChunk{Error: errors.Wrap(err, "failed to call LLM")}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		ch <- StreamChunk{Error: fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(bodyBytes))}
		return
	}

	// Accumulate full assistant response for tool call tracking.
	var fullContent strings.Builder
	var toolCalls []ToolCall

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip unparseable chunks
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				fullContent.WriteString(choice.Delta.Content)
				ch <- StreamChunk{Content: choice.Delta.Content}
			}
			for _, tc := range choice.Delta.ToolCalls {
				if tc.ID != "" {
					toolCalls = append(toolCalls, tc)
				}
			}

			if choice.FinishReason != nil {
				switch *choice.FinishReason {
				case "stop":
					ch <- StreamChunk{Done: true}
					return
				case "tool_calls":
					// Execute tools and continue the conversation.
					if executor == nil {
						ch <- StreamChunk{Error: errors.New("LLM requested tool calls but no executor provided")}
						return
					}
					if fullContent.Len() > 0 {
						messages = append(messages, Message{
							Role:    "assistant",
							Content: fullContent.String(),
						})
						fullContent.Reset()
					}
					messages = append(messages, Message{Role: "assistant", ToolCalls: toolCalls})

					// Execute all tool calls.
					for _, tc := range toolCalls {
						results, execErr := executor(tc)
						if execErr != nil {
							ch <- StreamChunk{Error: errors.Wrap(execErr, fmt.Sprintf("tool call %s failed", tc.Function.Name))}
							return
						}
						for _, r := range results {
							messages = append(messages, Message{
								Role:       "tool",
								ToolCallID: r.ToolCallID,
								Content:    r.Content,
							})
						}
					}

					// Continue the loop with the extended messages.
					streamChatLoop(ctx, config, messages, tools, executor, round+1, ch)
					return
				default:
					ch <- StreamChunk{Done: true}
					return
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Error: errors.Wrap(err, "stream read error")}
		return
	}

	// Scanner ended without explicit finish — treat as done.
	ch <- StreamChunk{Done: true}
}
