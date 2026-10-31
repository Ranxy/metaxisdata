package v1

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/qualityreport"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// qualityReportDefaultTrendDays is the window a report covers when the caller
	// asks for none. It matches the overview's default so the two pages agree.
	qualityReportDefaultTrendDays = 30
	// qualityReportMaxTrendDays is the execution retention: a longer window could
	// only show empty days.
	qualityReportMaxTrendDays = 90
)

// ExportQualityReport renders one target's quality report as a self-contained
// HTML document: the current state, the recent trend, the checks, the unresolved
// problems, a capped sample summary and the monitoring basis. Nothing in the
// document is fetched at display time, so it can be forwarded as a file.
//
// The RPC is gated by metaxisdata.dataQuality.rules.list, the target's read
// permission. The failed-sample section is part of that read: a target's
// evidence belongs to whoever may read the target, so the report deliberately
// does not require the execution-read permission as well.
func (s *DataQualityService) ExportQualityReport(ctx context.Context, req *connect.Request[v1pb.ExportQualityReportRequest]) (*connect.Response[v1pb.ExportQualityReportResponse], error) {
	if _, err := s.workspaceID(ctx, req.Msg.GetParent()); err != nil {
		return nil, err
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("target_guid is required")
	}
	// A report reads the same sample evidence a bulk export does, so it shares
	// the export concurrency bound: an unbounded fan-out of renders would spend
	// the metadata database's connections and the process's memory.
	release, err := s.acquireExportSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	days := qualityReportTrendDays(req.Msg.GetTrendDays())
	now := time.Now().UTC()
	windowStart := qualityReportWindowStart(now, days)

	// The report reuses the workspace assembly, so it renders exactly the rules,
	// issues and schedule GetQualityTarget returns. History is not needed: the rule
	// list shows each rule's last result, and the trend is a separate query.
	workspace, err := s.loadQualityTargetWorkspace(ctx, targetGUID,
		storepb.MetaType(req.Msg.GetMetaType()), 0, qualityTargetOpenIssueLimit)
	if err != nil {
		return nil, err
	}
	trend, err := s.store.QualityTargetExecutionTrend(ctx, targetGUID, windowStart)
	if err != nil {
		return nil, err
	}
	points := fillQualityTrend(windowStart, days, trend)

	// Fetch exactly the checks the report can list. The omitted count comes from
	// the trend tally (or this list, whichever saw more), never from rows read
	// only to be discarded.
	failedLimit := qualityreport.MaxFailedChecks
	failedExecutions, err := s.store.ListQualityExecutions(ctx, &store.FindQualityExecutionMessage{
		TargetGUID: &targetGUID,
		States:     []storepb.QualityExecutionState{storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED},
		Statuses:   []storepb.QualityStatus{storepb.QualityStatus_QUALITY_STATUS_FAIL},
		StartAt:    &windowStart,
		Limit:      &failedLimit,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the target's failed executions")
	}

	report := buildQualityReport(workspace, points, failedExecutions, windowStart, now, qualityReportGeneratedBy(ctx))
	document, err := qualityreport.Render(req.Msg.GetLanguage(), report)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to render the quality report"))
	}
	return connect.NewResponse(&v1pb.ExportQualityReportResponse{
		FileName: qualityReportFileName(now),
		// The document is html_content, not content: the audit layer stores
		// response payloads and redacts this field by name, so a report carrying
		// failed-sample-derived data never enters the permanent ledger.
		HtmlContent: document,
	}), nil
}

// qualityReportTrendDays clamps the requested window: zero (and a negative value)
// means the default, and the retention caps a longer one.
func qualityReportTrendDays(requested int32) int {
	switch {
	case requested <= 0:
		return qualityReportDefaultTrendDays
	case requested > qualityReportMaxTrendDays:
		return qualityReportMaxTrendDays
	default:
		return int(requested)
	}
}

// qualityReportWindowStart is the UTC midnight that begins a window of days.
func qualityReportWindowStart(now time.Time, days int) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
}

