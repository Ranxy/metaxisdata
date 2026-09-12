package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertToLlmMapsEveryRole(t *testing.T) {
	t.Parallel()

	messages := ConvertToLlm([]AgentMessage{
		{Role: "system", Content: "you are a SQL expert"},
		{Role: "user", Content: "explain this query"},
		{Role: "assistant", Content: "let me look at the table first"},
		{Role: "toolResult", Content: "users(id, name)", ToolCallID: "call-1", ToolName: "get_object_schema"},
	})

	require.Equal(t, []Message{
		{Role: "system", Content: "you are a SQL expert"},
		{Role: "user", Content: "explain this query"},
		{Role: "assistant", Content: "let me look at the table first"},
		{Role: "tool", Content: "users(id, name)", ToolCallID: "call-1"},
	}, messages)
}

func TestConvertToLlmKeepsAssistantTextAlongsideToolCalls(t *testing.T) {
	t.Parallel()

	toolCall := ToolCall{ID: "call-1", Type: "function"}
	toolCall.Function.Name = "search_objects"
	toolCall.Function.Arguments = `{"keyword":"orders"}`

	messages := ConvertToLlm([]AgentMessage{
		{Role: "assistant", Content: "I need to find the orders table", ToolCalls: []ToolCall{toolCall}},
	})

	require.Len(t, messages, 1)
	require.Equal(t, "assistant", messages[0].Role)
	// Dropping the text here would hide the model's own reasoning from the next
	// turn, which is how this regression was introduced.
	require.Equal(t, "I need to find the orders table", messages[0].Content)
	require.Equal(t, []ToolCall{toolCall}, messages[0].ToolCalls)
}

func TestConvertToLlmDropsUnknownRoles(t *testing.T) {
	t.Parallel()

	messages := ConvertToLlm([]AgentMessage{
		{Role: "system", Content: "s"},
		{Role: "unknown", Content: "dropped"},
		{Role: "", Content: "dropped"},
	})

	require.Len(t, messages, 1)
	require.Equal(t, "system", messages[0].Role)
}

func TestConvertToLlmReturnsEmptySliceForNoMessages(t *testing.T) {
	t.Parallel()

	messages := ConvertToLlm(nil)
	require.NotNil(t, messages)
	require.Empty(t, messages)
}
