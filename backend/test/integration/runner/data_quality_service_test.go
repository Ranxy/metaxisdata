//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// qualityFixture is one MySQL instance the data-quality scenarios own. Every
// test gets its own instance, source database and table GUIDs, so the rules,
// runs, issues and notifications it creates cannot collide with another test's.
type qualityFixture struct {
	env            *integrationenv.ServiceEnv
	ctx            context.Context
	instanceID     string
	sourceDatabase string
	databaseName   string
	usersGUID      string
	dataQuality    v1connect.DataQualityServiceClient
	notifications  v1connect.NotificationServiceClient
	settings       v1connect.SettingServiceClient
}

// setupQualityMySQLFixture creates and syncs the MySQL fixture. A read-only data
// source is added when the scenario executes query rules; the metadata-rule and
// missing-source scenarios deliberately leave it out.
func setupQualityMySQLFixture(t *testing.T, withReadOnlyDataSource bool) *qualityFixture {
	t.Helper()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)
	instanceName := common.FormatInstance(instanceID)
	if withReadOnlyDataSource {
		_, err := env.CreateDataSource(ctx, instanceName, &v1pb.DataSource{
			Type:     v1pb.DataSourceType_READ_ONLY,
			Username: "root",
			Password: "root",
			Host:     env.MySQLHost,
			Port:     env.MySQLPort,
		}, "readonly", false)
		require.NoError(t, err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	return &qualityFixture{
		env:            env,
		ctx:            ctx,
		instanceID:     instanceID,
		sourceDatabase: sourceDatabase,
		databaseName:   databaseName,
		usersGUID:      waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "users"),
		dataQuality:    v1connect.NewDataQualityServiceClient(httpClient, env.BaseURL),
		notifications:  v1connect.NewNotificationServiceClient(httpClient, env.BaseURL),
		settings:       v1connect.NewSettingServiceClient(httpClient, env.BaseURL),
	}
}

// createRule creates one table-level rule through the public API.
func (f *qualityFixture) createRule(t *testing.T, ruleType v1pb.QualityRuleType, dimension v1pb.QualityDimension, severity v1pb.QualitySeverity, guid string, config *v1pb.QualityRuleConfig) *v1pb.QualityRule {
	t.Helper()

	resp, err := f.dataQuality.CreateQualityRule(f.ctx, withToken(f.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:       fmt.Sprintf("integration %s rule", ruleType.String()),
			Description: "created by the data quality integration scenarios",
			Dimension:   dimension,
			RuleType:    ruleType,
			Target:      &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config:      config,
			Severity:    severity,
			// schedule_interval is omitted on purpose: the workspace default applies,
			// which is what the SPA does when its settings have not been touched.
		},
	}))
	require.NoError(t, err, "create quality rule")
	return resp.Msg
}

// runRules starts one manual run for the named rules and returns the queued runs.
func (f *qualityFixture) runRules(t *testing.T, ruleNames ...string) []*v1pb.QualityRun {
	t.Helper()

	resp, err := f.dataQuality.RunQualityRules(f.ctx, withToken(f.env.AdminToken(), &v1pb.RunQualityRulesRequest{
		Parent: "workspaces/-",
		Rules:  ruleNames,
	}))
	require.NoError(t, err, "run quality rules")
	require.NotEmpty(t, resp.Msg.GetRuns(), "a manual run creates at least one run")
	return resp.Msg.GetRuns()
}

// waitForCompletedRun polls one run until the runner finished it.
func (f *qualityFixture) waitForCompletedRun(t *testing.T, runName string) *v1pb.QualityRun {
	t.Helper()

	var run *v1pb.QualityRun
	require.Eventually(t, func() bool {
		resp, err := f.dataQuality.GetQualityRun(f.ctx, withToken(f.env.AdminToken(), &v1pb.GetQualityRunRequest{Name: runName}))
		if err != nil {
			return false
		}
		run = resp.Msg
		return run.GetStatus() == v1pb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED
	}, 60*time.Second, 250*time.Millisecond, "quality run %s never completed: %s", runName, f.env.ServerLogs())
	return run
}

