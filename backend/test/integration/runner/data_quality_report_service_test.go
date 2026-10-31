//go:build integration

package runner

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The report is one self-contained HTML document rendered from the same target
// data GetQualityTarget returns. This scenario runs one failing rule against the
// MySQL fixture and checks the document's structure, the caps and its
// self-containment, in both languages and through the window clamp.
func TestDataQualityReportRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, true)
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Regex{
			Regex: &v1pb.RegexConfig{Column: "name", Pattern: "^z"},
		}})
	// The quality tables live in the shared control-plane database, and the target
	// directory lists every live target, so the scenario must leave no live rule
	// behind for the later directory assertions.
	deleteRule := func(name string) {
		_, err := fixture.dataQuality.DeleteQualityRule(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.DeleteQualityRuleRequest{Name: name}))
		require.NoError(t, err, "delete the scenario's rule")
	}
	t.Cleanup(func() { deleteRule(rule.GetName()) })
	execution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, execution.GetOutcome(),
		"the fixture row does not match the pattern, so the check fails with a sample")

	exportReport := func(request *v1pb.ExportQualityReportRequest) *v1pb.ExportQualityReportResponse {
		t.Helper()
		response, err := fixture.dataQuality.ExportQualityReport(fixture.ctx, withToken(fixture.env.AdminToken(), request))
		require.NoError(t, err, "export the quality report")
		return response.Msg
	}
	baseRequest := func() *v1pb.ExportQualityReportRequest {
		return &v1pb.ExportQualityReportRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
			TrendDays:  7,
			Language:   "en-US",
		}
	}

	english := exportReport(baseRequest())
	require.Regexp(t, `^quality-report-\d{8}T\d{6}Z\.html$`, english.GetFileName())
	document := english.GetHtmlContent()
	require.True(t, strings.HasPrefix(document, "<!doctype html>"), "a complete HTML document")
	require.Contains(t, document, `<html lang="en">`)
	for _, section := range []string{
		"Report scope", "Result summary", "Daily trend", "Rules", "Open issues", "Failed samples", "Monitoring basis",
	} {
		require.Contains(t, document, section)
	}
	require.Contains(t, document, "users", "the target path names the checked table")
	require.Contains(t, document, "Regex", "the rule list names the rule type")
	require.Contains(t, document, "alice", "the failed row's sampled value is evidence")
	require.Contains(t, document, "<svg", "the trend is an inline chart")
	require.Contains(t, document, "@media print")
	require.Contains(t, document, "Showing 1 of 1 failed checks in the window.")
	require.Contains(t, document, "capped at 2 MiB", "the documented size cap is stated in the document")
	require.Contains(t, document, "</html>")

	// The audited response must not carry the document: the ledger keeps the
	// request, the file name and the redacted field marker, never the samples.
	var auditHTML, auditFileName string
	require.NoError(t, fixture.env.Store.GetDB().QueryRowContext(fixture.ctx, `
		SELECT payload->'response'->>'htmlContent', payload->'response'->>'fileName'
		FROM audit_log
		WHERE payload->>'method' = $1
		ORDER BY id DESC
		LIMIT 1`,
		v1connect.DataQualityServiceExportQualityReportProcedure).Scan(&auditHTML, &auditFileName))
	require.Equal(t, audit.RedactedValue, auditHTML, "the report body must be redacted in the ledger")
	require.Equal(t, english.GetFileName(), auditFileName)
	require.NotContains(t, auditHTML, "<!doctype html>")
	// Self-contained: the document must not fetch anything when it is opened.
	for _, marker := range []string{"http://", "https://", "<script", "<link", "@import"} {
		require.NotContains(t, document, marker)
	}

	// The language is honored, and an empty language reads as English.
	chinese := exportReport(withQualityReportLanguage(baseRequest(), "zh-CN"))
	require.Contains(t, chinese.GetHtmlContent(), `<html lang="zh-CN">`)
	require.Contains(t, chinese.GetHtmlContent(), "数据质量报告")
	require.Contains(t, chinese.GetHtmlContent(), "失败样本")
	defaultLanguage := exportReport(withQualityReportLanguage(baseRequest(), ""))
	require.Contains(t, defaultLanguage.GetHtmlContent(), `<html lang="en">`)

	// trend_days 0 is the 30-day default and a larger value is capped at the
	// 90-day retention; the rendered window proves the clamp.
	require.Contains(t, exportReport(withQualityReportTrendDays(baseRequest(), 0)).GetHtmlContent(), "Last 30 days")
	require.Contains(t, exportReport(withQualityReportTrendDays(baseRequest(), 1000)).GetHtmlContent(), "Last 90 days")

	// A target with no rules and no meta type cannot be described; the API says so
	// instead of inventing a document.
	_, err := fixture.dataQuality.ExportQualityReport(fixture.ctx, withToken(fixture.env.AdminToken(),
		&v1pb.ExportQualityReportRequest{Parent: "workspaces/-", TargetGuid: "no-such-target"}))
	require.Error(t, err)
}

