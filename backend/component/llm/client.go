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
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
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

// DebugLogger receives full request/response bodies for debugging.
type DebugLogger func(reqBody string, respBody string)

// chatRequest is the JSON body sent to the chat completion API.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

// streamToolCall is a tool call fragment from the SSE stream.
type streamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatStreamChunk is one SSE data event.
type chatStreamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []streamToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamChat sends messages to the LLM and returns a channel of stream chunks.
func StreamChat(ctx context.Context, config ResolvedConfig, messages []Message, tools []ToolDef, executor ToolExecutor) <-chan StreamChunk {
	return StreamChatWithDebug(ctx, config, messages, tools, executor, nil)
}

// StreamChatWithDebug is like StreamChat but optionally logs request/response via debugLogger.
func StreamChatWithDebug(ctx context.Context, config ResolvedConfig, messages []Message, tools []ToolDef, executor ToolExecutor, debugLogger DebugLogger) <-chan StreamChunk {
	ch := make(chan StreamChunk, 16)

	go func() {
		defer close(ch)
		streamChatLoop(ctx, config, messages, tools, executor, 0, ch, debugLogger)
	}()

	return ch
}

func streamChatLoop(ctx context.Context, config ResolvedConfig, messages []Message, tools []ToolDef, executor ToolExecutor, round int, ch chan<- StreamChunk, debugLogger DebugLogger) {
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

	var fullContent strings.Builder
	var fullRespBody strings.Builder
	var toolCallBufs map[int]*toolCallAccum

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
		_, _ = fullRespBody.WriteString(data + "\n")

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				_, _ = fullContent.WriteString(choice.Delta.Content)
				ch <- StreamChunk{Content: choice.Delta.Content}
			}
			for _, tc := range choice.Delta.ToolCalls {
				if toolCallBufs == nil {
					toolCallBufs = make(map[int]*toolCallAccum)
				}
				acc, ok := toolCallBufs[tc.Index]
				if !ok {
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
				case "stop":
					if debugLogger != nil {
						debugLogger(string(reqBytes), fullRespBody.String())
					}
					ch <- StreamChunk{Done: true}
					return
				case "tool_calls":
					if debugLogger != nil {
						debugLogger(string(reqBytes), fullRespBody.String())
					}

					if executor == nil {
						ch <- StreamChunk{Error: errors.New("LLM requested tool calls but no executor provided")}
						return
					}

					var toolCalls []ToolCall
					// Build tool calls from accumulated chunks, sorted by index.
					for i := 0; i < len(toolCallBufs); i++ {
						acc := toolCallBufs[i]
						if acc == nil {
							continue
						}
						toolCalls = append(toolCalls, ToolCall{
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

					if fullContent.Len() > 0 {
						messages = append(messages, Message{Role: "assistant", Content: fullContent.String()})
						fullContent.Reset()
					}
					messages = append(messages, Message{Role: "assistant", ToolCalls: toolCalls})

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

					streamChatLoop(ctx, config, messages, tools, executor, round+1, ch, debugLogger)
					return
				default:
					if debugLogger != nil {
						debugLogger(string(reqBytes), fullRespBody.String())
					}
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

	if debugLogger != nil {
		debugLogger(string(reqBytes), fullRespBody.String())
	}
	ch <- StreamChunk{Done: true}
}

type toolCallAccum struct {
	ID      string
	Type    string
	Name    string
	ArgsBuf strings.Builder
}