// runToCompletion starts one manual run and waits for it.
func (f *qualityFixture) runToCompletion(t *testing.T, ruleNames ...string) *v1pb.QualityRun {
	t.Helper()

	runs := f.runRules(t, ruleNames...)
	require.Len(t, runs, 1, "all selected rules live in one instance")
	return f.waitForCompletedRun(t, runs[0].GetName())
}

// resultsForRun lists the results of one run.
func (f *qualityFixture) resultsForRun(t *testing.T, runName string) []*v1pb.QualityRuleResult {
	t.Helper()

	resp, err := f.dataQuality.ListQualityRuleResults(f.ctx, withToken(f.env.AdminToken(), &v1pb.ListQualityRuleResultsRequest{
		Parent:   "workspaces/-",
		Run:      runName,
		PageSize: 100,
	}))
	require.NoError(t, err, "list the run's results")
	return resp.Msg.GetQualityRuleResults()
}

// resultsForRule lists a rule's results, newest first.
func (f *qualityFixture) resultsForRule(t *testing.T, ruleName string) []*v1pb.QualityRuleResult {
	t.Helper()

	resp, err := f.dataQuality.ListQualityRuleResults(f.ctx, withToken(f.env.AdminToken(), &v1pb.ListQualityRuleResultsRequest{
		Parent:   "workspaces/-",
		Rule:     ruleName,
		PageSize: 100,
	}))
	require.NoError(t, err, "list the rule's results")
	return resp.Msg.GetQualityRuleResults()
}