// An orphaned target keeps its rules readable from the snapshots, so its report
// still renders and says the live target could not be read.
func TestDataQualityReportOrphanedTargetRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, false)
	orphanGUID := "orphan-instance;missing-db;public;gone"
	resourceID, err := common.RandomString(12)
	require.NoError(t, err)
	minRows := int64(1)
	_, err = fixture.env.Store.CreateQualityRule(fixture.ctx, &store.QualityRuleMessage{
		ResourceID: resourceID,
		Title:      "orphan row count",
		Dimension:  storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		TargetGUID: orphanGUID,
		TargetType: storepb.MetaType_TABLE,
		TargetPath: &storepb.QualityTargetPath{
			Instance: "orphan-instance", Database: "missing-db", Schema: "public", Object: "gone",
		},
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: &minRows},
		}},
		Severity: storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		State:    storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
	}, time.Hour)
	require.NoError(t, err)
	workspaceID, err := fixture.env.Store.GetWorkspaceID(fixture.ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		name := common.FormatWorkspace(workspaceID) + "/qualityRules/" + resourceID
		_, deleteErr := fixture.dataQuality.DeleteQualityRule(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.DeleteQualityRuleRequest{Name: name}))
		require.NoError(t, deleteErr, "delete the scenario's orphan rule")
	})

	response, err := fixture.dataQuality.ExportQualityReport(fixture.ctx, withToken(fixture.env.AdminToken(),
		&v1pb.ExportQualityReportRequest{
			Parent:     "workspaces/-",
			TargetGuid: orphanGUID,
			MetaType:   v1pb.MetaType_TABLE,
			Language:   "en-US",
		}))
	require.NoError(t, err, "an orphaned target with rules stays reportable")
	document := response.Msg.GetHtmlContent()
	require.Contains(t, document, "no longer resolves")
	require.Contains(t, document, "orphan-instance/missing-db/public/gone", "the saved path snapshot still describes the target")
	require.Contains(t, document, "orphan row count")
	require.NotEmpty(t, response.Msg.GetFileName())
	require.Contains(t, response.Msg.GetFileName(), "quality-report-")
}

// The shared target assembly is what GetQualityTarget and the report both read.
// This pins the fields the extraction moved (latest run, run count, schedule,
// open issues, pending drafts) so a refactor cannot change the RPC silently, and
// checks that the newest of two runs is the one reported regardless of read order.
func TestDataQualityTargetAssemblyRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, true)
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Regex{
			Regex: &v1pb.RegexConfig{Column: "name", Pattern: "^z"},
		}})
	t.Cleanup(func() {
		_, err := fixture.dataQuality.DeleteQualityRule(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.DeleteQualityRuleRequest{Name: rule.GetName()}))
		require.NoError(t, err, "delete the scenario's rule")
	})

	// First run opens the issue; the second is the newest run the summary must
	// report even if a read orders the target's runs differently.
	fixture.runToCompletion(t, rule.GetName())
	secondExecutions := fixture.runRules(t, rule.GetName())
	require.Len(t, secondExecutions, 1)
	secondExecution := fixture.waitForCompletedExecution(t, secondExecutions[0].GetName())
	issue := fixture.waitForIssue(t, rule.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
	require.NotNil(t, issue)

	// A pending draft is seeded directly so the summary's draft tally is pinned
	// to the shared assembly rather than to a UI path.
	fixture.seedSuggestion(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		qualityDraftSuggestionPayload("draft from the assembly scenario"))

	response, err := fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
		}))
	require.NoError(t, err, "get the quality target")
	summary := response.Msg.GetSummary()
	require.Len(t, response.Msg.GetQualityRules(), 1, "the extracted assembly returns the target's rules")
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, response.Msg.GetQualityRules()[0].GetLastResult().GetStatus())
	require.NotEmpty(t, response.Msg.GetRuleHistory(), "the shared assembly still reads the per-rule history")
	require.Len(t, response.Msg.GetTargetColumns(), 3, "the resolved metadata still supplies the target's columns")
	require.Equal(t, secondExecution.GetRun(), summary.GetLatestRun().GetName(),
		"the summary reports the newest run, independent of read order")
	require.Equal(t, int32(2), summary.GetRunCount())
	require.NotNil(t, summary.GetSchedule())
	require.Positive(t, summary.GetSchedule().GetScheduleInterval().AsDuration())
	require.Len(t, response.Msg.GetOpenIssues(), 1)
	require.Equal(t, rule.GetName(), response.Msg.GetOpenIssues()[0].GetRule())
	require.Equal(t, int32(1), summary.GetPendingDraftCount())
	require.False(t, summary.GetOrphaned())
	require.Empty(t, response.Msg.GetOrphanedReason())

	// The standalone schedule RPC and the summary come from the same row.
	schedule, err := fixture.dataQuality.GetQualityTargetSchedule(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetScheduleRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
		}))
	require.NoError(t, err)
	require.Equal(t, schedule.Msg.GetScheduleInterval().AsDuration(),
		summary.GetSchedule().GetScheduleInterval().AsDuration())
}

