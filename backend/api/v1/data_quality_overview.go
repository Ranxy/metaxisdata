package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityOverviewTrendDays bounds the trend window a caller may ask for.
const (
	defaultQualityOverviewTrendDays = 7
	maxQualityOverviewTrendDays     = 30
)

// GetQualityOverview aggregates the workspace's quality state for the overview
// page: rule and issue counts, the last day's activity and the daily trend.
func (s *DataQualityService) GetQualityOverview(ctx context.Context, req *connect.Request[v1pb.GetQualityOverviewRequest]) (*connect.Response[v1pb.QualityOverview], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	days := qualityOverviewTrendDays(req.Msg.GetTrendDays())
	now := time.Now().UTC()
	dayStart := now.Add(-24 * time.Hour)
	trendStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))

	ruleCounts, err := s.store.QualityRuleStateCounts(ctx)
	if err != nil {
		return nil, err
	}
	monitored, err := s.store.CountMonitoredTables(ctx)
	if err != nil {
		return nil, err
	}
	issueCounts, err := s.store.QualityOpenIssueSeverityCounts(ctx)
	if err != nil {
		return nil, err
	}
	failingRules, err := s.store.CountDistinctFailedRulesSince(ctx, dayStart)
	if err != nil {
		return nil, err
	}
	executionCount, err := s.store.CountQualityExecutionsSince(ctx, dayStart)
	if err != nil {
		return nil, err
	}
	lastDay, err := s.store.QualityExecutionStatusCountsSince(ctx, dayStart)
	if err != nil {
		return nil, err
	}
	trend, err := s.store.QualityExecutionTrend(ctx, trendStart)
	if err != nil {
		return nil, err
	}

	enabled := int32(ruleCounts[storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED])
	overview := &v1pb.QualityOverview{
		RuleCount:           enabled + int32(ruleCounts[storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED]),
		EnabledRuleCount:    enabled,
		PausedRuleCount:     int32(ruleCounts[storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED]),
		MonitoredTableCount: int32(monitored),
		OpenWarningCount:    int32(issueCounts[storepb.QualitySeverity_QUALITY_SEVERITY_WARNING]),
		OpenErrorCount:      int32(issueCounts[storepb.QualitySeverity_QUALITY_SEVERITY_ERROR]),
		OpenCriticalCount:   int32(issueCounts[storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL]),
		FailingRuleCount:    int32(failingRules),
		ExecutionCount:      int32(executionCount),
		LastDayCounts:       convertQualityStatusCountsToV1(lastDay),
		Trend:               fillQualityTrend(trendStart, days, trend),
		TrendDays:           int32(days),
	}
	return connect.NewResponse(overview), nil
}

// qualityOverviewTrendDays clamps the requested trend window into the accepted
// range.
func qualityOverviewTrendDays(requested int32) int {
	switch {
	case requested <= 0:
		return defaultQualityOverviewTrendDays
	case requested > maxQualityOverviewTrendDays:
		return maxQualityOverviewTrendDays
	default:
		return int(requested)
	}
}

// fillQualityTrend turns the sparse per-day rows into one point per day, so a day
// without executions is a zero bar instead of a gap that shifts the chart.
func fillQualityTrend(start time.Time, days int, trend []store.QualityTrendDay) []*v1pb.QualityTrendPoint {
	byDay := make(map[string]*storepb.QualityStatusCounts, len(trend))
	for _, day := range trend {
		byDay[day.Date] = day.Counts
	}
	points := make([]*v1pb.QualityTrendPoint, 0, days)
	for offset := 0; offset < days; offset++ {
		date := start.AddDate(0, 0, offset).Format("2006-01-02")
		counts := byDay[date]
		if counts == nil {
			counts = &storepb.QualityStatusCounts{}
		}
		points = append(points, &v1pb.QualityTrendPoint{
			Date:         date,
			Pass:         counts.GetPass(),
			Warn:         counts.GetWarn(),
			Fail:         counts.GetFail(),
			Error:        counts.GetError(),
			Skipped:      counts.GetSkipped(),
			NotEvaluated: counts.GetNotEvaluated(),
			Total:        counts.GetTotal(),
		})
	}
	return points
}