// listIssues reads one rule's issues in a given state, retrying store write
// visibility inside caller-side Eventually loops.
func (f *qualityFixture) listIssues(t *testing.T, ruleName string, status v1pb.QualityIssueStatus) ([]*v1pb.QualityIssue, error) {
	t.Helper()

	resp, err := f.dataQuality.ListQualityIssues(f.ctx, withToken(f.env.AdminToken(), &v1pb.ListQualityIssuesRequest{
		Parent:   "workspaces/-",
		Rule:     ruleName,
		Status:   status,
		PageSize: 100,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetQualityIssues(), nil
}

// waitForIssue waits for one issue of the rule to reach the given state.
func (f *qualityFixture) waitForIssue(t *testing.T, ruleName string, status v1pb.QualityIssueStatus) *v1pb.QualityIssue {
	t.Helper()

	var issue *v1pb.QualityIssue
	require.Eventually(t, func() bool {
		issues, err := f.listIssues(t, ruleName, status)
		if err != nil || len(issues) == 0 {
			return false
		}
		issue = issues[0]
		return true
	}, 30*time.Second, 200*time.Millisecond, "the rule's %s issue never appeared: %s", status.String(), f.env.ServerLogs())
	return issue
}

// waitForNotification waits for a data-quality notification of one kind about
// this rule. Messages are deduplicated per hour bucket, so a unique rule makes the
// match unambiguous even though the inbox is shared.
func (f *qualityFixture) waitForNotification(t *testing.T, kind v1pb.DataQualityNotificationKind, ruleName string) *v1pb.Notification {
	t.Helper()

	var message *v1pb.Notification
	require.Eventually(t, func() bool {
		resp, err := f.notifications.ListNotifications(f.ctx, withToken(f.env.AdminToken(), &v1pb.ListNotificationsRequest{
			Parent:   "workspaces/-",
			PageSize: 100,
		}))
		if err != nil {
			return false
		}
		for _, candidate := range resp.Msg.GetNotifications() {
			if candidate.GetType() != v1pb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY {
				continue
			}
			detail := candidate.GetDataQuality()
			if detail.GetKind() == kind && detail.GetRule() == ruleName {
				message = candidate
				return true
			}
		}
		return false
	}, 30*time.Second, 200*time.Millisecond, "the %s notification about %s never arrived: %s", kind.String(), ruleName, f.env.ServerLogs())
	return message
}

// qualityRowCountConfig builds a row-count config with one fail bound.
func qualityRowCountConfig(minRows int64) *v1pb.QualityRuleConfig {
	return &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{
		MinRows: &minRows,
	}}}
}

// TestDataQualityRuleLifecycleRealServerIntegration drives one rule from creation
// through preview, execution, result reading, revision bump and archival, which is
// the whole M1.4 surface against the real runner.
func TestDataQualityRuleLifecycleRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	token := func() string { return fixture.env.AdminToken() }

	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		fixture.usersGUID, qualityRowCountConfig(1))
	require.NotEmpty(t, rule.GetName())
	require.Equal(t, int32(1), rule.GetRevision())
	require.Nil(t, rule.GetLastResult(), "a fresh rule has no result")

	// Read it back: the target snapshot is filled from the registry and there is
	// still no result.
	read, err := fixture.dataQuality.GetQualityRule(fixture.ctx, withToken(token(), &v1pb.GetQualityRuleRequest{Name: rule.GetName()}))
	require.NoError(t, err)
	require.Equal(t, rule.GetName(), read.Msg.GetName())
	require.Equal(t, v1pb.MetaType_TABLE, read.Msg.GetTarget().GetMetaType())
	require.Equal(t, fixture.usersGUID, read.Msg.GetTarget().GetMetaGuid())
	require.Nil(t, read.Msg.GetLastResult())
	require.False(t, read.Msg.GetTargetSchemaChanged())

	listed, err := fixture.dataQuality.ListQualityRules(fixture.ctx, withToken(token(), &v1pb.ListQualityRulesRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
	}))
	require.NoError(t, err)
	require.Contains(t, qualityRuleNames(listed.Msg.GetQualityRules()), rule.GetName())

	// Preview compiles and probes the rule without creating a run or a result.
	preview, err := fixture.dataQuality.PreviewQualityRule(fixture.ctx, withToken(token(), &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			RuleType: v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:   &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:   qualityRowCountConfig(1),
		},
	}))
	require.NoError(t, err)
	require.NotEmpty(t, preview.Msg.GetCompiledSql(), "the preview shows the compiled SQL")
	require.Empty(t, preview.Msg.GetParameters(), "a row count binds no parameters")

	noResultsAfterPreview := fixture.resultsForRule(t, rule.GetName())
	require.Empty(t, noResultsAfterPreview, "a preview is not an execution")

	// Run it: the API answers with queued runs, and the runner finishes the run.
	runs := fixture.runRules(t, rule.GetName())
	require.Len(t, runs, 1)
	require.Equal(t, v1pb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING, runs[0].GetStatus())
	require.Equal(t, "instances/"+fixture.instanceID, runs[0].GetInstance())
	require.Equal(t, v1pb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL, runs[0].GetTrigger())

	finished := fixture.waitForCompletedRun(t, runs[0].GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, finished.GetOutcome(),
		"the users fixture has one row, which meets min_rows=1")
	require.NotNil(t, finished.GetFinishTime())

	results := fixture.resultsForRun(t, runs[0].GetName())
	require.Len(t, results, 1)
	result := results[0]
	require.Equal(t, rule.GetName(), result.GetRule())
	require.Equal(t, runs[0].GetName(), result.GetRun())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, result.GetStatus())
	require.Equal(t, int32(1), result.GetRuleRevision())
	require.Equal(t, int64(1), result.GetTotalRows())
	require.Equal(t, int64(0), result.GetFailedRows())
	require.NotEmpty(t, result.GetExecutedSql(), "a query rule records the SQL it ran")
	require.True(t, result.GetFinishTime().AsTime().After(result.GetStartTime().AsTime()))

	// The list now carries the latest result summary.
	afterRun, err := fixture.dataQuality.GetQualityRule(fixture.ctx, withToken(token(), &v1pb.GetQualityRuleRequest{Name: rule.GetName()}))
	require.NoError(t, err)
	require.NotNil(t, afterRun.Msg.GetLastResult())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, afterRun.Msg.GetLastResult().GetStatus())
	require.Equal(t, runs[0].GetName(), afterRun.Msg.GetLastResult().GetRun())

	// A definition edit bumps the revision; the historical result keeps the
	// revision it was judged with.
	updated, err := fixture.dataQuality.UpdateQualityRule(fixture.ctx, withToken(token(), &v1pb.UpdateQualityRuleRequest{
		Rule: &v1pb.QualityRule{
			Name:     rule.GetName(),
			Config:   qualityRowCountConfig(1000),
			Revision: rule.GetRevision(),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config"}},
	}))
	require.NoError(t, err)
	require.Equal(t, int32(2), updated.Msg.GetRevision(), "a semantic edit appends a revision")
	require.Equal(t, int64(1000), updated.Msg.GetConfig().GetRowCount().GetMinRows())

	oldResults := fixture.resultsForRule(t, rule.GetName())
	require.Len(t, oldResults, 1, "the edit does not rewrite history")
	require.Equal(t, int32(1), oldResults[0].GetRuleRevision())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, oldResults[0].GetStatus())

	// Archiving removes the rule from the default list but keeps it readable and
	// discoverable through the ARCHIVED filter.
	_, err = fixture.dataQuality.DeleteQualityRule(fixture.ctx, withToken(token(), &v1pb.DeleteQualityRuleRequest{Name: rule.GetName()}))
	require.NoError(t, err)

	afterArchive, err := fixture.dataQuality.ListQualityRules(fixture.ctx, withToken(token(), &v1pb.ListQualityRulesRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
	}))
	require.NoError(t, err)
	require.NotContains(t, qualityRuleNames(afterArchive.Msg.GetQualityRules()), rule.GetName())

	archived, err := fixture.dataQuality.ListQualityRules(fixture.ctx, withToken(token(), &v1pb.ListQualityRulesRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
		State:    v1pb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED,
	}))
	require.NoError(t, err)
	require.Contains(t, qualityRuleNames(archived.Msg.GetQualityRules()), rule.GetName())

	archiveRead, err := fixture.dataQuality.GetQualityRule(fixture.ctx, withToken(token(), &v1pb.GetQualityRuleRequest{Name: rule.GetName()}))
	require.NoError(t, err, "an archived rule stays readable so its history can be inspected")
	require.Equal(t, v1pb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED, archiveRead.Msg.GetState())
	require.Len(t, fixture.resultsForRule(t, rule.GetName()), 1, "archiving keeps the results")
}

