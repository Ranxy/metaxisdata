package v1

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
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
// page: rule and issue counts, the last day's activity, the daily trend, and the
// coverage and quality score the workspace can act on.
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
	// The score is computed from the enabled rules themselves, in Go: the checked
	// column comes from the rule config, which SQL cannot read without duplicating
	// that mapping. The workspace rule cap bounds the read.
	enabledRules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{
		State: storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the enabled quality rules")
	}
	latest, err := s.latestQualityExecutions(ctx, enabledRules)
	if err != nil {
		return nil, err
	}
	// Environment titles are presentation only: a breakdown row falls back to its
	// key, so a settings read blip must not blank the whole overview.
	environments, err := s.store.ListEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to read the environments for the quality overview", clog.WithError(err))
		environments = nil
	}
	// A rule can outlive its target or its column. The numerator is restricted to
	// the targets (and columns) the coverage denominator still counts, so it can
	// never claim coverage that no longer exists.
	targetKeys := make([]store.MetaGUIDKey, 0, len(enabledRules))
	seenTarget := make(map[store.MetaGUIDKey]struct{}, len(enabledRules))
	for _, rule := range enabledRules {
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		if _, ok := seenTarget[key]; ok {
			continue
		}
		seenTarget[key] = struct{}{}
		targetKeys = append(targetKeys, key)
	}
	// One repeatable-read snapshot for the denominator and the column guard: two
	// pooled reads could disagree when schema sync drops and re-creates an object,
	// letting the numerator claim a target the denominator never counted.
	coverage, targetColumns, err := s.store.QualityCoverageSnapshot(ctx, targetKeys)
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
	overview := assembleQualityScoreOverview(enabledRules, targetColumns, latest, coverage, environments)
	overview.RuleCount = enabled + int32(ruleCounts[storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED])
	overview.EnabledRuleCount = enabled
	overview.PausedRuleCount = int32(ruleCounts[storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED])
	overview.OpenWarningCount = int32(issueCounts[storepb.QualitySeverity_QUALITY_SEVERITY_WARNING])
	overview.OpenErrorCount = int32(issueCounts[storepb.QualitySeverity_QUALITY_SEVERITY_ERROR])
	overview.OpenCriticalCount = int32(issueCounts[storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL])
	overview.FailingRuleCount = int32(failingRules)
	overview.ExecutionCount = int32(executionCount)
	overview.LastDayCounts = convertQualityStatusCountsToV1(lastDay)
	overview.Trend = fillQualityTrend(trendStart, days, trend)
	overview.TrendDays = int32(days)
	return connect.NewResponse(overview), nil
}

// qualityOverviewDimension is the per-dimension score accumulator.
type qualityOverviewDimension struct {
	aggregate dataquality.ScoreAggregate
	rules     int32
}

// qualityOverviewOwner is the per-owner score accumulator.
type qualityOverviewOwner struct {
	aggregate dataquality.ScoreAggregate
	rules     int32
}

// qualityOverviewInstance is one instance's coverage numerator: the distinct
// targets an enabled rule watches and the distinct (target, column) pairs it
// checks.
type qualityOverviewInstance struct {
	monitored map[string]struct{}
	covered   map[string]struct{}
}

// qualityOverviewBreakdown is a coverage roll-up by instance or environment.
type qualityOverviewBreakdown struct {
	tables    int32
	columns   int32
	monitored int32
	covered   int32
}

