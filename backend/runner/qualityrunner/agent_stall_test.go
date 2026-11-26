package qualityrunner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// A check that stopped running produces no execution and therefore no failure event, so the sweep
// is the only way an agent learns about it. It must fire once per check per day, name the check, and
// point at the check rather than at an execution that does not exist.
func TestStalledCheckEventNamesTheCheckOnceADay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	check := &store.QualityRuleStalenessMessage{
		RuleID:         460,
		RuleResourceID: "r460",
		Title:          "performed_at must be recent",
		TargetGUID:     "inst1;app;public;orders",
		CreatedAt:      now.Add(-30 * 24 * time.Hour),
		Cadence:        time.Hour,
	}
	event, stalled := stalledCheckEvent(check, "2026-03-02", now)
	require.True(t, stalled)
	require.Contains(t, event.Content, "performed_at must be recent")
	require.Contains(t, event.Content, "never run")
	require.Equal(t, "qualityRules/r460", event.Reference)
	require.Equal(t, "stalled:460:2026-03-02", event.Key)

	// The same check on the next day is a new event: it is still not running.
	next, stalled := stalledCheckEvent(check, "2026-03-03", now)
	require.True(t, stalled)
	require.NotEqual(t, event.Key, next.Key)
}

// A check that completed inside its stall window, and one that is simply young, are both silent:
// the sweep must not page an agent about a check that is working or has not had its chance yet.
func TestStalledCheckEventIsSilentForAWorkingOrYoungCheck(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	completed := now.Add(-2 * time.Minute)
	working := &store.QualityRuleStalenessMessage{
		RuleID: 1, RuleResourceID: "r1", CreatedAt: now.Add(-24 * time.Hour),
		Cadence: time.Hour, LastCompletedAt: &completed,
	}
	_, stalled := stalledCheckEvent(working, "2026-03-02", now)
	require.False(t, stalled)

	young := &store.QualityRuleStalenessMessage{
		RuleID: 2, RuleResourceID: "r2", CreatedAt: now.Add(-time.Minute), Cadence: time.Hour,
	}
	_, stalled = stalledCheckEvent(young, "2026-03-02", now)
	require.False(t, stalled)

	_, stalled = stalledCheckEvent(nil, "2026-03-02", now)
	require.False(t, stalled)
}

// The sweep shares the scheduler's ten-second tick, so it throttles itself to a minute rather than
// reading every target's staleness six times a minute.
func TestStallSweepRunsAtMostOnceAMinute(t *testing.T) {
	t.Parallel()

	runner := &Runner{}
	start := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	require.True(t, runner.stallSweepDue(start))
	require.False(t, runner.stallSweepDue(start.Add(10*time.Second)))
	require.False(t, runner.stallSweepDue(start.Add(stallSweepInterval-time.Second)))
	require.True(t, runner.stallSweepDue(start.Add(stallSweepInterval)))
}