// qualityReportFileName is the download name, a UTC timestamp in the proto's
// documented shape: quality-report-20261031T120000Z.html.
func qualityReportFileName(at time.Time) string {
	return "quality-report-" + at.UTC().Format("20060102T150405Z") + ".html"
}

// qualityReportGeneratedBy names the caller in the document. The report is
// explicitly forwardable, so it carries the display name only: a contact address
// belongs in the audit row, not in a file handed to third parties.
func qualityReportGeneratedBy(ctx context.Context) string {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return ""
	}
	if user.Name != "" {
		return user.Name
	}
	return common.FormatUserUID(user.ID)
}

// buildQualityReport maps the assembled store data onto the renderer's report.
// It is a pure function of its inputs, so the mapping (orphan state, rule list,
// failed-sample caps, basis) is unit-testable without a store.
func buildQualityReport(workspace *qualityTargetWorkspace, points []*v1pb.QualityTrendPoint, failedExecutions []*store.QualityExecutionMessage, windowStart, generatedAt time.Time, generatedBy string) *qualityreport.Report {
	rules := make(map[int64]*store.QualityRuleMessage, len(workspace.rules))
	for _, rule := range workspace.rules {
		rules[rule.ID] = rule
	}
	report := &qualityreport.Report{
		Target:         qualityReportTarget(workspace),
		WindowDays:     len(points),
		WindowStart:    windowStart,
		GeneratedAt:    generatedAt,
		GeneratedBy:    generatedBy,
		Orphaned:       workspace.orphanedReason != "",
		OrphanedReason: workspace.orphanedReason,
		RuleCount:      int32(len(workspace.rules)),
		FailedChecks:   qualityReportFailedChecks(failedExecutions, rules),
	}
	for _, rule := range workspace.rules {
		if rule.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED {
			report.EnabledRuleCount++
		}
	}
	// The report carries the same quality score as the target summary, computed by
	// the same helper so the file and the workspace header can never disagree.
	scores := qualityTargetScores(workspace.rules, workspace.latest)
	runKey := store.MetaGUIDKey{GUID: workspace.targetGUID, ObjectType: workspace.metaType}
	report.QualityScore = scores[runKey].score
	report.ScoredRuleCount = scores[runKey].scored
	report.UnscoredRuleCount = report.EnabledRuleCount - report.ScoredRuleCount
	worstRank := int32(0)
	for _, rule := range workspace.rules {
		entry := qualityreport.Rule{
			Title:        rule.Title,
			TypeKey:      qualityReportEnumKey(rule.RuleType),
			DimensionKey: qualityReportEnumKey(rule.Dimension),
			SeverityKey:  qualityReportEnumKey(rule.Severity),
			StateKey:     qualityReportEnumKey(rule.State),
			Column:       qualityReportRuleColumn(rule),
			Owner:        formatQualityOwner(rule.OwnerID),
			Revision:     rule.Revision,
		}
		if execution := workspace.latest[rule.ID]; execution != nil {
			entry.LastStatusKey = qualityReportEnumKey(execution.Status)
			entry.ObservedValue = execution.ObservedValue
			entry.ObservedKindKey = qualityReportEnumKey(execution.ObservedKind)
			entry.Thresholds = execution.Thresholds
			entry.LastRunTime = execution.FinishedAt
			if rank := store.QualityTargetStatusRank(execution.Status); rank > worstRank {
				worstRank = rank
			}
		}
		report.Rules = append(report.Rules, entry)
	}
	if worstRank > 0 {
		report.WorstStatusKey = qualityReportEnumKey(store.QualityTargetStatusFromRank(worstRank))
	}
	for _, point := range points {
		report.Counts.Total += point.GetTotal()
		report.Counts.Pass += point.GetPass()
		report.Counts.Warn += point.GetWarn()
		report.Counts.Fail += point.GetFail()
		report.Counts.Error += point.GetError()
		report.Counts.Skipped += point.GetSkipped()
		report.Counts.NotEvaluated += point.GetNotEvaluated()
		report.Trend = append(report.Trend, qualityreport.TrendDay{
			Date: point.GetDate(),
			Counts: qualityreport.Counts{
				Total:        point.GetTotal(),
				Pass:         point.GetPass(),
				Warn:         point.GetWarn(),
				Fail:         point.GetFail(),
				Error:        point.GetError(),
				Skipped:      point.GetSkipped(),
				NotEvaluated: point.GetNotEvaluated(),
			},
		})
	}
	// Every FAIL execution in the window is a failed check. The trend tally and
	// the sample query are two observations, and an execution may complete
	// between them, so the larger one is the omitted-count denominator: the list
	// must never claim to be longer than the total it is shown against.
	report.FailedCheckTotal = report.Counts.Fail
	if fetched := int32(len(failedExecutions)); fetched > report.FailedCheckTotal {
		report.FailedCheckTotal = fetched
	}
	for _, issue := range workspace.openIssues {
		title := ""
		if rule := rules[issue.RuleID]; rule != nil {
			title = rule.Title
		}
		report.OpenIssues = append(report.OpenIssues, qualityreport.Issue{
			RuleTitle:   title,
			SeverityKey: qualityReportEnumKey(issue.Severity),
			StatusKey:   qualityReportEnumKey(issue.Status),
			FirstSeen:   issue.FirstSeenAt,
			LastSeen:    issue.LastSeenAt,
			Occurrences: issue.OccurrenceCount,
		})
	}
	report.Basis = qualityReportBasis(workspace)
	return report
}

