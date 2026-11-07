package agentrunner

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// compactionRetainRatio is the share of the trigger budget kept verbatim as the
	// recent tail; the older region is summarized.
	compactionRetainRatio = 0.16
	// compactionForcedRetainRatio is the smaller tail kept after a provider rejected
	// the context as too large.
	compactionForcedRetainRatio = 0.08
	// compactionMinRegionMessages avoids a summary call for a trivial region.
	compactionMinRegionMessages = 4
	// compactionMaxOutputTokens bounds the summary text.
	compactionMaxOutputTokens = 8192
	// pruneToolResultHead and pruneToolResultTail keep the useful ends of an old tool
	// result while dropping the middle before a summary call.
	pruneToolResultHead = 2048
	pruneToolResultTail = 512
	// pruneMarker replaces the removed middle of a tool result.
	pruneMarker = "\n\n[... tool result pruned ...]\n\n"
	// keepRecentToolResults is how many recent messages keep their tool results intact
	// in the model context.
	keepRecentToolResults = 40
	// compactionFeature labels the summarization call in metrics.
	compactionFeature = "dq_agent_compaction"
)

// compactionInstruction is the final user message of a summary call. The replayed
// conversation comes first, so the provider can reuse its prefix cache.
const compactionInstruction = `You are now acting as a compaction engine for a long-running data quality agent.
Condense the conversation ABOVE into a structured checkpoint that lets the agent resume the work with no loss of essential context.

Output EXACTLY the Markdown structure below: keep every section, in order. Use terse bullets, not prose paragraphs. Write "(none)" for an empty section — never drop a section.

## Session Objective and Target
- [the target (table/view) and what the agent is trying to keep true about it]

## Work Completed
- [wakes already handled, checks run or reviewed, events investigated]

## Confirmed Findings and Investigations
- [investigation ids/verdicts and the finding behind each]

## Memories and Decisions
- [asset facts and proposed decisions already recorded; mark proposals as proposals]

## Open Issues and Pending Work
- [unfinished work, blockers waiting on a person, failures that still need a conclusion]

## Tool Results Worth Keeping
- [the few tool results whose exact content still matters: ids, counts, error strings]

## Next Step
- [the single next action, or "(none)"]

## Critical Context
- [constraints, decisions and their rationale, anything needed to continue]

Rules:
- Preserve exact target GUIDs, execution ids, rule ids, error codes, numeric values and dates.
- Do not mention this summarization request or that the context was compacted.
- If a previous checkpoint summary is included, merge it: keep still-true facts, drop stale ones.
- Output only the checkpoint text: do not call any tool or take any other action.`

// compactionSystemPrompt frames the summarization call.
func compactionSystemPrompt() string {
	return "You compress the transcript of a long-running data quality agent into a checkpoint. " +
		"The checkpoint is the agent's only memory of the summarized span, so keep every fact the next " +
		"wake needs and drop small talk."
}