// TestDataQualityIssueLifecycleAndNotificationsRealServerIntegration runs a rule
// that must fail, checks the problem ledger and the issue-opened message, then
// repairs the rule and checks the automatic recovery and its message.
func TestDataQualityIssueLifecycleAndNotificationsRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)

	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, qualityRowCountConfig(1000))

	failedRun := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, failedRun.GetOutcome())
	results := fixture.resultsForRun(t, failedRun.GetName())
	require.Len(t, results, 1)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, results[0].GetStatus())
	// A row-count verdict is the observed count, not a number of failed rows: the
	// whole table is one observation, and min_rows=1000 puts it out of range.
	require.Equal(t, float64(1), results[0].GetObservedValue())
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, results[0].GetObservedKind())

	// A FAIL opens exactly one unresolved problem, and the problem names the run
	// that first saw it.
	issue := fixture.waitForIssue(t, rule.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
	require.Equal(t, rule.GetName(), issue.GetRule())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR, issue.GetSeverity())
	require.Equal(t, fixture.usersGUID, issue.GetTarget().GetMetaGuid())
	require.Equal(t, failedRun.GetName(), issue.GetFirstSeenRun())
	require.Equal(t, int32(1), issue.GetOccurrenceCount())
	require.Nil(t, issue.GetResolvedTime())

	openedNotice := fixture.waitForNotification(t,
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, rule.GetName())
	detail := openedNotice.GetDataQuality()
	require.Equal(t, issue.GetName(), detail.GetIssue())
	require.Equal(t, failedRun.GetName(), detail.GetRun())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, detail.GetToStatus())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, detail.GetFromStatus())

	// Repairing the rule (a passing bound) makes the next run recover the issue.
	_, err := fixture.dataQuality.UpdateQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.UpdateQualityRuleRequest{
		Rule: &v1pb.QualityRule{
			Name:     rule.GetName(),
			Config:   qualityRowCountConfig(1),
			Revision: rule.GetRevision(),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config"}},
	}))
	require.NoError(t, err)

	recoveredRun := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, recoveredRun.GetOutcome())

	resolved := fixture.waitForIssue(t, rule.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)
	require.Equal(t, issue.GetName(), resolved.GetName(), "the same issue is resolved, not replaced")
	require.NotNil(t, resolved.GetResolvedTime())
	require.Equal(t, v1pb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED, resolved.GetResolutionReason())

	recoveredNotice := fixture.waitForNotification(t,
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED, rule.GetName())
	require.Equal(t, issue.GetName(), recoveredNotice.GetDataQuality().GetIssue())
	require.Equal(t, recoveredRun.GetName(), recoveredNotice.GetDataQuality().GetRun())
}