// assembleQualityScoreOverview fills the coverage and score fields of the
// overview: the coverage totals, the judged-target count, the weighted workspace
// score and its scored/unscored rule split, the per-instance and per-environment
// coverage, and the per-dimension and per-owner scores. It is a pure function of
// its inputs so every formula is testable without a store.
//
// Only a rule whose target still has a coverage denominator row, and whose
// checked column is still in that target's registry, counts: a rule that outlived
// its table, its instance or its column has no denominator row to be counted
// against, and counting it would push coverage past 100%. A rule is scored from
// its latest completed execution against its current target; ERROR, SKIPPED and
// NOT_EVALUATED are not verdicts, so they leave the rule unscored instead of
// pulling the score down. No judged rule means no score at all.
func assembleQualityScoreOverview(enabledRules []*store.QualityRuleMessage, targetColumns map[store.MetaGUIDKey]map[string]struct{}, latest map[int64]*store.QualityExecutionMessage, coverage []*store.QualityCoverageRow, environments []*storepb.EnvironmentSetting_Environment) *v1pb.QualityOverview {
	overview := &v1pb.QualityOverview{}

	environmentTitles := make(map[string]string, len(environments))
	for _, environment := range environments {
		environmentTitles[environment.GetId()] = environment.GetTitle()
	}

	workspaceScore := &dataquality.ScoreAggregate{}
	scoredRules, unscoredRules := int32(0), int32(0)
	monitoredTargets := make(map[string]struct{}, len(enabledRules))
	judgedTargets := make(map[string]struct{}, len(enabledRules))
	coveredColumns := make(map[string]struct{}, len(enabledRules))
	instances := make(map[string]*qualityOverviewInstance)
	dimensions := make(map[storepb.QualityDimension]*qualityOverviewDimension)
	owners := make(map[string]*qualityOverviewOwner)

	for _, rule := range enabledRules {
		key := store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}
		currentColumns, ok := targetColumns[key]
		if !ok {
			// The target no longer resolves, so there is no coverage denominator
			// row to count this rule against.
			continue
		}
		monitoredTargets[rule.TargetGUID] = struct{}{}
		instanceID := ""
		if extracted, ok := common.GetInstanceFromGUID(rule.TargetGUID); ok {
			instanceID = extracted
		}
		instance := instances[instanceID]
		if instance == nil {
			instance = &qualityOverviewInstance{monitored: map[string]struct{}{}, covered: map[string]struct{}{}}
			instances[instanceID] = instance
		}
		instance.monitored[rule.TargetGUID] = struct{}{}
		if column := dataquality.ConfigColumn(rule.Config); column != "" {
			// A dropped or renamed column is no longer in the registry, so it is
			// no longer covered either.
			if _, exists := currentColumns[column]; exists {
				columnKey := rule.TargetGUID + "\x00" + column
				coveredColumns[columnKey] = struct{}{}
				instance.covered[columnKey] = struct{}{}
			}
		}
		dimension := dimensions[rule.Dimension]
		if dimension == nil {
			dimension = &qualityOverviewDimension{}
			dimensions[rule.Dimension] = dimension
		}
		dimension.rules++
		ownerKey := formatQualityOwner(rule.OwnerID)
		owner := owners[ownerKey]
		if owner == nil {
			owner = &qualityOverviewOwner{}
			owners[ownerKey] = owner
		}
		owner.rules++

		execution := ruleLatestResult(rule, latest)
		if execution == nil {
			unscoredRules++
			continue
		}
		score, judged := dataquality.CheckScore(rule.RuleType, execution.Status, execution.FailedRows, execution.TotalRows)
		if !judged {
			unscoredRules++
			continue
		}
		scoredRules++
		failing := execution.Status == storepb.QualityStatus_QUALITY_STATUS_FAIL
		workspaceScore.Add(score, rule.Severity, failing)
		dimension.aggregate.Add(score, rule.Severity, failing)
		owner.aggregate.Add(score, rule.Severity, failing)
		judgedTargets[rule.TargetGUID] = struct{}{}
	}

	overview.MonitoredTableCount = int32(len(monitoredTargets))
	overview.CoveredColumnCount = int32(len(coveredColumns))
	overview.JudgedTableCount = int32(len(judgedTargets))
	overview.ScoredRuleCount = scoredRules
	overview.UnscoredRuleCount = unscoredRules
	workspaceScoreValue, workspaceJudged := workspaceScore.Score()
	overview.QualityScore = scorePointer(workspaceScoreValue, workspaceJudged)

	var totalTables, totalColumns int32
	byEnvironment := make(map[string]*qualityOverviewBreakdown)
	for _, row := range coverage {
		totalTables += row.TableCount
		totalColumns += row.ColumnCount
		breakdown := &v1pb.QualityCoverageBreakdown{
			Key:              row.InstanceResourceID,
			Title:            row.InstanceTitle,
			TotalTableCount:  row.TableCount,
			TotalColumnCount: row.ColumnCount,
		}
		if instance := instances[row.InstanceResourceID]; instance != nil {
			breakdown.MonitoredTableCount = int32(len(instance.monitored))
			breakdown.CoveredColumnCount = int32(len(instance.covered))
		}
		overview.CoverageByInstance = append(overview.CoverageByInstance, breakdown)

		environment := byEnvironment[row.Environment]
		if environment == nil {
			environment = &qualityOverviewBreakdown{}
			byEnvironment[row.Environment] = environment
		}
		environment.tables += row.TableCount
		environment.columns += row.ColumnCount
		environment.monitored += breakdown.MonitoredTableCount
		environment.covered += breakdown.CoveredColumnCount
	}
	overview.TotalTableCount = totalTables
	overview.TotalColumnCount = totalColumns

	environmentKeys := make([]string, 0, len(byEnvironment))
	for key := range byEnvironment {
		environmentKeys = append(environmentKeys, key)
	}
	slices.Sort(environmentKeys)
	for _, key := range environmentKeys {
		environment := byEnvironment[key]
		overview.CoverageByEnvironment = append(overview.CoverageByEnvironment, &v1pb.QualityCoverageBreakdown{
			Key:                 key,
			Title:               environmentTitles[key],
			TotalTableCount:     environment.tables,
			MonitoredTableCount: environment.monitored,
			TotalColumnCount:    environment.columns,
			CoveredColumnCount:  environment.covered,
		})
	}

	dimensionValues := make([]storepb.QualityDimension, 0, len(dimensions))
	for dimension := range dimensions {
		dimensionValues = append(dimensionValues, dimension)
	}
	slices.Sort(dimensionValues)
	for _, dimension := range dimensionValues {
		entry := dimensions[dimension]
		score := &v1pb.QualityDimensionScore{
			Dimension:        v1pb.QualityDimension(dimension),
			RuleCount:        entry.rules,
			ScoredRuleCount:  entry.aggregate.Scored(),
			FailingRuleCount: entry.aggregate.Failing(),
		}
		value, judged := entry.aggregate.Score()
		score.QualityScore = scorePointer(value, judged)
		overview.ScoreByDimension = append(overview.ScoreByDimension, score)
	}

	ownerKeys := make([]string, 0, len(owners))
	for owner := range owners {
		ownerKeys = append(ownerKeys, owner)
	}
	slices.Sort(ownerKeys)
	for _, owner := range ownerKeys {
		entry := owners[owner]
		score := &v1pb.QualityOwnerScore{
			Owner:            owner,
			RuleCount:        entry.rules,
			ScoredRuleCount:  entry.aggregate.Scored(),
			FailingRuleCount: entry.aggregate.Failing(),
		}
		value, judged := entry.aggregate.Score()
		score.QualityScore = scorePointer(value, judged)
		overview.ScoreByOwner = append(overview.ScoreByOwner, score)
	}
	return overview
}

// scorePointer renders an optional judged score; an unjudged scope has no score
// at all rather than a 0 or a 100.
func scorePointer(score float64, judged bool) *float64 {
	if !judged {
		return nil
	}
	return &score
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
