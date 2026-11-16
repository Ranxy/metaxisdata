package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/metrics"
)

const (
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

// messageBudgetBytes sizes one message for the conversation cap: the text plus the
// tool-call fields the conversation resends on every turn.
func messageBudgetBytes(message AgentMessage) int {
	size := len(message.Content) + len(message.ToolCallID) + len(message.ToolName)
	for _, call := range message.ToolCalls {
		size += len(call.ID) + len(call.GetName()) + len(call.Function.Arguments)
	}
	return size
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

// run drives one agent-loop invocation. It reports the invocation's outcome to
// cfg.Metrics as one completed model call, labelled with the caller's feature:
// the loop is the only place that knows how the invocation ended, so no caller
// records calls, duration or tokens itself.
func run(ctx context.Context, cfg AgentConfig, ch chan<- AgentEvent) {
	startedAt := time.Now()
	outcome := metrics.LLMOutcomeError
	var (
		usage            Usage
		usageReported    bool
		responseReceived bool
	)
	defer func() {
		// The loop can make several turns; the call metric counts the invocation
		// once and attributes the tokens the provider reported across its turns.
		var promptTokens, completionTokens int64
		if usageReported {
			promptTokens = usage.PromptTokens
			completionTokens = usage.CompletionTokens
		}
		// The label is the operator-configured title; the resource name embeds a
		// generated id, which is not a label value. Fall back for callers that
		// build a config without a title (tests).
		provider := cfg.Provider.ProfileTitle
		if provider == "" {
			provider = cfg.Provider.ProfileName
		}
		cfg.Metrics.ObserveLLMCall(cfg.Feature, provider, outcome, time.Since(startedAt), promptTokens, completionTokens)
		// A call that never got a provider response did not "omit usage": the
		// missing-usage signal is about completions, so canceled/errored-before-
		// response invocations are excluded.
		if responseReceived && !usageReported {
			cfg.Metrics.CountLLMUsageMissing(cfg.Feature)
		}
	}()

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	maxConversationBytes := cfg.MaxConversationBytes
	if maxConversationBytes <= 0 {
		maxConversationBytes = DefaultMaxConversationBytes
	}
	budget := &conversationBudget{limit: maxConversationBytes}

	// usage accumulates over the turns the provider reported; a provider that
	// reports nothing leaves usageReported false rather than claiming zero tokens.
	messages := make([]AgentMessage, 0, len(cfg.History)+2)
	if len(cfg.History) > 0 {
		// A session wake continues the stored conversation; the caller's History
		// already carries its system message and the new wake message.
		for _, message := range cfg.History {
			if !budget.add(messageBudgetBytes(message)) {
				sendEvent(ctx, ch, AgentEvent{Type: AgentEventError,
					Error: errors.Errorf("LLM conversation exceeded %d bytes", maxConversationBytes)})
				return
			}
			messages = append(messages, message)
		}
	} else {
		budget.add(len(cfg.SystemPrompt))
		budget.add(len(cfg.UserPrompt))
		messages = append(messages,
			AgentMessage{Role: "system", Content: cfg.SystemPrompt},
			AgentMessage{Role: "user", Content: cfg.UserPrompt},
		)
	}

	// appendMessage keeps the conversation inside its budget: every turn resends
	// the whole conversation, so an oversized tool result would blow up both the
	// request and the process's memory.
	appendMessage := func(msg AgentMessage) error {
		if !budget.add(messageBudgetBytes(msg)) {
			return errors.Errorf("LLM conversation exceeded %d bytes", maxConversationBytes)
		}
		messages = append(messages, msg)
		return nil
	}

	for turn := 1; turn <= maxTurns; turn++ {
		if ctx.Err() != nil {
			outcome = metrics.LLMOutcomeCanceled
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: ctx.Err()})
			return
		}

		if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventTurnStart, Turn: turn}) {
			outcome = metrics.LLMOutcomeCanceled
			return
		}

		// 1. Call LLM — one turn.
		llmMsgs := ConvertToLlm(messages)
		assistantMsg, turnUsage, err := streamOneTurn(ctx, cfg, llmMsgs, ch)
		if err != nil {
			if ctx.Err() != nil {
				outcome = metrics.LLMOutcomeCanceled
			}
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: err})
			return
		}
		responseReceived = true
		if turnUsage != nil {
			usageReported = true
			usage.add(turnUsage)
			if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventUsage, Turn: turn, Usage: turnUsage}) {
				outcome = metrics.LLMOutcomeCanceled
				return
			}
		}
		// An empty completion (an error page from a proxy, an empty content
		// field) must not be returned or cached as an explanation.
		if len(assistantMsg.ToolCalls) == 0 && strings.TrimSpace(assistantMsg.Content) == "" {
			outcome = metrics.LLMOutcomeEmptyResponse
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: &ResponseError{
				Code: FailureMalformed,
				Err:  errors.New("LLM returned an empty response"),
			}})
			return
		}
		if err := appendMessage(assistantMsg); err != nil {
			sendEvent(ctx, ch, AgentEvent{Type: AgentEventError, Error: err})
			return
		}
		// Report the completed message before the tool calls run: a session recorder
		// persists it verbatim (the tool calls included) and the UI can show the
		// assistant text while the tools execute.
		completed := assistantMsg
		if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventAssistant, Assistant: &completed, Turn: turn}) {
			outcome = metrics.LLMOutcomeCanceled
			return
		}

		// 2. No tool calls → agent is done.
		if len(assistantMsg.ToolCalls) == 0 {
			outcome = metrics.LLMOutcomeSuccess
			end := AgentEvent{Type: AgentEventAgentEnd}
			if usageReported {
				end.Usage = &usage
			}
			sendEvent(ctx, ch, end)
			return
		}

		// 3. Execute tools sequentially.
		for _, tc := range assistantMsg.ToolCalls {
			if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventToolStart, ToolCall: &tc, Turn: turn}) {
				outcome = metrics.LLMOutcomeCanceled
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
				outcome = metrics.LLMOutcomeCanceled
				return
			}

			toolResult := AgentMessage{
				Role: "toolResult", ToolCallID: tc.ID,
				ToolName: tc.Function.Name,
				Content:  content,
			}
			if err := appendMessage(toolResult); err != nil {
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

// streamOneTurn calls the LLM once and returns the full assistant message and the
// token usage the provider reported for this turn (nil when it reported none).
func streamOneTurn(ctx context.Context, cfg AgentConfig, llmMsgs []Message, ch chan<- AgentEvent) (AgentMessage, *Usage, error) {
	var (
		fullContent strings.Builder
		toolCalls   []ToolCall
		turnUsage   *Usage
	)

	streamCh := Stream(ctx, StreamRequest{
		Provider:       cfg.Provider,
		Messages:       llmMsgs,
		Tools:          cfg.Tools,
		MaxTokens:      cfg.MaxTokens,
		Temperature:    cfg.Temperature,
		ResponseFormat: cfg.ResponseFormat,
		IncludeUsage:   cfg.IncludeUsage,
		DebugLogger:    cfg.DebugLogger,
	})
	// Drain to the channel close instead of breaking on the Done chunk: the stream
	// goroutine invokes the debug logger and then closes the channel, so waiting for
	// the close gives the caller a happens-before edge over that callback. Breaking
	// early let a successful call return while the debug capture was still running.
	for chunk := range streamCh {
		if chunk.Error != nil {
			return AgentMessage{}, nil, chunk.Error
		}
		if chunk.Content != "" {
			_, _ = fullContent.WriteString(chunk.Content)
			if !sendEvent(ctx, ch, AgentEvent{Type: AgentEventContent, Content: chunk.Content}) {
				return AgentMessage{}, nil, ctx.Err()
			}
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		if chunk.Usage != nil {
			turnUsage = chunk.Usage
		}
	}

	return AgentMessage{
		Role:      "assistant",
		Content:   fullContent.String(),
		ToolCalls: toolCalls,
	}, turnUsage, nil
}
