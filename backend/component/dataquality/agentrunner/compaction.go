package agentrunner

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/transcript"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// compactionMaxOutputTokens bounds the summary text.
	compactionMaxOutputTokens = 8192
	// keepRecentToolResults is how many recent messages keep their tool results intact
	// in the model context. Older results keep only their head and tail; the window is
	// small enough that a few oversized results cannot keep the context over budget.
	keepRecentToolResults = 20
	// compactionFeature labels the summarization call in metrics.
	compactionFeature = "dq_agent_compaction"
)

// emergencyCompactionTrigger is the forced-compaction threshold for a stale model
// pair whose normal pressure budget is zero: leave a quarter of the message room
// as the verbatim tail so overflow recovery still has a legal cut.
func emergencyCompactionTrigger(config llm.ResolvedConfig) int {
	room := config.ContextWindow - config.MaxOutputTokens
	if room <= 0 {
		return 0
	}
	trigger := room / 4
	if trigger < 4096 {
		trigger = 4096
	}
	if trigger > room {
		trigger = room
	}
	return trigger
}

// compactionInstruction is the final user message of a summary call. The replayed
// conversation comes first, so the provider can reuse its prefix cache.
const compactionInstruction = `You are now acting as a compaction engine for a long-running data quality agent.
Condense the conversation ABOVE into a structured checkpoint that lets the agent resume the work with no loss of essential context.

Output EXACTLY the Markdown structure below: keep every section, in order. Use terse bullets, not prose paragraphs. Write "(none)" for an empty section — never drop a section.

## Session Objective and Target
- [the target (table/view) and what the agent is trying to keep true about it]

## Work Completed
- [wakes already handled, checks run or reviewed, events investigated]

## Confirmed Findings
- [the asset facts already recorded and the evidence behind each]

## Recorded Asset Facts
- [the current asset fact for the target and what it established]

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
	system *store.QualityAgentTranscriptMessage,
	config llm.ResolvedConfig,
	messages []*store.QualityAgentTranscriptMessage,
	trigger int,
	forced bool,
) (int64, bool, error) {
	if e.chat == nil || len(messages) == 0 {
		return 0, false, nil
	}
	if trigger <= 0 {
		trigger = llm.CompactionTrigger(config.ContextWindow, config.MaxOutputTokens)
	}
	if trigger <= 0 {
		if !forced {
			e.recordEngine(ctx, session, wake, 0,
				"compaction skipped: the model capacities leave no pressure budget")
			return 0, false, nil
		}
		trigger = emergencyCompactionTrigger(config)
		if trigger <= 0 {
			e.recordEngine(ctx, session, wake, 0,
				"compaction skipped: the model output budget fills its context window")
			return 0, false, nil
		}
	}
	entries := transcriptEntries(messages)
	// Prune first: replacing the middle of old tool results is free and may already be
	// enough. Only when the pruned context is still over the trigger is a summary call
	// worth its tokens.
	pruned := transcript.PruneToolResults(transcript.BuildConversation("", session.Summary, entries), keepRecentToolResults)
	pressure := transcript.EstimateTokens(pruned)
	if !forced && pressure <= trigger {
		return 0, false, nil
	}
	plan, ok := transcript.PlanCompaction(entries, trigger, forced)
	if !ok {
		return 0, false, nil
	}
	throughSeq := plan.ThroughSeq

	systemPrompt := compactionSystemPrompt()
	if system != nil && system.Content != "" {
		systemPrompt = system.Content
	}
	summaryMessages := []agent.Message{{Role: "system", Content: systemPrompt}}
	if session.Summary != "" {
		summaryMessages = append(summaryMessages, agent.Message{
			Role:    "user",
			Content: "Previous checkpoint summary (merge it; keep still-true facts and drop stale ones):\n" + session.Summary,
		})
	}
	summaryMessages = append(summaryMessages, transcript.PruneToolResults(transcript.BuildConversation("", "", plan.Region), 0)...)
	summaryMessages = append(summaryMessages, agent.Message{Role: "user", Content: compactionInstruction})

	summaryBudget := compactionMaxOutputTokens
	if config.MaxOutputTokens > 0 && config.MaxOutputTokens < summaryBudget {
		summaryBudget = config.MaxOutputTokens
	}
	if summaryBudget < dataquality.MinAgentModelOutputTokens {
		e.recordEngine(ctx, session, wake, 0, fmt.Sprintf(
			"compaction skipped: the model output budget (%d) is below the summary floor", summaryBudget))
		return 0, false, nil
	}

	var (
		summary string
		tokens  int64
	)
	for event := range e.chat(ctx, agent.Config{
		Provider:             config,
		Feature:              compactionFeature,
		History:              summaryMessages,
		MaxTurns:             1,
		MaxTokens:            summaryBudget,
		MaxConversationBytes: agent.DefaultMaxConversationBytes,
		IncludeUsage:         true,
		Metrics:              e.metrics,
	}) {
		switch event.Type {
		case agent.EventAssistant:
			if event.Assistant != nil && strings.TrimSpace(event.Assistant.Content) != "" {
				summary = event.Assistant.Content
			}
		case agent.EventUsage:
			if event.Usage != nil {
				tokens += event.Usage.TotalTokens
				if event.Usage.TotalTokens == 0 {
					tokens += event.Usage.PromptTokens + event.Usage.CompletionTokens
				}
			}
		case agent.EventError:
			return tokens, false, errors.Wrap(event.Error, "the context compaction failed")
		default:
		}
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return tokens, false, errors.New("the context compaction produced no summary")
	}
	// A summary larger than the span it replaces is not progress; keep the original.
	if transcript.EstimateTextTokens(summary) >= transcript.EstimateTokens(transcript.BuildConversation("", "", plan.Region)) {
		return tokens, false, nil
	}
	_, appendErr := e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           wake,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_COMPACTION,
		Content:        summary,
		Tokens:         clampInt32(tokens),
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

// pruneToolResults replaces the middle of old tool results with a marker.
func pruneToolResults(messages []agent.Message, keepRecent int) []agent.Message {
	return transcript.PruneToolResults(messages, keepRecent)
}