// qualityReportTarget describes the reported object. A target that no longer
// resolves keeps the path snapshot saved with its rules, which is the only
// description left.
func qualityReportTarget(workspace *qualityTargetWorkspace) qualityreport.Target {
	target := qualityreport.Target{
		GUID:    workspace.targetGUID,
		TypeKey: qualityReportEnumKey(workspace.metaType),
		Path:    qualityReportDisplayPath(workspace.path),
	}
	if workspace.resolution != nil {
		target.Instance = workspace.resolution.Target.InstanceResourceID
		target.Engine = workspace.resolution.Target.Engine.String()
		return target
	}
	if segments := common.SplitMetaGUID(workspace.targetGUID); len(segments) > 0 {
		target.Instance = segments[0]
	}
	return target
}

// qualityReportBasis records the monitoring basis: the schedule, the current rule
// revisions and the engine versions that produced the results.
func qualityReportBasis(workspace *qualityTargetWorkspace) qualityreport.Basis {
	basis := qualityreport.Basis{}
	if workspace.schedule != nil {
		basis.ScheduleInterval = workspace.schedule.ScheduleInterval
		basis.NextRunTime = workspace.schedule.NextRunAt
	}
	if len(workspace.rules) > 0 {
		basis.HasRevisionRange = true
		for index, rule := range workspace.rules {
			if index == 0 || rule.Revision < basis.RuleRevisionMin {
				basis.RuleRevisionMin = rule.Revision
			}
			if index == 0 || rule.Revision > basis.RuleRevisionMax {
				basis.RuleRevisionMax = rule.Revision
			}
		}
	}
	seen := map[string]struct{}{}
	for _, execution := range workspace.latest {
		if execution.RuleEngineVersion == "" {
			continue
		}
		if _, ok := seen[execution.RuleEngineVersion]; ok {
			continue
		}
		seen[execution.RuleEngineVersion] = struct{}{}
		basis.EngineVersions = append(basis.EngineVersions, execution.RuleEngineVersion)
	}
	slices.Sort(basis.EngineVersions)
	if workspace.resolution != nil {
		basis.TargetColumnCount = int32(len(workspace.resolution.Target.Columns))
	}
	return basis
}

