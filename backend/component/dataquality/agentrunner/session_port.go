package agentrunner

import (
	"github.com/Ranxy/metaxisdata/backend/component/agent/transcript"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// transcriptEntry maps one stored row onto the mechanism's entry shape. The
// adapter is deliberately the only place that knows both types.
func transcriptEntry(message *store.QualityAgentTranscriptMessage) transcript.Entry {
	return transcript.Entry{
		Seq:        message.Seq,
		Role:       transcriptRole(message.Role),
		Content:    message.Content,
		ToolCalls:  toolCallsToLLM(message.ToolCalls),
		ToolCallID: message.ToolCallID,
		ToolName:   message.ToolName,
	}
}

// transcriptEntries maps a stored conversation.
func transcriptEntries(messages []*store.QualityAgentTranscriptMessage) []transcript.Entry {
	entries := make([]transcript.Entry, 0, len(messages))
	for _, message := range messages {
		entries = append(entries, transcriptEntry(message))
	}
	return entries
}

// transcriptRole reduces the stored role to the roles the conversation builder
// distinguishes. A runner-only row is an engine note.
func transcriptRole(role storepb.QualityAgentMessageRole) transcript.Role {
	switch role {
	case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER:
		return transcript.RoleUser
	case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT:
		return transcript.RoleAssistant
	case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL:
		return transcript.RoleTool
	case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM:
		return transcript.RoleSystem
	case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE:
		return transcript.RoleEngine
	case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION:
		return transcript.RoleCompaction
	default:
		return transcript.RoleEngine
	}
}
