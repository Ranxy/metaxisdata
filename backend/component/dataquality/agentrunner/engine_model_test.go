package agentrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// scriptedChat drives the model loop deterministically: it admits one turn, runs the
// given tool calls through the configured executor, reports usage and ends.
type scriptedChat struct {
	calls  []scriptedCall
	tokens int64
}

type scriptedCall struct {
	name string
	args string
}

func (s *scriptedChat) run(_ context.Context, cfg llm.AgentConfig) <-chan llm.AgentEvent {
	events := make(chan llm.AgentEvent, 8)
	go func() {
		defer close(events)
		events <- llm.AgentEvent{Type: llm.AgentEventTurnStart}
		for index, call := range s.calls {
			toolCall := llm.ToolCall{ID: string(rune('a' + index))}
			toolCall.Function.Name = call.name
			toolCall.Function.Arguments = call.args
			events <- llm.AgentEvent{Type: llm.AgentEventToolStart, ToolCall: &toolCall}
			results, err := cfg.Executor(toolCall)
			if err != nil {
				events <- llm.AgentEvent{Type: llm.AgentEventError, Error: err}
				return
			}
			content := ""
			if len(results) > 0 {
				content = results[0].Content
			}
			events <- llm.AgentEvent{Type: llm.AgentEventToolEnd, ToolCall: &toolCall, ToolResult: content}
		}
		if s.tokens > 0 {
			events <- llm.AgentEvent{Type: llm.AgentEventUsage, Usage: &llm.Usage{TotalTokens: s.tokens}}
		}
		events <- llm.AgentEvent{Type: llm.AgentEventAgentEnd}
	}()
	return events
}

func modelEngine(store Store, chat Chat) *Engine {
	return NewModelEngine(store, chat, func(context.Context) (llm.ResolvedConfig, bool, error) {
		return llm.ResolvedConfig{}, true, nil
	})
}

func modelGoal() *store.QualityAgentGoalMessage {
	return &store.QualityAgentGoalMessage{
		ID: 1, ResourceID: "g1", WorkspaceID: "ws1", Kind: storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_CUSTOM,
		TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE, Objective: "understand the drop",
		RoundsStarted: 0, MaxRounds: 8, AgentPrincipalID: 42,
	}
}

// A model round admits itself once, runs the tools and completes.
func TestModelTurnRecordsAndCompletes(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{}
	chat := &scriptedChat{tokens: 1234, calls: []scriptedCall{
		{name: "get_target_summary", args: "{}"},
		{name: "record_investigation", args: `{"summary":"the upstream batch was short"}`},
		{name: "complete_goal", args: `{"summary":"the drop is explained"}`},
	}}
	engine := modelEngine(fake, chat.run)
	outcome, err := engine.Run(context.Background(), modelGoal())
	require.NoError(t, err)
	require.True(t, outcome.Admitted)
	require.Equal(t, int64(1234), outcome.TokensUsed)
	require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_COMPLETE, outcome.Phase)
	require.Len(t, fake.investigations, 1)
	require.Equal(t, "the upstream batch was short", fake.investigations[0].Summary)
	require.Equal(t, int32(42), *fake.investigations[0].CreatedBy)

	kinds := map[storepb.QualityAgentGoalStepKind]int{}
	for _, step := range fake.steps {
		kinds[step.Kind]++
	}
	require.Equal(t, 1, kinds[storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_TURN_ADMITTED])
	require.Equal(t, 3, kinds[storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_TOOL_CALL])
	require.Equal(t, 1, kinds[storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_EFFECT_INTENT])
	require.Equal(t, 1, kinds[storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_EFFECT_RESULT])
}

// The same blocker must persist for three consecutive rounds before the goal parks.
func TestModelBlockedThreshold(t *testing.T) {
	t.Parallel()

	goal := modelGoal()
	goal.Kind = storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_MAINTAIN_TARGET
	goal.Checkpoint = &storepb.QualityAgentGoalCheckpoint{}
	chat := &scriptedChat{calls: []scriptedCall{
		{name: "block_goal", args: `{"code":"missing-llm-data","reason":"the profile is not configured"}`},
	}}
	engine := modelEngine(&fakeStore{}, chat.run)
	for round := 1; round <= 2; round++ {
		outcome, err := engine.Run(context.Background(), goal)
		require.NoError(t, err)
		require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE, outcome.Phase)
		goal.Checkpoint = outcome.Checkpoint
	}
	third, err := engine.Run(context.Background(), goal)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED, third.Phase)
	require.Equal(t, "model-reported", third.BlockedCode)
}

// A round that never reaches a model turn is not admitted, so it cannot consume the
// round cap.
func TestModelTurnNotAdmitted(t *testing.T) {
	t.Parallel()

	chat := Chat(func(context.Context, llm.AgentConfig) <-chan llm.AgentEvent {
		events := make(chan llm.AgentEvent, 1)
		close(events)
		return events
	})
	engine := NewModelEngine(&fakeStore{}, chat, func(context.Context) (llm.ResolvedConfig, bool, error) {
		return llm.ResolvedConfig{}, true, nil
	})
	outcome, err := engine.Run(context.Background(), modelGoal())
	require.NoError(t, err)
	require.False(t, outcome.Admitted)
}
