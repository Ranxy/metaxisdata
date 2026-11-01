package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityTrendLimit resolves the requested window of a check's trend. Omitted or
// non-positive means the documented default; a window longer than the execution
// retention's daily ceiling is clamped, because no older judged execution exists.
func qualityTrendLimit(requested int32) int {
	if requested <= 0 {
		return dataquality.TrendDefaultPoints
	}
	return min(int(requested), dataquality.TrendMaxPoints)
}

// GetQualityRuleTrend reads one check's judged executions into its plotted
// history, the band its current thresholds draw and a threshold suggestion
// derived from the same history. It is a read model over rows the runner already
// wrote: nothing is written, audited or measured against the target database.
//
// Only executions against the rule's current target are read, because a check
// moved to another table cannot compare row counts across the move.
func (s *DataQualityService) GetQualityRuleTrend(ctx context.Context, req *connect.Request[v1pb.GetQualityRuleTrendRequest]) (*connect.Response[v1pb.QualityRuleTrend], error) {
	workspaceID, resourceID, err := s.ruleResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	rule, err := s.fetchQualityRule(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	executions, err := s.store.ListRecentJudgedQualityExecutions(ctx, rule.ID,
		store.MetaGUIDKey{GUID: rule.TargetGUID, ObjectType: rule.TargetType}, qualityTrendLimit(req.Msg.GetLimit()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the check's judged executions"))
	}
	revisions, err := s.store.ListQualityRuleRevisionsByRevision(ctx, rule.ID, trendRevisionNumbers(executions))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the check's revisions"))
	}
	return connect.NewResponse(buildQualityRuleTrend(workspaceID, rule, executions, revisions)), nil
}

// trendRevisionNumbers collects the distinct revisions the window's executions
// were judged with, so the read model resolves only the snapshots it needs.
func trendRevisionNumbers(executions []*store.QualityExecutionMessage) []int32 {
	revisions := make([]int32, 0, len(executions))
	seen := make(map[int32]struct{}, len(executions))
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		if _, ok := seen[execution.RuleRevision]; ok {
			continue
		}
		seen[execution.RuleRevision] = struct{}{}
		revisions = append(revisions, execution.RuleRevision)
	}
	return revisions
}

// buildQualityRuleTrend maps the stored history onto the response model. It is a
// pure function so the empty-history and ordering contract is testable without a
// database: a check that was never judged is a trend with no points and a
// NO_JUDGED_EXECUTION notice, never an error.
func buildQualityRuleTrend(workspaceID string, rule *store.QualityRuleMessage, executions []*store.QualityExecutionMessage, revisions []*store.QualityRuleRevisionMessage) *v1pb.QualityRuleTrend {
	if rule == nil {
		return nil
	}
	input := dataquality.TrendInput{
		RuleType:   rule.RuleType,
		Revision:   rule.Revision,
		Config:     rule.Config,
		Executions: make([]dataquality.TrendExecution, 0, len(executions)),
		Revisions:  make([]dataquality.TrendRevision, 0, len(revisions)),
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		input.Executions = append(input.Executions, dataquality.TrendExecution{
			ID:            execution.ID,
			StartedAt:     execution.StartedAt,
			Status:        execution.Status,
			ObservedValue: execution.ObservedValue,
			FailedRows:    execution.FailedRows,
			TotalRows:     execution.TotalRows,
			Sampled:       execution.Sampled,
			Revision:      execution.RuleRevision,
			Thresholds:    execution.Thresholds,
		})
	}
	for _, revision := range revisions {
		if revision == nil {
			continue
		}
		input.Revisions = append(input.Revisions, dataquality.TrendRevision{
			Revision: revision.Revision,
			RuleType: revision.RuleType,
			Config:   revision.Config,
		})
	}

	trend := dataquality.BuildTrend(input)
	converted := &v1pb.QualityRuleTrend{
		Rule:             formatQualityRuleName(workspaceID, rule.ResourceID),
		Unit:             convertQualityTrendUnit(trend.Unit),
		Points:           make([]*v1pb.QualityRuleTrendPoint, 0, len(trend.Points)),
		Thresholds:       convertQualityThresholdsToV1(trend.Band),
		BandNotice:       convertQualityTrendNotice(trend.BandNotice),
		HistoryRestarted: trend.HistoryRestarted,
	}
	// BuildTrend returns the points oldest first, which is the proto's documented
	// order; the store read is newest first and the read model flips it once.
	for _, point := range trend.Points {
		converted.Points = append(converted.Points, &v1pb.QualityRuleTrendPoint{
			Execution:    formatQualityExecutionName(workspaceID, point.ExecutionID),
			StartTime:    timestamppb.New(point.StartedAt),
			Status:       v1pb.QualityStatus(point.Status),
			Value:        point.Value,
			FailedRows:   point.FailedRows,
			TotalRows:    point.TotalRows,
			Sampled:      point.Sampled,
			RuleRevision: point.Revision,
		})
	}
	converted.Suggestion = convertQualityTrendSuggestion(trend.Suggestion)
	return converted
}

