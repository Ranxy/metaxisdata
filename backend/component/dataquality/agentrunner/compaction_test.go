package agentrunner

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func fillerMessages(count int) []*store.QualityAgentTranscriptMessage {
	messages := make([]*store.QualityAgentTranscriptMessage, 0, count)
	for index := 0; index < count; index++ {
		role := storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER
		if index%2 == 1 {
			role = storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT
		}
		messages = append(messages, &store.QualityAgentTranscriptMessage{
			Seq:     int64(index + 1),
			Role:    role,
			Content: strings.Repeat("a data quality observation. ", 200),
		})
	}
	return messages
}

// Over the trigger, the oldest region is summarized into one checkpoint message and the
// session's watermark moves to the last summarized message.
func TestMaybeCompactSummarizesTheOldRegion(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "## Session Objective and Target\n- target-1\n\n## Next Step\n- (none)"},
	}), resolveToModel, nil)
	session := testSession()
	messages := fillerMessages(12)

	tokens, compacted, err := engine.maybeCompact(context.Background(), session, 3, nil, llm.ResolvedConfig{ModelName: "test-model"}, messages, 2_000, false)
	require.NoError(t, err)
	require.True(t, compacted)
	require.Positive(t, tokens)
	require.NotEmpty(t, session.Summary)
	require.Greater(t, session.SummaryThroughSeq, int64(0))
	require.Less(t, session.SummaryThroughSeq, int64(12))

	var compaction *store.QualityAgentTranscriptMessage
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION {
			compaction = message
		}
	}
	require.NotNil(t, compaction)
	require.Equal(t, session.Summary, compaction.Content)
}

// Under the trigger nothing is summarized and no model call is made.
func TestMaybeCompactSkipsUnderTheTrigger(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "unused"}}), resolveToModel, nil)
	session := testSession()

	_, compacted, err := engine.maybeCompact(context.Background(), session, 1, nil, llm.ResolvedConfig{ModelName: "test-model"}, fillerMessages(2), 10_000, false)
	require.NoError(t, err)
	require.False(t, compacted)
	require.Empty(t, session.Summary)
	require.Empty(t, stores.messages)
}

// A summary that does not shrink the span it replaces is rejected: the original
// messages stay the model's context instead of being replaced by a larger checkpoint.
func TestMaybeCompactRejectsANonShrinkingSummary(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: strings.Repeat("a very long summary that is bigger than the region. ", 5000)},
	}), resolveToModel, nil)
	session := testSession()
	_, compacted, err := engine.maybeCompact(context.Background(), session, 1, nil,
		llm.ResolvedConfig{ModelName: "test-model"}, fillerMessages(6), 100, false)
	require.NoError(t, err)
	require.False(t, compacted)
	require.Empty(t, session.Summary)
	for _, message := range stores.messages {
		require.NotEqual(t, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION, message.Role)
	}
}

// A forced compaction after a provider overflow ignores the trigger and compacts anyway.
func TestMaybeCompactForcedIgnoresTheTrigger(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "## Next Step\n- (none)"},
	}), resolveToModel, nil)
	session := testSession()
	_, compacted, err := engine.maybeCompact(context.Background(), session, 1, nil,
		llm.ResolvedConfig{ModelName: "test-model"}, fillerMessages(12), 1_000_000, true)
	require.NoError(t, err)
	require.True(t, compacted)
	require.NotEmpty(t, session.Summary)
}

// A zero trigger derives the pressure threshold from the pinned model's
// capacities instead of the retired package constant.
func TestMaybeCompactDerivesTheTriggerFromTheModel(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "## Session Objective and Target\n- target-1\n\n## Next Step\n- (none)"},
	}), resolveToModel, nil)
	session := testSession()
	messages := fillerMessages(12)

	config := llm.ResolvedConfig{ModelName: "test-model", ContextWindow: 12000, MaxOutputTokens: 4096}
	_, compacted, err := engine.maybeCompact(context.Background(), session, 1, nil, config, messages, 0, false)
	require.NoError(t, err)
	require.True(t, compacted)
}

// A stale model pair whose normal pressure budget is zero still gets a legal cut: the
// emergency trigger leaves a quarter of the message room as the verbatim tail.
func TestEmergencyCompactionTriggerLeavesAQuarterOfTheMessageRoom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config llm.ResolvedConfig
		want   int
	}{
		{name: "no message room", config: llm.ResolvedConfig{ContextWindow: 1000, MaxOutputTokens: 2000}, want: 0},
		{name: "room large enough for the floor", config: llm.ResolvedConfig{ContextWindow: 20000, MaxOutputTokens: 1000}, want: 4750},
		{name: "room smaller than the floor", config: llm.ResolvedConfig{ContextWindow: 10000, MaxOutputTokens: 1000}, want: 4096},
		{name: "room smaller than the floor is clamped", config: llm.ResolvedConfig{ContextWindow: 3000, MaxOutputTokens: 1000}, want: 2000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, emergencyCompactionTrigger(test.config))
		})
	}
}

// A compaction the engine refuses explains itself in a runner note instead of leaving
// the session silently over budget.
func TestMaybeCompactExplainsWhyItSkipped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  llm.ResolvedConfig
		trigger int
		want    string
	}{
		{
			name:   "the model capacities leave no pressure budget",
			config: llm.ResolvedConfig{ModelName: "test-model", ContextWindow: 32768, MaxOutputTokens: 32767},
			want:   "compaction skipped",
		},
		{
			name:    "the summary budget is below the output floor",
			config:  llm.ResolvedConfig{ModelName: "test-model", ContextWindow: 131072, MaxOutputTokens: 1000},
			trigger: 2_000,
			want:    "below the summary floor",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stores := newFakeStore()
			engine := NewModelEngine(stores, scriptedChat(nil), resolveToModel, nil)
			_, compacted, err := engine.maybeCompact(context.Background(), testSession(), 1, nil,
				test.config, fillerMessages(12), test.trigger, false)
			require.NoError(t, err)
			require.False(t, compacted)
			require.Contains(t, engineContents(stores), test.want)
		})
	}
}

// engineContents joins the runner notes a fake store recorded so a test can
// assert that a skipped path explained itself.
func engineContents(stores *fakeStore) string {
	var contents []string
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE {
			contents = append(contents, message.Content)
		}
	}
	return strings.Join(contents, "\n")
}