// maybeCompact summarizes the oldest part of the conversation when the context is over
// the trigger budget (or after a provider context overflow). It persists one COMPACTION
// message and advances the session's summary watermark; the transcript itself is never
// deleted.
func (e *Engine) maybeCompact(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	wake int32,
	config llm.ResolvedConfig,
	messages []*store.QualityAgentTranscriptMessage,
	trigger int,
	forced bool,
) (int64, bool, error) {
	if e.chat == nil || len(messages) == 0 {
		return 0, false, nil
	}
	if trigger <= 0 {
		trigger = dataquality.DefaultAgentRunnerCompactionTriggerTokens
	}
	pressure := int(session.ContextTokens)
	if estimated := EstimateTokens(BuildConversation(session, nil, messages)); estimated > pressure {
		pressure = estimated
	}
	if !forced && pressure <= trigger {
		return 0, false, nil
	}
	retain := int(float64(trigger) * compactionRetainRatio)
	if forced {
		retain = int(float64(trigger) * compactionForcedRetainRatio)
	}
	cut := balancedCut(messages, retainedCut(messages, retain))
	start := 0
	for start < cut && messages[start].Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM {
		start++
	}
	if cut-start < compactionMinRegionMessages {
		return 0, false, nil
	}
	region := messages[start:cut]
	throughSeq := region[len(region)-1].Seq

	summaryMessages := []llm.AgentMessage{{Role: "system", Content: compactionSystemPrompt()}}
	if session.Summary != "" {
		summaryMessages = append(summaryMessages, llm.AgentMessage{
			Role:    "user",
			Content: "Previous checkpoint summary (merge it; keep still-true facts and drop stale ones):\n" + session.Summary,
		})
	}
	summaryMessages = append(summaryMessages, pruneToolResults(BuildConversation(nil, nil, region), 0)...)
	summaryMessages = append(summaryMessages, llm.AgentMessage{Role: "user", Content: compactionInstruction})

	var (
		summary string
		tokens  int64
	)
	for event := range e.chat(ctx, llm.AgentConfig{
		Provider:             config,
		Feature:              compactionFeature,
		History:              summaryMessages,
		MaxTurns:             1,
		MaxTokens:            compactionMaxOutputTokens,
		MaxConversationBytes: llm.DefaultMaxConversationBytes,
		IncludeUsage:         true,
		Metrics:              e.metrics,
	}) {
		switch event.Type {
		case llm.AgentEventAssistant:
			if event.Assistant != nil && strings.TrimSpace(event.Assistant.Content) != "" {
				summary = event.Assistant.Content
			}
		case llm.AgentEventUsage:
			if event.Usage != nil {
				tokens += event.Usage.TotalTokens
				if event.Usage.TotalTokens == 0 {
					tokens += event.Usage.PromptTokens + event.Usage.CompletionTokens
				}
			}
		case llm.AgentEventError:
			return tokens, false, errors.Wrap(event.Error, "the context compaction failed")
		default:
		}
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return tokens, false, errors.New("the context compaction produced no summary")
	}
	// A summary larger than the span it replaces is not progress; keep the original.
	if estimateTextTokens(summary) >= EstimateTokens(BuildConversation(nil, nil, region)) {
		return tokens, false, nil
	}
	_, appendErr := e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           wake,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_COMPACTION,
		Content:        summary,
		IdempotencyKey: fmt.Sprintf("compaction:%d", throughSeq),
	})
	if appendErr != nil {
		return tokens, false, appendErr
	}
	stored, err := e.store.UpdateQualityAgentSessionCompaction(ctx, session.ResourceID, summary, throughSeq)
	if err != nil {
		return tokens, false, err
	}
	if !stored {
		return tokens, false, errors.New("the session changed before its compaction could be stored")
	}
	session.Summary = summary
	session.SummaryThroughSeq = throughSeq
	return tokens, true, nil
}

// retainedCut returns the index of the first message that stays verbatim: the tail
// whose estimated tokens reach the retention budget.
func retainedCut(messages []*store.QualityAgentTranscriptMessage, retain int) int {
	total := 0
	for index := len(messages) - 1; index >= 0; index-- {
		total += estimateMessageTokens(messages[index])
		if total >= retain {
			return index
		}
	}
	return 0
}

// balancedCut moves a cut earlier until it does not split an assistant tool call from
// its results. A provider rejects a conversation whose tool call has no result.
func balancedCut(messages []*store.QualityAgentTranscriptMessage, cut int) int {
	if cut <= 0 || cut > len(messages) {
		return 0
	}
	pending := make([]int, len(messages)+1)
	for index, message := range messages {
		pending[index+1] = pending[index]
		switch message.Role {
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT:
			pending[index+1] += len(message.ToolCalls.GetToolCalls())
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL:
			pending[index+1]--
		default:
		}
	}
	for index := cut; index > 0; index-- {
		if pending[index] == 0 {
			return index
		}
	}
	return 0
}

// estimateMessageTokens approximates one stored message's tokens.
func estimateMessageTokens(message *store.QualityAgentTranscriptMessage) int {
	total := estimateTextTokens(message.Content) + estimateTextTokens(message.ToolCallID) +
		estimateTextTokens(message.ToolName)
	for _, call := range message.ToolCalls.GetToolCalls() {
		total += estimateTextTokens(call.GetName()) + estimateTextTokens(call.GetArguments())
	}
	return total
}

// pruneToolResults replaces the middle of old tool results with a marker, keeping the
// head and tail. It runs before a summary call (no model request) and, with a recent
// window, on the normal model context so an old oversized result stops being resent in
// full on every turn.
func pruneToolResults(messages []llm.AgentMessage, keepRecent int) []llm.AgentMessage {
	limit := len(messages) - keepRecent
	for index := 0; index < limit && index < len(messages); index++ {
		message := &messages[index]
		if message.Role != "toolResult" {
			continue
		}
		if len(message.Content) <= pruneToolResultHead+pruneToolResultTail+len(pruneMarker) {
			continue
		}
		message.Content = common.TruncateUTF8Bytes(message.Content, pruneToolResultHead) +
			pruneMarker + lastRunes(message.Content, pruneToolResultTail)
	}
	return messages
}

// lastRunes returns at most limit bytes from the end of value, never splitting a rune.
func lastRunes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}
