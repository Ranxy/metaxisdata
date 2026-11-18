package qualityagentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func reportConfig(frequency store.QualityAgentReportFrequency, timezone, clock string, weekday int16) *store.QualityAgentMessage {
	return &store.QualityAgentMessage{
		ReportFrequency: frequency,
		ReportTimezone:  timezone,
		ReportTime:      clock,
		ReportWeekday:   weekday,
	}
}

func TestDueReportDailyKeepsLocalWallClock(t *testing.T) {
	t.Parallel()

	agent := reportConfig(store.QualityAgentReportFrequencyDaily, "America/New_York", "09:00", 0)
	// The first Sunday after the US fall-back: the period start must stay 09:00
	// local, not 10:00 local, although the window is 25 absolute hours.
	now := time.Date(2026, time.November, 1, 14, 0, 0, 0, time.UTC)
	schedule, ok, err := dueReport(agent, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "2026-11-01", schedule.periodKey)
	require.Equal(t, "2026-11-01 09:00:00 -0500 EST", schedule.periodEnd.Format("2006-01-02 15:04:05 -0700 MST"))
	require.Equal(t, "2026-10-31 09:00:00 -0400 EDT", schedule.periodStart.Format("2006-01-02 15:04:05 -0700 MST"))
}

func TestDueReportWeeklyUsesTheConfiguredWeekday(t *testing.T) {
	t.Parallel()

	// 1 is Monday in the store enum; a Monday 10:00 local wake reports the Monday
	// 09:00 slot of the same day, not Sunday.
	agent := reportConfig(store.QualityAgentReportFrequencyWeekly, "UTC", "09:00", 1)
	now := time.Date(2026, time.November, 2, 10, 0, 0, 0, time.UTC)
	schedule, ok, err := dueReport(agent, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, time.Monday, schedule.periodEnd.Weekday())
	require.Equal(t, "2026-W45", schedule.periodKey)
	require.Equal(t, schedule.periodEnd.AddDate(0, 0, -7), schedule.periodStart)

	// Sunday (7) maps to Sunday rather than Saturday.
	agent.ReportWeekday = 7
	schedule, ok, err = dueReport(agent, now)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, time.Sunday, schedule.periodEnd.Weekday())
}

// gatingStore records whether the runner read the agents after the switch.
type gatingStore struct {
	agents []*store.QualityAgentMessage
	listed bool
}

func (*gatingStore) GetWorkspaceID(context.Context) (string, error) { return "ws", nil }

func (s *gatingStore) ListQualityAgents(context.Context) ([]*store.QualityAgentMessage, error) {
	s.listed = true
	return s.agents, nil
}

func (*gatingStore) ListQualityAgentDatabasesByAgent(context.Context) (map[int64][]string, error) {
	return nil, nil
}

func (*gatingStore) GetQualityAgentReportByPeriod(context.Context, int64, string) (*store.QualityAgentReportMessage, error) {
	return nil, nil
}

func (*gatingStore) CreateQualityAgentReport(context.Context, *store.QualityAgentReportMessage) (*store.QualityAgentReportMessage, bool, error) {
	return nil, false, nil
}

func (*gatingStore) BuildQualityAgentReportSnapshot(context.Context, int64, []string, time.Time, time.Time) (*store.QualityAgentReportSnapshot, error) {
	return nil, nil
}

func (*gatingStore) EnsureQualityAgentConversation(context.Context, string, int64) (*store.QualityAgentSessionMessage, error) {
	return nil, nil
}

func (*gatingStore) AppendQualityAgentTranscriptMessage(context.Context, *store.QualityAgentTranscriptMessage) (*store.QualityAgentTranscriptMessage, error) {
	return nil, nil
}

func (*gatingStore) AdvanceQualityAgentSessionWake(context.Context, string, time.Time) (bool, error) {
	return true, nil
}

func (*gatingStore) GetUserByID(context.Context, int) (*store.UserMessage, error) { return nil, nil }

func (*gatingStore) CreateQualityAgentAttention(context.Context, *store.QualityAgentAttentionMessage) (*store.QualityAgentAttentionMessage, bool, error) {
	return nil, false, nil
}

// The report runner is gated by the workspace runner switch, like the session,
// enrollment and event paths.
func TestRunOnceRespectsTheRunnerSwitch(t *testing.T) {
	t.Parallel()

	stores := &gatingStore{agents: []*store.QualityAgentMessage{{
		ID: 7, State: store.QualityAgentStateActive, Capabilities: []string{"patrol"},
	}}}
	runner := &Runner{
		store:        stores,
		loadSettings: func(context.Context) (dataquality.Settings, error) { return dataquality.Settings{}, nil },
		now:          func() time.Time { return time.Unix(10_000, 0).UTC() },
		tick:         time.Second,
	}
	runner.runOnce(context.Background())
	require.False(t, stores.listed, "a disabled runner reports nothing")

	runner.loadSettings = func(context.Context) (dataquality.Settings, error) {
		return dataquality.Settings{AgentRunnerEnabled: true}, nil
	}
	runner.runOnce(context.Background())
	require.True(t, stores.listed)
}
