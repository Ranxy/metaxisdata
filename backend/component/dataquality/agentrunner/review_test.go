package agentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func reviewSession() *store.QualityAgentSessionMessage {
	reviewed := time.Now().UTC().Add(-time.Hour)
	return &store.QualityAgentSessionMessage{
		ID:             12,
		ResourceID:     "subagent-12",
		AgentID:        1,
		TargetGUID:     "1;shop;;orders",
		Role:           storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT,
		Mode:           storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR,
		CreatedAt:      reviewed,
		LastReviewedAt: &reviewed,
	}
}

func TestNeedsModelIsFalseForAHealthyTarget(t *testing.T) {
	t.Parallel()

	engine := NewEngine(newFakeStore())
	needs, err := engine.NeedsModel(context.Background(), reviewSession())
	require.NoError(t, err)
	require.False(t, needs, "a healthy target must not spend a model call on its daily review")
}

func TestNeedsModelIsTrueForActionableEvidence(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.actionable = true
	engine := NewEngine(stores)
	needs, err := engine.NeedsModel(context.Background(), reviewSession())
	require.NoError(t, err)
	require.True(t, needs)
}

func TestNeedsModelIsTrueForAPendingAgentMessage(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	session := reviewSession()
	stores.messages = []*store.QualityAgentTranscriptMessage{{
		SessionID: session.ID,
		Seq:       1,
		Role:      storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:    storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:   "Agent lead sent a message: investigate",
	}}
	engine := NewEngine(stores)
	needs, err := engine.NeedsModel(context.Background(), session)
	require.NoError(t, err)
	require.True(t, needs)
}

func TestReviewIsDeterministicAndWritesNothing(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewEngine(stores)
	session := reviewSession()
	outcome := engine.Review(context.Background(), session)
	require.NotNil(t, outcome.NextWakeAt)
	require.WithinDuration(t, time.Now().Add(maintainWake), *outcome.NextWakeAt, time.Minute)
	require.Empty(t, stores.messages, "a no-op review must not append a transcript row")
}

func TestNeedsModelIgnoresACaseThatIsNotDue(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	future := time.Now().UTC().Add(time.Hour)
	stores.openCases = []*store.QualityAgentCaseMessage{{NextActionAt: &future}}
	engine := NewEngine(stores)
	needs, err := engine.NeedsModel(context.Background(), reviewSession())
	require.NoError(t, err)
	require.False(t, needs)
}

func TestNeedsModelSkipsTriageUntilItsReviewIsDue(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	review := time.Now().UTC().Add(24 * time.Hour)
	stores.enrollment = &store.QualityAgentEnrollmentMessage{NextReviewAt: &review}
	session := reviewSession()
	session.Mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE
	engine := NewEngine(stores)
	needs, err := engine.NeedsModel(context.Background(), session)
	require.NoError(t, err)
	require.False(t, needs, "a triaged target waits for next_review_at")
}
