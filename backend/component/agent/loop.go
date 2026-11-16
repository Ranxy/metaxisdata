package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// ToolExecutor executes one tool call and returns the result content the loop
// feeds back to the model.
type ToolExecutor func(toolCall llm.ToolCall) ([]llm.ToolResult, error)

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
func messageBudgetBytes(message Message) int {
	size := len(message.Content) + len(message.ToolCallID) + len(message.ToolName)
	for _, call := range message.ToolCalls {
		size += len(call.ID) + len(call.GetName()) + len(call.Function.Arguments)
	}
	return size
}

// ---- Agent Loop ----

// RunLoop runs the agent loop: LLM ↔ tool calls until completion.
// It emits events to the channel and closes it when done.
func RunLoop(ctx context.Context, cfg Config) <-chan Event {
	ch := make(chan Event, 32)

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
func sendEvent(ctx context.Context, ch chan<- Event, evt Event) bool {
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
func run(ctx context.Context, cfg Config, ch chan<- Event) {
	startedAt := time.Now()
	outcome := metrics.LLMOutcomeError
	var (
		usage            llm.Usage
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
	messages := make([]Message, 0, len(cfg.History)+2)
	if len(cfg.History) > 0 {
		// A session wake continues the stored conversation; the caller's History
		// already carries its system message and the new wake message.
		for _, message := range cfg.History {
			if !budget.add(messageBudgetBytes(message)) {
				sendEvent(ctx, ch, Event{Type: EventError,
					Error: errors.Errorf("LLM conversation exceeded %d bytes", maxConversationBytes)})
				return
			}
			messages = append(messages, message)
		}
	} else {
		budget.add(len(cfg.SystemPrompt))
		budget.add(len(cfg.UserPrompt))
		messages = append(messages,
			Message{Role: "system", Content: cfg.SystemPrompt},
			Message{Role: "user", Content: cfg.UserPrompt},
		)
	}

	// appendMessage keeps the conversation inside its budget: every turn resends
	// the whole conversation, so an oversized tool result would blow up both the
	// request and the process's memory.
	appendMessage := func(msg Message) error {
		if !budget.add(messageBudgetBytes(msg)) {
			return errors.Errorf("LLM conversation exceeded %d bytes", maxConversationBytes)
		}
		messages = append(messages, msg)
		return nil
	}

	for turn := 1; turn <= maxTurns; turn++ {
		if ctx.Err() != nil {
			outcome = metrics.LLMOutcomeCanceled
			sendEvent(ctx, ch, Event{Type: EventError, Error: ctx.Err()})
			return
		}

		if !sendEvent(ctx, ch, Event{Type: EventTurnStart, Turn: turn}) {
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
			sendEvent(ctx, ch, Event{Type: EventError, Error: err})
			return
		}
		responseReceived = true
		if turnUsage != nil {
			usageReported = true
			usage.Add(turnUsage)
			if !sendEvent(ctx, ch, Event{Type: EventUsage, Turn: turn, Usage: turnUsage}) {
				outcome = metrics.LLMOutcomeCanceled
				return
			}
		}
		// An empty completion (an error page from a proxy, an empty content
		// field) must not be returned or cached as an explanation.
		if len(assistantMsg.ToolCalls) == 0 && strings.TrimSpace(assistantMsg.Content) == "" {
			outcome = metrics.LLMOutcomeEmptyResponse
			sendEvent(ctx, ch, Event{Type: EventError, Error: &llm.ResponseError{
				Code: llm.FailureMalformed,
				Err:  errors.New("LLM returned an empty response"),
			}})
			return
		}
		if err := appendMessage(assistantMsg); err != nil {
			sendEvent(ctx, ch, Event{Type: EventError, Error: err})
			return
		}
		// Report the completed message before the tool calls run: a session recorder
		// persists it verbatim (the tool calls included) and the UI can show the
		// assistant text while the tools execute.
		completed := assistantMsg
		if !sendEvent(ctx, ch, Event{Type: EventAssistant, Assistant: &completed, Turn: turn}) {
			outcome = metrics.LLMOutcomeCanceled
			return
		}

		// 2. No tool calls → agent is done.
		if len(assistantMsg.ToolCalls) == 0 {
			outcome = metrics.LLMOutcomeSuccess
			end := Event{Type: EventEnd}
			if usageReported {
				end.Usage = &usage
			}
			sendEvent(ctx, ch, end)
			return
		}

		// 3. Execute the batch: consecutive parallel-safe calls run together,
		// everything else is a barrier and runs alone. Events and conversation
		// messages commit in model order regardless of completion order.
		if !executeToolBatch(ctx, cfg, assistantMsg.ToolCalls, turn, ch, appendMessage) {
			// A failed batch is a cancellation only when the caller went away; a
			// budget/append failure is an error, not a canceled call.
			if ctx.Err() != nil {
				outcome = metrics.LLMOutcomeCanceled
			}
			return
		}
	}

	// Reaching here means every turn requested tool calls, so the last batch of
	// tool results was never sent back to the model: the answer is incomplete
	// and must not be presented or cached as final.
	sendEvent(ctx, ch, Event{
		Type:  EventError,
		Error: errors.Errorf("LLM agent reached the maximum of %d turns without producing a final answer", maxTurns),
	})
}

// toolOutcome is one executed call's result.
type toolOutcome struct {
	content string
	err     error
}

// executeToolBatch runs one model batch and reports whether the loop may
// continue. It preserves model order in both the event stream and the
// conversation: starts are emitted in order, then results are committed in
// order, even when the bodies overlapped.
func executeToolBatch(
	ctx context.Context,
	cfg Config,
	calls []llm.ToolCall,
	turn int,
	ch chan<- Event,
	appendMessage func(Message) error,
) bool {
	for index := 0; index < len(calls); {
		batch := calls[index : index+1]
		if cfg.MaxParallelToolCalls > 1 && cfg.ParallelSafe != nil && cfg.ParallelSafe(calls[index].Function.Name) {
			for len(batch) < cfg.MaxParallelToolCalls && index+len(batch) < len(calls) {
				next := calls[index+len(batch)]
				if !cfg.ParallelSafe(next.Function.Name) {
					break
				}
				batch = calls[index : index+len(batch)+1]
			}
		}
		index += len(batch)

		for position := range batch {
			call := batch[position]
			if !sendEvent(ctx, ch, Event{Type: EventToolStart, ToolCall: &call, Turn: turn}) {
				return false
			}
		}
		outcomes := runToolBatch(cfg, batch)
		for position := range batch {
			call := batch[position]
			outcome := outcomes[position]
			evt := Event{Type: EventToolEnd, ToolCall: &call, ToolResult: outcome.content, Turn: turn}
			if outcome.err != nil {
				evt.ToolError = outcome.err.Error()
			}
			if !sendEvent(ctx, ch, evt) {
				return false
			}
			toolResult := Message{
				Role: "toolResult", ToolCallID: call.ID,
				ToolName: call.Function.Name,
				Content:  outcome.content,
			}
			if err := appendMessage(toolResult); err != nil {
				sendEvent(ctx, ch, Event{Type: EventError, Error: err})
				return false
			}
		}
	}
	return true
}

// runToolBatch executes the batch, overlapping the safe calls. A single call is
// executed inline: spawning a goroutine for the serial path would only add a
// scheduling point.
func runToolBatch(cfg Config, batch []llm.ToolCall) []toolOutcome {
	if len(batch) == 1 {
		return []toolOutcome{executeOne(cfg, batch[0])}
	}
	outcomes := make([]toolOutcome, len(batch))
	var wait sync.WaitGroup
	wait.Add(len(batch))
	for index := range batch {
		go func(index int) {
			defer wait.Done()
			outcomes[index] = executeOne(cfg, batch[index])
		}(index)
	}
	wait.Wait()
	return outcomes
}

// executeOne runs one tool call and normalizes its failure into the content the
// model reads.
func executeOne(cfg Config, call llm.ToolCall) toolOutcome {
	results, err := cfg.Executor(call)
	if err != nil {
		return toolOutcome{content: fmt.Sprintf("error: %s", err.Error()), err: err}
	}
	if len(results) == 0 {
		return toolOutcome{}
	}
	return toolOutcome{content: results[0].Content}
}

// streamOneTurn calls the LLM once and returns the full assistant message and the
// token usage the provider reported for this turn (nil when it reported none).
func streamOneTurn(ctx context.Context, cfg Config, llmMsgs []llm.Message, ch chan<- Event) (Message, *llm.Usage, error) {
	var (
		fullContent strings.Builder
		toolCalls   []llm.ToolCall
		turnUsage   *llm.Usage
	)

	streamCh := llm.Stream(ctx, llm.StreamRequest{
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
			return Message{}, nil, chunk.Error
		}
		if chunk.Content != "" {
			_, _ = fullContent.WriteString(chunk.Content)
			if !sendEvent(ctx, ch, Event{Type: EventContent, Content: chunk.Content}) {
				return Message{}, nil, ctx.Err()
			}
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		if chunk.Usage != nil {
			turnUsage = chunk.Usage
		}
	}

	return Message{
		Role:      "assistant",
		Content:   fullContent.String(),
		ToolCalls: toolCalls,
	}, turnUsage, nil
}