// convertQualityTrendSuggestion maps a suggestion of the read model. An absent
// suggestion is still returned: its notice explains why no threshold could be
// derived, which is the information the editor shows.
func convertQualityTrendSuggestion(suggestion dataquality.TrendSuggestion) *v1pb.QualityThresholdSuggestion {
	return &v1pb.QualityThresholdSuggestion{
		Thresholds: convertQualityThresholdsToV1(suggestion.Thresholds),
		SampleSize: suggestion.SampleSize,
		Method:     convertQualityTrendSuggestionMethod(suggestion.Method),
		Notice:     convertQualityTrendNotice(suggestion.Notice),
	}
}

// convertQualityTrendUnit maps the read model's unit onto the API enum.
func convertQualityTrendUnit(unit dataquality.TrendUnit) v1pb.QualityTrendUnit {
	switch unit {
	case dataquality.TrendUnitUnspecified:
		return v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_UNSPECIFIED
	case dataquality.TrendUnitCount:
		return v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_COUNT
	case dataquality.TrendUnitPercent:
		return v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_PERCENT
	case dataquality.TrendUnitSeconds:
		return v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_SECONDS
	case dataquality.TrendUnitNumber:
		return v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_NUMBER
	default:
		return v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_UNSPECIFIED
	}
}

// convertQualityTrendNotice maps the read model's explanation onto the API enum.
func convertQualityTrendNotice(notice dataquality.TrendNotice) v1pb.QualityTrendNotice {
	switch notice {
	case dataquality.TrendNoticeUnspecified:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSPECIFIED
	case dataquality.TrendNoticeNoJudgedExecution:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_NO_JUDGED_EXECUTION
	case dataquality.TrendNoticeEditedSinceLastRun:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_EDITED_SINCE_LAST_RUN
	case dataquality.TrendNoticeUnitNotComparable:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNIT_NOT_COMPARABLE
	case dataquality.TrendNoticeUnsupportedRuleType:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSUPPORTED_RULE_TYPE
	case dataquality.TrendNoticeInsufficientHistory:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_INSUFFICIENT_HISTORY
	case dataquality.TrendNoticeUnsupportedConfig:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSUPPORTED_CONFIG
	default:
		return v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSPECIFIED
	}
}

// convertQualityTrendSuggestionMethod maps the suggestion arithmetic onto the API
// enum, so the editor can state how the number was reached.
func convertQualityTrendSuggestionMethod(method dataquality.TrendSuggestionMethod) v1pb.QualityThresholdSuggestionMethod {
	switch method {
	case dataquality.TrendSuggestionMethodUnspecified:
		return v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_UNSPECIFIED
	case dataquality.TrendSuggestionMethodP95P99:
		return v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_P95_P99
	case dataquality.TrendSuggestionMethodObservedRange:
		return v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_OBSERVED_RANGE
	default:
		return v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_UNSPECIFIED
	}
}
