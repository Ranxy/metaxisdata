package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A target-level attention renders the target and its session, so the owner can
// tell six proposal rows apart and reply into the right work session.
func TestConvertQualityAgentAttentionRendersTargetAndSession(t *testing.T) {
	t.Parallel()

	created := time.Unix(10_000, 0).UTC()
	session := &store.QualityAgentSessionMessage{
		ID:         7,
		ResourceID: "session-1",
		TargetGUID: common.BuildMetaGUID("inst", "sales", "public", "orders"),
		TargetType: storepb.MetaType_TABLE,
	}
	attention := &store.QualityAgentAttentionMessage{
		ResourceID: "attention-1",
		Title:      "A draft is waiting",
		Question:   "Five drafts need a decision",
		CreatedAt:  created,
	}
	view := convertQualityAgentAttentionToV1("ws", "agent-1", session, []string{"price must be non-negative"}, attention)
	require.Equal(t, "workspaces/ws/qualityAgentSessions/session-1", view.GetSession())
	require.Equal(t, "orders", view.GetTarget().GetPath().GetObject())
	require.Equal(t, "sales", view.GetTarget().GetPath().GetDatabase())
	require.Equal(t, "inst", view.GetTarget().GetPath().GetInstance())
	require.Equal(t, []string{"price must be non-negative"}, view.GetPendingDraftTitles())

	agentLevel := convertQualityAgentAttentionToV1("ws", "agent-1", nil, nil, attention)
	require.Empty(t, agentLevel.GetSession())
	require.Nil(t, agentLevel.GetTarget())
}