// TestDataQualityMetadataAndReadOnlyRulesRealServerIntegration covers the two
// execution paths that differ from an ordinary query rule: schema drift observes
// the registry and must run without a read-only data source, and a query rule
// without one must record ERROR(NO_READ_ONLY_SOURCE) rather than silently fail.
func TestDataQualityMetadataAndReadOnlyRulesRealServerIntegration(t *testing.T) {
	t.Parallel()

	// No READ_ONLY data source on purpose: this instance only has the admin one.
	fixture := setupQualityMySQLFixture(t, false)

	// The server fills the schema snapshot from the current registry columns.
	driftRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
		v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_SchemaDrift{SchemaDrift: &v1pb.SchemaDriftConfig{}}})
	expected := driftRule.GetConfig().GetSchemaDrift().GetExpectedColumns()
	require.NotEmpty(t, expected, "the API fills the snapshot when the request leaves it empty")
	expectedNames := make([]string, 0, len(expected))
	for _, column := range expected {
		require.NotEmpty(t, column.GetName())
		require.NotEmpty(t, column.GetType())
		expectedNames = append(expectedNames, column.GetName())
	}
	require.Equal(t, []string{"id", "name", "age"}, expectedNames, "the snapshot keeps the registry's column order")

	// The metadata run needs no connection: it compares the registry snapshot with
	// the registry's current columns and passes.
	driftRun := fixture.runToCompletion(t, driftRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, driftRun.GetOutcome())
	driftResults := fixture.resultsForRun(t, driftRun.GetName())
	require.Len(t, driftResults, 1)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, driftResults[0].GetStatus())
	require.Empty(t, driftResults[0].GetExecutedSql(), "a metadata rule runs no SQL")
	require.Equal(t, int64(len(expected)), driftResults[0].GetTotalRows())

	// A column the snapshot does not know is drift the next time the rule runs.
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx, fmt.Sprintf(
		"ALTER TABLE %s.users ADD COLUMN nickname VARCHAR(32) NULL;", quoteMySQLIdentifier(fixture.sourceDatabase))))
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)

	driftedRun := fixture.runToCompletion(t, driftRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, driftedRun.GetOutcome())
	driftedResults := fixture.resultsForRun(t, driftedRun.GetName())
	require.Len(t, driftedResults, 1)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, driftedResults[0].GetStatus())
	require.GreaterOrEqual(t, driftedResults[0].GetFailedRows(), int64(1))
	require.NotEmpty(t, driftedResults[0].GetSamples(), "the drift items are kept as evidence")
	require.Empty(t, driftedResults[0].GetExecutedSql())

	// A query rule on the same instance cannot even be created: the missing
	// READ_ONLY source is a configuration mistake, so the save is refused with an
	// actionable message instead of a rule that can only ever record ERROR.
	_, err := fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "query rule without a read-only source",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:    &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    qualityRowCountConfig(1),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "READ_ONLY")
}

