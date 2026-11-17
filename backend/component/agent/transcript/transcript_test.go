package transcript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

// toolCall is one assistant tool call with an id and a name.
func toolCall(id string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = "lookup"
	call.Function.Arguments = "{}"
	return call
}

func TestBuildConversationOrdersSystemCheckpointAndMessages(t *testing.T) {
	t.Parallel()

	conversation := BuildConversation("system prompt", "earlier work", []Entry{
		{Seq: 1, Role: RoleSystem, Content: "stored system copy is skipped"},
		{Seq: 2, Role: RoleUser, Content: "question"},
		{Seq: 3, Role: RoleAssistant, Content: "answer", ToolCalls: []llm.ToolCall{toolCall("call-1")}},
		{Seq: 4, Role: RoleTool, Content: "tool result", ToolCallID: "call-1", ToolName: "lookup"},
		{Seq: 5, Role: RoleEngine, Content: "runner note"},
		{Seq: 6, Role: RoleCompaction, Content: "checkpoint marker"},
	})

	require.Len(t, conversation, 5)
	require.Equal(t, "system", conversation[0].Role)
	require.Equal(t, "system prompt", conversation[0].Content)
	require.Equal(t, "user", conversation[1].Role)
	require.Contains(t, conversation[1].Content, "earlier work")
	require.Equal(t, "question", conversation[2].Content)
	require.Equal(t, "answer", conversation[3].Content)
	require.Equal(t, "toolResult", conversation[4].Role)
	require.Equal(t, "tool result", conversation[4].Content)
}

func TestRepairToolCallsClosesDanglingCallsAndDropsOrphans(t *testing.T) {
	t.Parallel()

	conversation := RepairToolCalls([]agent.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{toolCall("open-1")}},
		{Role: "user", Content: "next turn"},
		{Role: "toolResult", ToolCallID: "orphan", Content: "nobody asked"},
	})

	require.Len(t, conversation, 3)
	require.Equal(t, "toolResult", conversation[1].Role)
	require.Equal(t, "open-1", conversation[1].ToolCallID)
	require.Equal(t, "user", conversation[2].Role)
}

func TestEstimateTokensCountsTextAndToolArguments(t *testing.T) {
	t.Parallel()

	plain := EstimateTokens([]agent.Message{{Role: "user", Content: strings.Repeat("x", 400)}})
	withCall := EstimateTokens([]agent.Message{{
		Role:      "assistant",
		ToolCalls: []llm.ToolCall{toolCall("call-1")},
	}})
	require.Greater(t, plain, 0)
	require.Greater(t, withCall, 0)
	require.Zero(t, EstimateTokens(nil))
}

func TestPruneToolResultsKeepsTheHeadAndTail(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 4000) + strings.Repeat("b", 4000)
	messages := []agent.Message{
		{Role: "toolResult", Content: long},
		{Role: "toolResult", Content: "recent result"},
	}
	pruned := PruneToolResults(messages, 1)

	require.Less(t, len(pruned[0].Content), len(long))
	require.True(t, strings.HasPrefix(pruned[0].Content, "aaaa"))
	require.True(t, strings.HasSuffix(pruned[0].Content, "bbbb"))
	require.Contains(t, pruned[0].Content, "[... tool result pruned ...]")
	require.Equal(t, "recent result", pruned[1].Content, "a recent result stays intact")
}

func TestBalancedCutDoesNotSplitAToolPair(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Seq: 1, Role: RoleUser},
		{Seq: 2, Role: RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("call-1")}},
		{Seq: 3, Role: RoleTool, ToolCallID: "call-1"},
		{Seq: 4, Role: RoleUser},
	}
	require.Equal(t, 1, BalancedCut(entries, 2), "a cut between the call and its result moves earlier")
	require.Equal(t, 3, BalancedCut(entries, 3))

	// A turn boundary flushes a dangling call, so a crash-damaged tail cannot push
	// every later cut back to zero.
	damaged := []Entry{
		{Seq: 1, Role: RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("call-crash")}},
		{Seq: 2, Role: RoleUser},
		{Seq: 3, Role: RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("call-live")}},
		{Seq: 4, Role: RoleTool, ToolCallID: "call-live"},
	}
	require.Equal(t, 2, BalancedCut(damaged, 2))
	require.Equal(t, 4, BalancedCut(damaged, 4))
}

func TestPlanCompactionSummarizesTheOldestLegalRegion(t *testing.T) {
	t.Parallel()

	// Small enough to fit: nothing to plan.
	small := []Entry{{Seq: 1, Role: RoleUser, Content: "hi"}}
	_, ok := PlanCompaction(small, 1_000, false)
	require.False(t, ok)

	// A region below the minimum stays untouched.
	tiny := []Entry{
		{Seq: 1, Role: RoleUser, Content: strings.Repeat("x", 4000)},
		{Seq: 2, Role: RoleUser, Content: "new"},
	}
	_, ok = PlanCompaction(tiny, 10, true)
	require.False(t, ok)

	// A long conversation yields a contiguous region whose last sequence is the
	// checkpoint watermark.
	entries := make([]Entry, 0, 8)
	for index := 1; index <= 8; index++ {
		entries = append(entries, Entry{Seq: int64(index), Role: RoleUser, Content: strings.Repeat("x", 2000)})
	}
	entries = append(entries, Entry{Seq: 9, Role: RoleUser, Content: "tail"})
	plan, ok := PlanCompaction(entries, 500, false)
	require.True(t, ok)
	require.NotEmpty(t, plan.Region)
	require.Equal(t, plan.Region[len(plan.Region)-1].Seq, plan.ThroughSeq)
	require.Less(t, plan.ThroughSeq, int64(10))
}
