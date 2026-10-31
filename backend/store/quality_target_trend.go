package store

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityTargetTrendQuery groups one target's completed executions into UTC days
// and six states. The predicates are the whole contract: the exact target GUID
// keeps one target's report out of another's, the COMPLETED state keeps a queued
// or abandoned execution out of the tally, and the lower bound is the report
// window (a day boundary is inclusive, like QualityExecutionTrend).
const qualityTargetTrendQuery = `
	SELECT to_char(date_trunc('day', started_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
		status, COUNT(*)
	FROM quality_execution
	WHERE target_guid = $1 AND state = $2 AND started_at >= $3
	GROUP BY day, status
	ORDER BY day
`

// QualityTargetExecutionTrend groups one target's completed executions started
// at or after since into UTC days. Days without an execution are absent; the
// caller fills the gaps so a chart does not skip a quiet day. It is
// QualityExecutionTrend scoped to one target.
func (s *Store) QualityTargetExecutionTrend(ctx context.Context, targetGUID string, since time.Time) ([]QualityTrendDay, error) {
	targetGUID = strings.TrimSpace(targetGUID)
	if targetGUID == "" {
		return nil, common.Errorf(common.Invalid, "a target GUID is required")
	}
	if since.IsZero() {
		return nil, common.Errorf(common.Invalid, "a start time is required")
	}
	rows, err := s.GetDB().QueryContext(ctx, qualityTargetTrendQuery, targetGUID,
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED), since)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality target trend")
	}
	defer rows.Close()

	accumulator := newQualityTrendAccumulator()
	for rows.Next() {
		var (
			day    string
			status int16
			count  int32
		)
		if err := rows.Scan(&day, &status, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan the quality target trend")
		}
		accumulator.add(day, storepb.QualityStatus(status), count)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality target trend")
	}
	return accumulator.trend(), nil
}

// qualityTrendAccumulator collects (day, status, count) rows into the ordered
// six-state days a chart consumes. It is pure so the grouping and the ordering
// are unit-testable without a database; the query returns the rows already
// grouped, the accumulator only tallies and orders them.
type qualityTrendAccumulator struct {
	byDay map[string]*storepb.QualityStatusCounts
	days  []string
}

// newQualityTrendAccumulator creates an empty accumulator.
func newQualityTrendAccumulator() *qualityTrendAccumulator {
	return &qualityTrendAccumulator{byDay: map[string]*storepb.QualityStatusCounts{}}
}

// add tallies count into day. A row whose day is empty is ignored rather than
// grouped under the empty string, which would render as a bogus bar.
func (a *qualityTrendAccumulator) add(day string, status storepb.QualityStatus, count int32) {
	if day == "" {
		return
	}
	counts, ok := a.byDay[day]
	if !ok {
		counts = &storepb.QualityStatusCounts{}
		a.byDay[day] = counts
		a.days = append(a.days, day)
	}
	counts.Total += count
	addQualityStatusCount(counts, status, count)
}

// trend returns the days in first-seen order, which is chronological because the
// query orders by day.
func (a *qualityTrendAccumulator) trend() []QualityTrendDay {
	trend := make([]QualityTrendDay, 0, len(a.days))
	for _, day := range a.days {
		trend = append(trend, QualityTrendDay{Date: day, Counts: a.byDay[day]})
	}
	return trend
}