// The 2 MiB cap is enforced on a real render: a failed execution carrying a wide
// sample produces a document over the cap, and the exported file is trimmed and
// says so in its body.
func TestDataQualityReportCapRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, true)
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		fixture.usersGUID, qualityRowCountConfig(1))
	t.Cleanup(func() {
		_, err := fixture.dataQuality.DeleteQualityRule(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.DeleteQualityRuleRequest{Name: rule.GetName()}))
		require.NoError(t, err, "delete the scenario's rule")
	})
	resourceID := rule.GetName()[strings.LastIndex(rule.GetName(), "/")+1:]
	stored, err := fixture.env.Store.GetQualityRule(fixture.ctx, &store.FindQualityRuleMessage{ResourceID: &resourceID})
	require.NoError(t, err)
	require.NotNil(t, stored)

	// Seed one completed failed execution whose stored sample is far wider than
	// any engine would record: 2000 columns x 5 rows, so even the capped model
	// renders past the document cap and exercises the trimming path.
	runResourceID, err := common.RandomString(12)
	require.NoError(t, err)
	run, err := fixture.env.Store.CreateQualityRun(fixture.ctx, &store.QualityRunMessage{
		ResourceID:         runResourceID,
		InstanceResourceID: fixture.instanceID,
		TargetGUID:         stored.TargetGUID,
		TargetType:         stored.TargetType,
		TargetPath:         stored.TargetPath,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Status:             storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	execution := &store.QualityExecutionMessage{
		RunID:              run.ID,
		RuleID:             stored.ID,
		RuleRevision:       stored.Revision,
		RuleEngineVersion:  "test",
		TargetGUID:         stored.TargetGUID,
		TargetType:         stored.TargetType,
		TargetPath:         stored.TargetPath,
		InstanceResourceID: fixture.instanceID,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		StartedAt:          now,
	}
	require.NoError(t, fixture.env.Store.PlanQualityExecutions(fixture.ctx, []*store.QualityExecutionMessage{execution}))
	require.NotZero(t, execution.ID)
	require.NoError(t, fixture.env.Store.MarkQualityExecutionRunning(fixture.ctx, execution.ID, now))

	cells := make([]*storepb.ColumnValue, 0, 2000)
	for index := 0; index < 2000; index++ {
		cells = append(cells, &storepb.ColumnValue{
			Column: fmt.Sprintf("column_%04d", index),
			Value:  strings.Repeat("a", 300),
		})
	}
	sample := &storepb.QualityFailureSamples{}
	for row := 0; row < 5; row++ {
		sample.Samples = append(sample.Samples, &storepb.FailureSample{Columns: cells})
	}
	require.NoError(t, fixture.env.Store.FinishQualityExecution(fixture.ctx, &store.QualityExecutionMessage{
		ID:         execution.ID,
		Status:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
		FailedRows: 5,
		TotalRows:  10,
		StartedAt:  now,
		FinishedAt: now,
		Sample:     sample,
	}))

	response, err := fixture.dataQuality.ExportQualityReport(fixture.ctx, withToken(fixture.env.AdminToken(),
		&v1pb.ExportQualityReportRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
			TrendDays:  7,
			Language:   "en-US",
		}))
	require.NoError(t, err, "the oversized report still renders")
	document := response.Msg.GetHtmlContent()
	require.LessOrEqual(t, len(document), 2<<20, "the exported document must respect the 2 MiB cap")
	require.Contains(t, document, "The report exceeded the 2 MiB size cap",
		"the trimmed document must say it was shortened")
	require.Contains(t, document, "</html>")
}

// withQualityReportLanguage overrides the request's language field.
func withQualityReportLanguage(request *v1pb.ExportQualityReportRequest, language string) *v1pb.ExportQualityReportRequest {
	request.Language = language
	return request
}

// withQualityReportTrendDays overrides the request's window.
func withQualityReportTrendDays(request *v1pb.ExportQualityReportRequest, days int32) *v1pb.ExportQualityReportRequest {
	request.TrendDays = days
	return request
}
