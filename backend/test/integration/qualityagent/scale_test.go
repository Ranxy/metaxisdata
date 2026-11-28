//go:build integration

package qualityagent

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestQualityAgentEnrollmentQueueHandlesTenThousandTargets is the opt-in scale
// smoke for the enrollment queue: one discovery batch records ten thousand targets,
// then the unmaterialized pages walk all of them without a full-table detour. It is
// not a full runner-hour simulation; it keeps the per-batch and per-page contract
// honest without adding minutes to every suite.
func TestQualityAgentEnrollmentQueueHandlesTenThousandTargets(t *testing.T) {
	if os.Getenv("QUALITYAGENT_SCALE") == "" {
		t.Skip("set QUALITYAGENT_SCALE=1 to run the quality-agent scale smoke")
	}

	f := newDraftAgent(t)
	const targetCount = 10_000
	updates := make([]*store.QualityAgentEnrollmentUpsert, 0, targetCount)
	for index := 0; index < targetCount; index++ {
		updates = append(updates, &store.QualityAgentEnrollmentUpsert{
			AgentID:     f.agent.ID,
			TargetGUID:  fmt.Sprintf("it-scale;db;;table_%d", index),
			TargetType:  storepb.MetaType_TABLE,
			DatabaseRef: "instances/it-scale;db",
		})
	}
	started := time.Now()
	_, err := f.env.store.UpsertQualityAgentEnrollments(f.ctx, f.env.workspaceID, updates)
	require.NoError(t, err)

	seen := 0
	for offset := 0; offset < targetCount; {
		limit := 200
		page, err := f.env.store.ListQualityAgentEnrollments(f.ctx, &store.FindQualityAgentEnrollmentMessage{
			WorkspaceID:   f.env.workspaceID,
			AgentID:       f.agent.ID,
			OrderByTarget: true,
			Limit:         &limit,
			Offset:        &offset,
		})
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		seen += len(page)
		offset += len(page)
	}
	require.Equal(t, targetCount, seen)
	require.Less(t, time.Since(started), 2*time.Minute,
		"the enrollment batch and pages should stay bounded")
}
