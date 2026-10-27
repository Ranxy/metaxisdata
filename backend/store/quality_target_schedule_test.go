package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityTargetScheduleRowStub produces a quality_target_schedule row for
// scanQualityTargetSchedule without a database. The destination order is
// qualityTargetScheduleColumns.
type qualityTargetScheduleRowStub struct {
	targetGUID      string
	targetType      storepb.MetaType
	intervalSeconds int32
	nextRunAt       sql.NullTime
	createdAt       time.Time
	updatedAt       time.Time
}

func (s qualityTargetScheduleRowStub) Scan(dest ...any) error {
	if len(dest) != 6 {
		return fmt.Errorf("quality target schedule stub got %d destinations, want 6", len(dest))
	}
	*(dest[0].(*string)) = s.targetGUID
	*(dest[1].(*int16)) = int16(s.targetType)
	*(dest[2].(*int32)) = s.intervalSeconds
	*(dest[3].(*sql.NullTime)) = s.nextRunAt
	*(dest[4].(*time.Time)) = s.createdAt
	*(dest[5].(*time.Time)) = s.updatedAt
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
