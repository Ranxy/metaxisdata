package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/schedule"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityTargetScheduleRowStub produces a quality_target_schedule row for
// scanQualityTargetSchedule without a database. The destination order is
// qualityTargetScheduleColumns.
type qualityTargetScheduleRowStub struct {
	targetGUID      string
	targetType      storepb.MetaType
	intervalSeconds int32
	cron            string
	timeZone        string
	nextRunAt       sql.NullTime
	createdAt       time.Time
	updatedAt       time.Time
}

func (s qualityTargetScheduleRowStub) Scan(dest ...any) error {
	if len(dest) != 8 {
		return fmt.Errorf("quality target schedule stub got %d destinations, want 8", len(dest))
	}
	*(dest[0].(*string)) = s.targetGUID
	*(dest[1].(*int16)) = int16(s.targetType)
	*(dest[2].(*int32)) = s.intervalSeconds
	*(dest[3].(*string)) = s.cron
	*(dest[4].(*string)) = s.timeZone
	*(dest[5].(*sql.NullTime)) = s.nextRunAt
	*(dest[6].(*time.Time)) = s.createdAt
	*(dest[7].(*time.Time)) = s.updatedAt
	return nil
}

// The schedule row maps whole seconds to a duration and keeps a missing next run
// nil: a paused target has an interval but is not scheduled.
func TestScanQualityTargetSchedule(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 26, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	nextRunAt := updatedAt.Add(2 * time.Hour)

	msg, err := scanQualityTargetSchedule(qualityTargetScheduleRowStub{
		targetGUID:      "instance-1;shop;public;orders",
		targetType:      storepb.MetaType_TABLE,
		intervalSeconds: 7200,
		nextRunAt:       sql.NullTime{Time: nextRunAt, Valid: true},
		createdAt:       createdAt,
		updatedAt:       updatedAt,
	})
	require.NoError(t, err)
	require.Equal(t, "instance-1;shop;public;orders", msg.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, msg.TargetType)
	require.Equal(t, 2*time.Hour, msg.ScheduleInterval)
	require.NotNil(t, msg.NextRunAt)
	require.Equal(t, nextRunAt, *msg.NextRunAt)
	require.Equal(t, createdAt, msg.CreatedAt)
	require.Equal(t, updatedAt, msg.UpdatedAt)

	paused, err := scanQualityTargetSchedule(qualityTargetScheduleRowStub{
		targetGUID:      "instance-1;shop;public;orders",
		targetType:      storepb.MetaType_TABLE,
		intervalSeconds: 3600,
		createdAt:       createdAt,
		updatedAt:       updatedAt,
	})
	require.NoError(t, err)
	require.Equal(t, time.Hour, paused.ScheduleInterval)
	require.Nil(t, paused.NextRunAt, "a target with no enabled check has no next run")

	// A cron target stores its nominal cadence in the interval column and keeps the
	// expression plus the resolved zone beside it.
	cron, err := scanQualityTargetSchedule(qualityTargetScheduleRowStub{
		targetGUID:      "instance-1;shop;public;orders",
		targetType:      storepb.MetaType_TABLE,
		intervalSeconds: 86400,
		cron:            "0 2 * * *",
		timeZone:        "Asia/Shanghai",
		nextRunAt:       sql.NullTime{Time: nextRunAt, Valid: true},
		createdAt:       createdAt,
		updatedAt:       updatedAt,
	})
	require.NoError(t, err)
	require.Equal(t, schedule.Spec{Interval: 24 * time.Hour, Cron: "0 2 * * *", TimeZone: "Asia/Shanghai"}, cron.Spec())
}

// The frequency gate is one predicate shared by the due-rules read and the
// pending-count read: if the two drifted apart, a tiered check that the due read
// skipped would keep the target's cycle pending forever.
func TestQualityFrequencyGate(t *testing.T) {
	t.Parallel()

	require.Equal(t, time.Hour, qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_HOURLY))
	require.Equal(t, 24*time.Hour, qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_DAILY))
	require.Equal(t, 7*24*time.Hour, qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_WEEKLY))
	require.Equal(t, time.Duration(0), qualityFrequencyTierPeriod(storepb.QualityFrequencyTier_QUALITY_FREQUENCY_TIER_UNSPECIFIED))

	gate := qualityFrequencyGateSQL(4, 5)
	require.Contains(t, gate, "gate.started_at > s.next_run_at - MAKE_INTERVAL(secs => GREATEST(0,")
	// The tier is quantized to the target's own cycle: a run's started_at follows its
	// cycle's due instant, so a plain "period - cycle" window would skip every other
	// cycle for a tier equal to the cadence.
	require.Contains(t, gate, "CEIL(")
	require.Contains(t, gate, "* s.schedule_interval_seconds")
	require.Contains(t, gate, "gate.trigger = $5")
	require.Contains(t, gate, "gate.state = $4")
	require.Contains(t, dueQualityTargetRulesStatement, qualityFrequencyGateSQL(4, 5),
		"the due-rules read must apply the frequency gate")
	require.Contains(t, pendingQualityTargetRulesStatement, qualityFrequencyGateSQL(5, 6),
		"the pending count must apply the same frequency gate")
}

// The due scan's read work is bounded by SQL, not only by the Go budget: after a
// backlog the store must not aggregate every due target, and a target the user
// postponed between the two statements must not be executed.
func TestDueQualityTargetStatementsBoundTheWork(t *testing.T) {
	t.Parallel()

	require.Contains(t, dueQualityTargetsStatement, "LIMIT $3",
		"the due-target query must not read every due target")
	require.Contains(t, dueQualityTargetsStatement, "ORDER BY s.next_run_at ASC",
		"LIMIT is only a prefix of the documented first-due order")
	require.Contains(t, dueQualityTargetRulesStatement, "s.next_run_at <= $7",
		"the rule query must re-check that the target is still due")
	require.Contains(t, dueQualityTargetRulesStatement, "e.started_at >= s.next_run_at",
		"attempts belong to the target's current cycle")
}

// A cron target stores the expression's nominal gap in the interval column: it is
// the cadence the target falls back to when the expression is cleared, and the
// target cycle a frequency tier counts. An interval target keeps its own value.
func TestNormalizedQualitySchedule(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 1, 8, 30, 0, 0, time.UTC)

	interval := normalizedQualitySchedule(schedule.Spec{Interval: 90 * time.Minute}, now)
	require.Equal(t, schedule.Spec{Interval: 90 * time.Minute}, interval)

	daily := normalizedQualitySchedule(schedule.Spec{Cron: "0 2 * * *", TimeZone: "UTC"}, now)
	require.Equal(t, "0 2 * * *", daily.Cron)
	require.Equal(t, 24*time.Hour, daily.Interval)

	weekly := normalizedQualitySchedule(schedule.Spec{Cron: "0 2 * * 1", TimeZone: "UTC"}, now)
	require.Equal(t, 7*24*time.Hour, weekly.Interval)

	// A schedule that fires less often than the longest interval clamps the
	// fallback cadence: the column keeps the accepted range.
	yearly := normalizedQualitySchedule(schedule.Spec{Cron: "0 3 1 1 *", TimeZone: "UTC"}, now)
	require.Equal(t, schedule.MaxInterval, yearly.Interval)
}
