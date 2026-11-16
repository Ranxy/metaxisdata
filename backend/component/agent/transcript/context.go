package transcript

import (
	"unicode/utf8"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
)

// The prune budget keeps the useful ends of an old tool result and drops the
// middle. It is a byte budget, so a CJK result is not over-counted.
const (
	pruneHead   = 2048
	pruneTail   = 512
	pruneMarker = "\n\n[... tool result pruned ...]\n\n"
)

// checkpointPreamble frames a compaction checkpoint as established background, so
// the model continues from the messages after it instead of acknowledging it.
const checkpointPreamble = "This is an automatically generated checkpoint condensing an " +
	"earlier span of this conversation to free up context. Treat the captured context as " +
	"established background and build on it without restating it. Continue directly from " +
	"the messages that follow."

// BuildConversation rebuilds the model input: the live system prompt, the rolling
// compaction checkpoint (which replaces the entries it covers), then every entry
// except engine notes and checkpoint markers. The result is repaired, so the
// provider always receives a legal tool-call sequence.
func BuildConversation(system, checkpoint string, entries []Entry) []agent.Message {
	conversation := make([]agent.Message, 0, len(entries)+2)
	if system != "" {
		conversation = append(conversation, agent.Message{Role: "system", Content: system})
	}
	if checkpoint != "" {
		conversation = append(conversation, agent.Message{
			Role:    "user",
			Content: checkpointPreamble + "\n\n<compacted-summary>\n" + checkpoint + "\n</compacted-summary>",
		})
	}
	for _, entry := range entries {
		switch entry.Role {
		case RoleUser:
			conversation = append(conversation, agent.Message{Role: "user", Content: entry.Content})
		case RoleAssistant:
			conversation = append(conversation, agent.Message{
				Role:      "assistant",
				Content:   entry.Content,
				ToolCalls: entry.ToolCalls,
			})
		case RoleTool:
			conversation = append(conversation, agent.Message{
				Role:       "toolResult",
				ToolCallID: entry.ToolCallID,
				ToolName:   entry.ToolName,
				Content:    entry.Content,
			})
		case RoleSystem, RoleEngine, RoleCompaction:
			// The live system prompt is prepended above; engine notes and checkpoint
			// markers are for the reader, not the model.
		default:
		}
	}
	return RepairToolCalls(conversation)
}

// RepairToolCalls inserts a synthetic result for every assistant tool call that has
// no stored result, and drops a result whose call is gone. A provider rejects a
// conversation with an unanswered call, and a crash between the assistant message
// and the tool execution leaves exactly that shape.
func RepairToolCalls(messages []agent.Message) []agent.Message {
	repaired := make([]agent.Message, 0, len(messages))
	pending := make([]string, 0, 2)
	flush := func() {
		for _, id := range pending {
			repaired = append(repaired, agent.Message{
				Role:       "toolResult",
				ToolCallID: id,
				Content:    "the server restarted before this tool ran; its result is unknown",
			})
		}
		pending = pending[:0]
	}
	for _, message := range messages {
		switch message.Role {
		case "assistant":
			flush()
			repaired = append(repaired, message)
			for _, call := range message.ToolCalls {
				pending = append(pending, call.ID)
			}
		case "toolResult":
			matched := false
			for index, id := range pending {
				if id == message.ToolCallID {
					pending = append(pending[:index], pending[index+1:]...)
					matched = true
					break
				}
			}
			if matched {
				repaired = append(repaired, message)
			}
		default:
			flush()
			repaired = append(repaired, message)
		}
	}
	flush()
	return repaired
}

// EstimateTokens approximates a conversation's token size. A provider's real
// prompt_tokens is always preferred when a wake reported one; this estimate sizes
// messages added since, leaning high so a Chinese-heavy conversation is not
// undercounted.
func EstimateTokens(messages []agent.Message) int {
	total := 0
	for _, message := range messages {
		total += EstimateTextTokens(message.Content)
		total += EstimateTextTokens(message.ToolCallID)
		total += EstimateTextTokens(message.ToolName)
		for _, call := range message.ToolCalls {
			total += EstimateTextTokens(call.Function.Name)
			total += EstimateTextTokens(call.Function.Arguments)
		}
	}
	return total
}

// EstimateTextTokens approximates one string's token count from both its byte and
// its rune length and keeps the larger.
func EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	byBytes := (len(text) + 3) / 4
	byRunes := (utf8.RuneCountInString(text)*3 + 3) / 4
	if byBytes > byRunes {
		return byBytes
	}
	return byRunes
}

// MessageTokens estimates one entry's tokens, tool calls included.
func MessageTokens(entry Entry) int {
	total := EstimateTextTokens(entry.Content) + EstimateTextTokens(entry.ToolCallID) +
		EstimateTextTokens(entry.ToolName)
	for _, call := range entry.ToolCalls {
		total += EstimateTextTokens(call.Function.Name) + EstimateTextTokens(call.Function.Arguments)
	}
	return total
}

// PruneToolResults replaces the middle of old tool results with a marker, keeping
// the head and tail. It runs before a summary call and, with a recent window, on
// the normal model context so an old oversized result stops being resent in full
// on every turn.
func PruneToolResults(messages []agent.Message, keepRecent int) []agent.Message {
	limit := len(messages) - keepRecent
	for index := 0; index < limit && index < len(messages); index++ {
		message := &messages[index]
		if message.Role != "toolResult" {
			continue
		}
		if len(message.Content) <= pruneHead+pruneTail+len(pruneMarker) {
			continue
		}
		message.Content = truncateHead(message.Content, pruneHead) + pruneMarker + lastBytes(message.Content, pruneTail)
	}
	return messages
}

// truncateHead returns at most limit bytes from the start of value.
func truncateHead(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

// lastBytes returns at most limit bytes from the end of value, never splitting a
// rune.
func lastBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}
