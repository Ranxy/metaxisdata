package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

func TestConvertToLlmMapsEveryRole(t *testing.T) {
	t.Parallel()

	messages := ConvertToLlm([]Message{
		{Role: "system", Content: "you are a SQL expert"},
		{Role: "user", Content: "explain this query"},
		{Role: "assistant", Content: "let me look at the table first"},
		{Role: "toolResult", Content: "users(id, name)", ToolCallID: "call-1", ToolName: "get_object_schema"},
	})

	require.Equal(t, []llm.Message{
		{Role: "system", Content: "you are a SQL expert"},
		{Role: "user", Content: "explain this query"},
		{Role: "assistant", Content: "let me look at the table first"},
		{Role: "tool", Content: "users(id, name)", ToolCallID: "call-1"},
	}, messages)
}

func TestConvertToLlmKeepsAssistantTextAlongsideToolCalls(t *testing.T) {
	t.Parallel()

	toolCall := llm.ToolCall{ID: "call-1", Type: "function"}
	toolCall.Function.Name = "search_objects"
	toolCall.Function.Arguments = `{"keyword":"orders"}`

	messages := ConvertToLlm([]Message{
		{Role: "assistant", Content: "I need to find the orders table", ToolCalls: []llm.ToolCall{toolCall}},
	})

	require.Len(t, messages, 1)
	require.Equal(t, "assistant", messages[0].Role)
	// Dropping the text here would hide the model's own reasoning from the next
	// turn, which is how this regression was introduced.
	require.Equal(t, "I need to find the orders table", messages[0].Content)
	require.Equal(t, []llm.ToolCall{toolCall}, messages[0].ToolCalls)
}

func TestConvertToLlmDropsUnknownRoles(t *testing.T) {
	t.Parallel()

	messages := ConvertToLlm([]Message{
		{Role: "system", Content: "s"},
		{Role: "unknown", Content: "dropped"},
		{Role: "", Content: "dropped"},
	})

	require.Len(t, messages, 1)
	require.Equal(t, "system", messages[0].Role)
}

// Reasoning is display-only: it must never cross the boundary into the request
// sent to the provider.
func TestConvertToLlmDropsReasoning(t *testing.T) {
	t.Parallel()

	converted := ConvertToLlm([]Message{{
		Role:      "assistant",
		Content:   "answer",
		Reasoning: "private chain of thought",
	}})
	require.Len(t, converted, 1)
	encoded, err := json.Marshal(converted)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private chain of thought")
}