// qualityReportFailedChecks turns the recent failed executions into the report's
// sample section. The renderer applies the hard caps (20 checks, 5 rows each, 2
// MiB); the mapping only carries the evidence over.
func qualityReportFailedChecks(executions []*store.QualityExecutionMessage, rules map[int64]*store.QualityRuleMessage) []qualityreport.FailedCheck {
	checks := make([]qualityreport.FailedCheck, 0, len(executions))
	for _, execution := range executions {
		check := qualityreport.FailedCheck{
			RuleTitle:       qualityReportFailedCheckTitle(execution, rules),
			Column:          qualityReportFailedCheckColumn(execution, rules),
			FinishedAt:      execution.FinishedAt,
			FailedRows:      execution.FailedRows,
			TotalRows:       execution.TotalRows,
			Sampled:         execution.Sampled,
			SampleTruncated: execution.Truncated,
			ErrorCode:       execution.ErrorCode,
			ErrorMessage:    execution.ErrorMessage,
		}
		if check.FinishedAt.IsZero() {
			check.FinishedAt = execution.StartedAt
		}
		samples := execution.Sample.GetSamples()
		// Keep one row past the render cap so the document can say the sample
		// list was cut; copying the execution's remaining rows would only waste
		// memory, since the renderer cannot show them.
		if len(samples) > qualityreport.MaxSampleRows+1 {
			samples = samples[:qualityreport.MaxSampleRows+1]
		}
		for _, sample := range samples {
			converted := qualityreport.FailureSample{Cells: make([]qualityreport.SampleCell, 0, len(sample.GetColumns()))}
			for _, cell := range sample.GetColumns() {
				converted.Cells = append(converted.Cells, qualityreport.SampleCell{
					Column: cell.GetColumn(),
					Value:  cell.GetValue(),
					IsNull: cell.GetIsNull(),
				})
			}
			check.Samples = append(check.Samples, converted)
		}
		checks = append(checks, check)
	}
	return checks
}

// qualityReportFailedCheckTitle names the rule that failed. A rule archived
// since the execution stays identified by its row id, so the evidence is never
// anonymous.
func qualityReportFailedCheckTitle(execution *store.QualityExecutionMessage, rules map[int64]*store.QualityRuleMessage) string {
	if rule := rules[execution.RuleID]; rule != nil {
		return rule.Title
	}
	return fmt.Sprintf("#%d", execution.RuleID)
}

// qualityReportFailedCheckColumn names the checked column, from the execution's
// own snapshot first: it is the column the failed row shows.
func qualityReportFailedCheckColumn(execution *store.QualityExecutionMessage, rules map[int64]*store.QualityRuleMessage) string {
	if column := execution.TargetPath.GetColumn(); column != "" {
		return column
	}
	if rule := rules[execution.RuleID]; rule != nil {
		return dataquality.ConfigColumn(rule.Config)
	}
	return ""
}

// qualityReportRuleColumn is the column a rule checks: the rule config names it,
// the path snapshot is the fallback for rules written before that was populated.
func qualityReportRuleColumn(rule *store.QualityRuleMessage) string {
	if column := dataquality.ConfigColumn(rule.Config); column != "" {
		return column
	}
	return rule.TargetPath.GetColumn()
}

// qualityReportDisplayPath renders a path snapshot as
// instance/database/schema/object[.column], skipping the containers an engine
// does not have. It is display text, not an identity.
func qualityReportDisplayPath(path *storepb.QualityTargetPath) string {
	segments := make([]string, 0, 4)
	for _, segment := range []string{path.GetInstance(), path.GetDatabase(), path.GetSchema(), path.GetObject()} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	display := strings.Join(segments, "/")
	if path.GetColumn() != "" {
		display += "." + path.GetColumn()
	}
	return display
}

// qualityReportEnumPrefixes are stripped from an enum's String so the report
// carries the bare value: QUALITY_STATUS_NOT_EVALUATED becomes NOT_EVALUATED.
var qualityReportEnumPrefixes = []string{
	"QUALITY_STATUS_",
	"QUALITY_SEVERITY_",
	"QUALITY_RULE_TYPE_",
	"QUALITY_RULE_STATE_",
	"QUALITY_DIMENSION_",
	"QUALITY_ISSUE_STATUS_",
	"QUALITY_OBSERVED_KIND_",
	"QUALITY_EXECUTION_STATE_",
	"QUALITY_COMPARISON_",
}

// qualityReportEnumKey is the renderer's catalog key for one enum value. The
// renderer owns the translated text, so the handler passes the bare enum value.
func qualityReportEnumKey(value fmt.Stringer) string {
	name := value.String()
	for _, prefix := range qualityReportEnumPrefixes {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return name
}