// TestDataQualityColumnProfileAndLabelRealServerIntegration covers the
// deterministic profile path and the manual label lifecycle (M3b).
func TestDataQualityColumnProfileAndLabelRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)

	profiled, err := fixture.dataQuality.ProfileColumns(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Len(t, profiled.Msg.GetColumnProfiles(), 3, "the fixture table has three columns")
	for _, profile := range profiled.Msg.GetColumnProfiles() {
		require.Equal(t, fixture.usersGUID, profile.GetTableGuid())
		require.NotNil(t, profile.GetProfile())
		require.Equal(t, int64(1), profile.GetProfile().GetTotalRows())
		require.NotEmpty(t, profile.GetSampleMethod())
		require.NotEmpty(t, profile.GetEngineVersion())
		require.NotNil(t, profile.GetProfiledAt())
	}

	listedProfiles, err := fixture.dataQuality.ListColumnProfiles(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListColumnProfilesRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{"name"},
	}))
	require.NoError(t, err)
	require.Len(t, listedProfiles.Msg.GetColumnProfiles(), 1)
	require.Equal(t, "name", listedProfiles.Msg.GetColumnProfiles()[0].GetColumn())

	// There is no label yet, then a manual accept stores one and clear removes it.
	labels, err := fixture.dataQuality.ListColumnLabels(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListColumnLabelsRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Empty(t, labels.Msg.GetColumnLabels())

	accepted, err := fixture.dataQuality.UpdateColumnLabel(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.UpdateColumnLabelRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Column:    "name",
		Label:     v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT,
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT,
	}))
	require.NoError(t, err)
	require.NotNil(t, accepted.Msg.GetColumnLabel())
	require.Equal(t, v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT, accepted.Msg.GetColumnLabel().GetLabel())
	require.Equal(t, v1pb.QualityLabelSource_QUALITY_LABEL_SOURCE_MANUAL, accepted.Msg.GetColumnLabel().GetSource())
	require.Equal(t, v1pb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED, accepted.Msg.GetColumnLabel().GetStatus())
	require.NotEmpty(t, accepted.Msg.GetColumnLabel().GetAcceptedBy())

	labels, err = fixture.dataQuality.ListColumnLabels(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListColumnLabelsRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Len(t, labels.Msg.GetColumnLabels(), 1)
	require.Equal(t, "name", labels.Msg.GetColumnLabels()[0].GetColumn())

	cleared, err := fixture.dataQuality.UpdateColumnLabel(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.UpdateColumnLabelRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Column:    "name",
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_CLEAR,
	}))
	require.NoError(t, err)
	require.Nil(t, cleared.Msg.GetColumnLabel())

	labels, err = fixture.dataQuality.ListColumnLabels(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListColumnLabelsRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Empty(t, labels.Msg.GetColumnLabels())
}

// TestDataQualityLLMAnalysisIsOffByDefaultRealServerIntegration pins the switch:
// with the workspace setting untouched, both model-backed suggestion RPCs refuse
// with FailedPrecondition instead of calling a provider.
func TestDataQualityLLMAnalysisIsOffByDefaultRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)

	settings, err := fixture.settings.GetWorkspaceProfileSetting(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.GetWorkspaceProfileSettingRequest{}))
	require.NoError(t, err)
	if settings.Msg.GetDataQuality().GetEnableLlmAnalysis() {
		t.Skip("the workspace has LLM analysis enabled; this scenario pins the disabled default")
	}

	_, err = fixture.dataQuality.SuggestQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.SuggestQualityRuleRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Intent:    "the email column must not be empty",
		Language:  "en",
	}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "LLM analysis is disabled by default: %v", err)

	_, err = fixture.dataQuality.SuggestColumnLabels(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.SuggestColumnLabelsRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{"name"},
		Language:  "en",
	}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "LLM analysis is disabled by default: %v", err)
}

// qualityRuleNames returns the resource names of a rule page.
func qualityRuleNames(rules []*v1pb.QualityRule) []string {
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.GetName())
	}
	return names
}

// TestDataQualityParameterizedRuleRealServerIntegration covers the path a
// parameterized rule takes: the compiled SQL carries placeholders, the preview
// sends its bound arguments, and execution sends them again. A rule whose SQL
// and arguments were not sent together used to fail the dry run with
// "expected N arguments, got 0".
func TestDataQualityParameterizedRuleRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	minValue := "0"
	maxValue := "200"
	config := &v1pb.QualityRuleConfig{
		Config: &v1pb.QualityRuleConfig_Range{Range: &v1pb.RangeConfig{
			Column: "age",
			Min:    &minValue,
			Max:    &maxValue,
		}},
		Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{
			{Column: "id", Op: v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT, Values: []string{"0"}},
		}},
	}
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, config)

	// The dry run is the reported failure path: it must send the arguments.
	preview, err := fixture.dataQuality.PreviewQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule:   rule,
	}))
	require.NoError(t, err, "a parameterized rule must preview successfully")
	require.NotEmpty(t, preview.Msg.GetCompiledSql())
	require.NotEmpty(t, preview.Msg.GetParameters(), "the preview names the bound values")

	run := fixture.runToCompletion(t, rule.GetName())
	results := fixture.resultsForRun(t, run.GetName())
	require.Len(t, results, 1)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, results[0].GetStatus(), "the fixture's age is within 0..200")
	require.NotEmpty(t, results[0].GetExecutedSql())
	require.Equal(t, int64(1), results[0].GetTotalRows())
}
