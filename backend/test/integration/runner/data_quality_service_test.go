//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
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

// runRules plans and queues one execution per named rule and returns them.
func (f *qualityFixture) runRules(t *testing.T, ruleNames ...string) []*v1pb.QualityExecution {
	t.Helper()

	resp, err := f.dataQuality.RunQualityRules(f.ctx, withToken(f.env.AdminToken(), &v1pb.RunQualityRulesRequest{
		Parent: "workspaces/-",
		Rules:  ruleNames,
	}))
	require.NoError(t, err, "run quality rules")
	require.NotEmpty(t, resp.Msg.GetExecutions(), "a manual run plans at least one execution")
	require.Len(t, resp.Msg.GetExecutions(), len(ruleNames), "one execution per selected rule")
	return resp.Msg.GetExecutions()
}

// waitForCompletedExecution polls one execution until the runner finished it.
func (f *qualityFixture) waitForCompletedExecution(t *testing.T, executionName string) *v1pb.QualityExecution {
	t.Helper()

	var execution *v1pb.QualityExecution
	require.Eventually(t, func() bool {
		resp, err := f.dataQuality.GetQualityExecution(f.ctx, withToken(f.env.AdminToken(), &v1pb.GetQualityExecutionRequest{Name: executionName}))
		if err != nil {
			return false
		}
		execution = resp.Msg
		return execution.GetState() == v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED
	}, 60*time.Second, 250*time.Millisecond, "quality execution %s never completed: %s", executionName, f.env.ServerLogs())
	return execution
}

// runToCompletion starts one manual execution and waits for it.
func (f *qualityFixture) runToCompletion(t *testing.T, ruleName string) *v1pb.QualityExecution {
	t.Helper()

	executions := f.runRules(t, ruleName)
	require.Len(t, executions, 1)
	return f.waitForCompletedExecution(t, executions[0].GetName())
}

// executionsForRule lists a rule's executions, newest first.
func (f *qualityFixture) executionsForRule(t *testing.T, ruleName string) []*v1pb.QualityExecution {
	t.Helper()

	resp, err := f.dataQuality.ListQualityExecutions(f.ctx, withToken(f.env.AdminToken(), &v1pb.ListQualityExecutionsRequest{
		Parent:   "workspaces/-",
		Rule:     ruleName,
		PageSize: 100,
	}))
	require.NoError(t, err, "list the rule's executions")
	return resp.Msg.GetQualityExecutions()
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

// seedSuggestion stores one pending suggestion directly: the LLM RPCs are off by
// default, so the review-queue scenarios seed the rows a model would have
// proposed.
func (f *qualityFixture) seedSuggestion(t *testing.T, kind storepb.QualitySuggestionKind, payload *storepb.QualitySuggestionPayload) *store.QualitySuggestionMessage {
	t.Helper()

	resourceID, err := common.RandomString(12)
	require.NoError(t, err)
	suggestion, err := f.env.Store.CreateQualitySuggestion(f.ctx, &store.QualitySuggestionMessage{
		ResourceID:    resourceID,
		Kind:          kind,
		Source:        storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
		TargetGUID:    f.usersGUID,
		TargetType:    storepb.MetaType_TABLE,
		Payload:       payload,
		Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Provider:      "profiles/integration",
		Model:         "integration-model",
		PromptVersion: "dq.rule_draft.v1",
	})
	require.NoError(t, err)
	return suggestion
}

// qualityDraftSuggestionPayload builds a pending rule-draft payload: a row-count
// rule the fixture table satisfies.
func qualityDraftSuggestionPayload(title string) *storepb.QualitySuggestionPayload {
	minRows := int64(1)
	return &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{
		Rule: &storepb.QualityRuleDraftConfig{
			Title:      title,
			Rationale:  "the fixture table always has at least one row",
			Confidence: 0.9,
			Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			Config: &storepb.QualityRuleConfig{
				Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: &minRows}},
			},
		},
	}}
}

// qualityLabelSuggestionPayload builds a pending semantic-label proposal.
func qualityLabelSuggestionPayload(column string, label storepb.QualitySemanticType) *storepb.QualitySuggestionPayload {
	return &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Label{
		Label: &storepb.SemanticLabelSuggestion{
			Column:     column,
			Label:      label,
			Confidence: 0.8,
		},
	}}
}

// qualitySuggestionResourceName renders one suggestion's public resource name.
func qualitySuggestionResourceName(workspaceID, resourceID string) string {
	return common.FormatWorkspace(workspaceID) + "/qualitySuggestions/" + resourceID
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
	require.Nil(t, rule.GetLastResult(), "a fresh rule has no execution summary")

	// Read it back: the target snapshot is filled from the registry and there is
	// still no execution summary.
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

	// Preview compiles and probes the rule without creating an execution.
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

	noExecutionsAfterPreview := fixture.executionsForRule(t, rule.GetName())
	require.Empty(t, noExecutionsAfterPreview, "a preview is not an execution")

	// Run it: the API answers with planned PENDING executions, and the runner
	// finishes each one.
	executions := fixture.runRules(t, rule.GetName())
	require.Len(t, executions, 1)
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, executions[0].GetState())
	require.Equal(t, "instances/"+fixture.instanceID, executions[0].GetInstance())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL, executions[0].GetTrigger())

	finished := fixture.waitForCompletedExecution(t, executions[0].GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, finished.GetOutcome(),
		"the users fixture has one row, which meets min_rows=1")
	require.Equal(t, rule.GetName(), finished.GetRule())
	require.Equal(t, int32(1), finished.GetRuleRevision())
	require.Equal(t, int64(1), finished.GetTotalRows())
	require.Equal(t, int64(0), finished.GetFailedRows())
	require.NotEmpty(t, finished.GetExecutedSql(), "a query rule records the SQL it ran")
	require.NotNil(t, finished.GetFinishTime())
	require.True(t, finished.GetFinishTime().AsTime().After(finished.GetStartTime().AsTime()))

	// The list now carries the latest execution summary.
	afterRun, err := fixture.dataQuality.GetQualityRule(fixture.ctx, withToken(token(), &v1pb.GetQualityRuleRequest{Name: rule.GetName()}))
	require.NoError(t, err)
	require.NotNil(t, afterRun.Msg.GetLastResult())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, afterRun.Msg.GetLastResult().GetStatus())
	require.Equal(t, executions[0].GetName(), afterRun.Msg.GetLastResult().GetExecution())

	// A definition edit bumps the revision; the historical execution keeps the
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

	oldExecutions := fixture.executionsForRule(t, rule.GetName())
	require.Len(t, oldExecutions, 1, "the edit does not rewrite history")
	require.Equal(t, int32(1), oldExecutions[0].GetRuleRevision())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, oldExecutions[0].GetOutcome())

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
	require.Len(t, fixture.executionsForRule(t, rule.GetName()), 1, "archiving keeps the executions")
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

	failedExecution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, failedExecution.GetOutcome())
	// A row-count verdict is the observed count, not a number of failed rows: the
	// whole table is one observation, and min_rows=1000 puts it out of range.
	require.Equal(t, float64(1), failedExecution.GetObservedValue())
	require.Equal(t, v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, failedExecution.GetObservedKind())

	// A FAIL opens exactly one unresolved problem, and the problem names the
	// execution that first saw it.
	issue := fixture.waitForIssue(t, rule.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
	require.Equal(t, rule.GetName(), issue.GetRule())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR, issue.GetSeverity())
	require.Equal(t, fixture.usersGUID, issue.GetTarget().GetMetaGuid())
	require.Equal(t, failedExecution.GetName(), issue.GetFirstSeenExecution())
	require.Equal(t, int32(1), issue.GetOccurrenceCount())
	require.Nil(t, issue.GetResolvedTime())

	openedNotice := fixture.waitForNotification(t,
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, rule.GetName())
	detail := openedNotice.GetDataQuality()
	require.Equal(t, issue.GetName(), detail.GetIssue())
	require.Equal(t, failedExecution.GetName(), detail.GetExecution())
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

	recoveredExecution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, recoveredExecution.GetOutcome())

	resolved := fixture.waitForIssue(t, rule.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED)
	require.Equal(t, issue.GetName(), resolved.GetName(), "the same issue is resolved, not replaced")
	require.NotNil(t, resolved.GetResolvedTime())
	require.Equal(t, v1pb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED, resolved.GetResolutionReason())

	recoveredNotice := fixture.waitForNotification(t,
		v1pb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED, rule.GetName())
	require.Equal(t, issue.GetName(), recoveredNotice.GetDataQuality().GetIssue())
	require.Equal(t, recoveredExecution.GetName(), recoveredNotice.GetDataQuality().GetExecution())
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
	driftExecution := fixture.runToCompletion(t, driftRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, driftExecution.GetOutcome())
	require.Empty(t, driftExecution.GetExecutedSql(), "a metadata rule runs no SQL")
	require.Equal(t, int64(len(expected)), driftExecution.GetTotalRows())

	// A column the snapshot does not know is drift the next time the rule runs.
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx, fmt.Sprintf(
		"ALTER TABLE %s.users ADD COLUMN nickname VARCHAR(32) NULL;", quoteMySQLIdentifier(fixture.sourceDatabase))))
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)

	driftedExecution := fixture.runToCompletion(t, driftRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, driftedExecution.GetOutcome())
	require.GreaterOrEqual(t, driftedExecution.GetFailedRows(), int64(1))
	require.NotEmpty(t, driftedExecution.GetSamples(), "the drift items are kept as evidence")
	require.Empty(t, driftedExecution.GetExecutedSql())

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

// TestDataQualitySuggestionCenterRealServerIntegration covers the suggestion
// center's batch surface: creating rules from drafts (single and batch), reading
// and filtering the queue, and deciding many suggestions in one call.
func TestDataQualitySuggestionCenterRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	token := fixture.env.AdminToken()
	client := fixture.dataQuality

	workspaceID, err := fixture.env.Store.GetWorkspaceID(fixture.ctx)
	require.NoError(t, err)
	name := func(suggestion *store.QualitySuggestionMessage) string {
		return qualitySuggestionResourceName(workspaceID, suggestion.ResourceID)
	}

	draftValid := fixture.seedSuggestion(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		qualityDraftSuggestionPayload("users row count"))
	draftInvalid := fixture.seedSuggestion(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		qualityDraftSuggestionPayload("a draft whose rule request is invalid"))
	draftSingle := fixture.seedSuggestion(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		qualityDraftSuggestionPayload("a draft created through CreateQualityRule"))
	labelSuggestion := fixture.seedSuggestion(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		qualityLabelSuggestionPayload("name", storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT))

	// An empty batch is a malformed request and fails the whole call.
	_, err = client.BatchCreateQualityRules(fixture.ctx, withToken(token, &v1pb.BatchCreateQualityRulesRequest{
		Parent: "workspaces/-",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Batch creation: the valid item creates its rule and accepts its draft; the
	// invalid item fails on its own and leaves its draft pending for a reviewer.
	batch, err := client.BatchCreateQualityRules(fixture.ctx, withToken(token, &v1pb.BatchCreateQualityRulesRequest{
		Parent: "workspaces/-",
		Requests: []*v1pb.CreateQualityRuleRequest{
			{
				Parent: "workspaces/-",
				Rule: &v1pb.QualityRule{
					Title:     "batch row count from a draft",
					Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
					RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
					Target:    &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE},
					Config:    qualityRowCountConfig(1),
					Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
				},
				Suggestion: name(draftValid),
			},
			{
				Parent: "workspaces/-",
				Rule: &v1pb.QualityRule{
					Title:     "batch row count with an invalid config",
					Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
					RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
					Target:    &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE},
					// No bound at all: the server refuses this item on its own.
					Config:   &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{}}},
					Severity: v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
				},
				Suggestion: name(draftInvalid),
			},
		},
	}))
	require.NoError(t, err)
	require.Len(t, batch.Msg.GetResults(), 2, "one result per request, in request order")
	require.Equal(t, name(draftValid), batch.Msg.GetResults()[0].GetSuggestion(), "the result echoes the suggestion it applies")
	require.Equal(t, name(draftInvalid), batch.Msg.GetResults()[1].GetSuggestion())
	created := batch.Msg.GetResults()[0].GetRule()
	require.NotNil(t, created, "the valid item created a rule")
	require.Empty(t, batch.Msg.GetResults()[0].GetError())
	require.Equal(t, fixture.usersGUID, created.GetTarget().GetMetaGuid())
	require.Nil(t, batch.Msg.GetResults()[1].GetRule(), "a failed item reports no rule")
	require.NotEmpty(t, batch.Msg.GetResults()[1].GetError())

	// The created rule exists, it is not just echoed in the response.
	read, err := client.GetQualityRule(fixture.ctx, withToken(token, &v1pb.GetQualityRuleRequest{Name: created.GetName()}))
	require.NoError(t, err)
	require.Equal(t, created.GetName(), read.Msg.GetName())
	require.Equal(t, fixture.usersGUID, read.Msg.GetTarget().GetMetaGuid())

	// The accepted draft is decided as a side effect; the failed item's draft is
	// still waiting for a reviewer.
	acceptedDraft, err := client.GetQualitySuggestion(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{Name: name(draftValid)}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, acceptedDraft.Msg.GetStatus())
	require.Equal(t, fixture.usersGUID, acceptedDraft.Msg.GetTarget().GetMetaGuid())

	pendingDraft, err := client.GetQualitySuggestion(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{Name: name(draftInvalid)}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, pendingDraft.Msg.GetStatus())

	// A deep link that names no suggestion is NotFound instead of a list read.
	_, err = client.GetQualitySuggestion(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{
		Name: qualitySuggestionResourceName(workspaceID, "doesnotexist"),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Creating a rule from a suggestion refuses a request target that names a
	// different table: the draft stays pending and no rule is created.
	_, err = client.CreateQualityRule(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "direct create from a draft",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:    &v1pb.QualityTarget{MetaGuid: "stale;target", MetaType: v1pb.MetaType_VIEW},
			Config:    qualityRowCountConfig(1),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
		Suggestion: name(draftSingle),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	stillPending, err := client.GetQualitySuggestion(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{Name: name(draftSingle)}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, stillPending.Msg.GetStatus())

	// A matching target creates the rule and accepts the draft.
	single, err := client.CreateQualityRule(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "direct create from a draft",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:    &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    qualityRowCountConfig(1),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
		Suggestion: name(draftSingle),
	}))
	require.NoError(t, err)
	require.Equal(t, fixture.usersGUID, single.Msg.GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.MetaType_TABLE, single.Msg.GetTarget().GetMetaType())

	decidedDraft, err := client.GetQualitySuggestion(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{Name: name(draftSingle)}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, decidedDraft.Msg.GetStatus())

	// An already-decided draft cannot create a second rule.
	_, err = client.CreateQualityRule(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "a second rule from the same draft",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Config:    qualityRowCountConfig(1),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
		Suggestion: name(draftSingle),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeAborted, connect.CodeOf(err))

	// The path filters are GUID prefixes. All seeded suggestions share this
	// fixture's table GUID, so the filters narrow to them and nothing else.
	list := func(t *testing.T, request *v1pb.ListQualitySuggestionsRequest) []*v1pb.QualitySuggestion {
		t.Helper()
		request.Parent = "workspaces/-"
		request.PageSize = 100
		resp, err := client.ListQualitySuggestions(fixture.ctx, withToken(token, request))
		require.NoError(t, err)
		return resp.Msg.GetQualitySuggestions()
	}

	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{TargetGuid: fixture.usersGUID}), 4)
	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{Instance: fixture.instanceID}), 4)
	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{Instance: common.FormatInstance(fixture.instanceID)}), 4,
		"an instance resource name is accepted as its GUID segment")
	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{
		Instance: fixture.instanceID,
		Database: fixture.sourceDatabase,
	}), 4)

	tableRequest := func(table string) *v1pb.ListQualitySuggestionsRequest {
		return &v1pb.ListQualitySuggestionsRequest{
			Instance: fixture.instanceID,
			Database: fixture.sourceDatabase,
			Table:    table,
		}
	}
	require.Len(t, list(t, tableRequest("users")), 4, "a table filter selects that table's suggestions")
	require.Empty(t, list(t, tableRequest("orders")), "the fixture's other table has no suggestions")

	pending := list(t, &v1pb.ListQualitySuggestionsRequest{
		TargetGuid: fixture.usersGUID,
		Status:     v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
	})
	require.Len(t, pending, 2, "the invalid item's draft and the label are still pending")

	// Batch decisions: accepting the label writes the column label; rejecting the
	// remaining draft succeeds in the same call.
	decisions, err := client.BatchUpdateQualitySuggestions(fixture.ctx, withToken(token, &v1pb.BatchUpdateQualitySuggestionsRequest{
		Parent: "workspaces/-",
		Requests: []*v1pb.UpdateQualitySuggestionRequest{
			{Name: name(labelSuggestion), Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
				DecisionNote: "matches the profile"},
			{Name: name(draftInvalid), Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED},
		},
	}))
	require.NoError(t, err)
	require.Len(t, decisions.Msg.GetResults(), 2)
	require.Equal(t, name(labelSuggestion), decisions.Msg.GetResults()[0].GetName(), "the result echoes the suggestion it decided")
	require.Equal(t, name(draftInvalid), decisions.Msg.GetResults()[1].GetName())
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED, decisions.Msg.GetResults()[0].GetSuggestion().GetStatus())
	require.Equal(t, v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED, decisions.Msg.GetResults()[1].GetSuggestion().GetStatus())

	labels, err := client.ListColumnLabels(fixture.ctx, withToken(token, &v1pb.ListColumnLabelsRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Len(t, labels.Msg.GetColumnLabels(), 1, "accepting a label suggestion asserts the label")
	require.Equal(t, "name", labels.Msg.GetColumnLabels()[0].GetColumn())
	require.Equal(t, v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT, labels.Msg.GetColumnLabels()[0].GetLabel())
	require.Equal(t, v1pb.QualityLabelSource_QUALITY_LABEL_SOURCE_LLM, labels.Msg.GetColumnLabels()[0].GetSource())

	// Re-deciding fails per item (already decided / not found) without failing the
	// batch: the response is a success with one error string per bad item.
	redone, err := client.BatchUpdateQualitySuggestions(fixture.ctx, withToken(token, &v1pb.BatchUpdateQualitySuggestionsRequest{
		Parent: "workspaces/-",
		Requests: []*v1pb.UpdateQualitySuggestionRequest{
			{Name: name(labelSuggestion), Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED},
			{Name: qualitySuggestionResourceName(workspaceID, "doesnotexist"), Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED},
		},
	}))
	require.NoError(t, err)
	require.Len(t, redone.Msg.GetResults(), 2)
	require.Nil(t, redone.Msg.GetResults()[0].GetSuggestion())
	require.Contains(t, redone.Msg.GetResults()[0].GetError(), "already decided")
	require.Nil(t, redone.Msg.GetResults()[1].GetSuggestion())
	require.Contains(t, redone.Msg.GetResults()[1].GetError(), "not found")
}

// TestDataQualitySuggestionJobsRealServerIntegration covers the durable drafting
// job API: submission validation (LLM disabled by default), reading, listing,
// status filters and idempotent cancellation. Runner execution is covered by the
// qualitysuggestionrunner unit tests, since the integration environment has no
// provider profile.
func TestDataQualitySuggestionJobsRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	token := fixture.env.AdminToken()
	client := fixture.dataQuality
	workspaceID, err := fixture.env.Store.GetWorkspaceID(fixture.ctx)
	require.NoError(t, err)
	jobName := func(resourceID string) string {
		return common.FormatWorkspace(workspaceID) + "/qualitySuggestionJobs/" + resourceID
	}
	validTarget := &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE}

	// LLM analysis is off by default: a well-formed submission is refused before the
	// job is queued, and nothing is left behind.
	_, err = client.CreateQualitySuggestionJob(fixture.ctx, withToken(token, &v1pb.CreateQualitySuggestionJobRequest{
		Parent:  "workspaces/-",
		Intent:  "email must not be empty",
		Targets: []*v1pb.QualityTarget{validTarget},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	manyTargets := make([]*v1pb.QualityTarget, 0, 21)
	for index := range 21 {
		manyTargets = append(manyTargets, &v1pb.QualityTarget{
			MetaGuid: fmt.Sprintf("instances/inst;shop;public;table_%d", index),
			MetaType: v1pb.MetaType_TABLE,
		})
	}
	invalidRequests := []struct {
		name    string
		request *v1pb.CreateQualitySuggestionJobRequest
		code    connect.Code
	}{
		{
			name: "no intent",
			request: &v1pb.CreateQualitySuggestionJobRequest{
				Parent: "workspaces/-", Targets: []*v1pb.QualityTarget{validTarget},
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "no targets",
			request: &v1pb.CreateQualitySuggestionJobRequest{
				Parent: "workspaces/-", Intent: "anything",
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "duplicate targets",
			request: &v1pb.CreateQualitySuggestionJobRequest{
				Parent: "workspaces/-", Intent: "anything",
				Targets: []*v1pb.QualityTarget{validTarget, validTarget},
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "too many targets",
			request: &v1pb.CreateQualitySuggestionJobRequest{
				Parent: "workspaces/-", Intent: "anything", Targets: manyTargets,
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "unknown column",
			request: &v1pb.CreateQualitySuggestionJobRequest{
				Parent: "workspaces/-", Intent: "anything",
				Targets: []*v1pb.QualityTarget{{
					MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE,
					Columns: []string{"no_such_column"},
				}},
			},
			code: connect.CodeInvalidArgument,
		},
		{
			name: "unknown target",
			request: &v1pb.CreateQualitySuggestionJobRequest{
				Parent: "workspaces/-", Intent: "anything",
				Targets: []*v1pb.QualityTarget{{
					MetaGuid: "instances/missing;shop;public;orders", MetaType: v1pb.MetaType_TABLE,
				}},
			},
			code: connect.CodeNotFound,
		},
	}
	for _, test := range invalidRequests {
		_, err := client.CreateQualitySuggestionJob(fixture.ctx, withToken(token, test.request))
		require.Errorf(t, err, "%s must be refused", test.name)
		require.Equalf(t, test.code, connect.CodeOf(err), "%s: %v", test.name, err)
	}

	// A deep link that names no job is NotFound.
	_, err = client.GetQualitySuggestionJob(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionJobRequest{
		Name: jobName("doesnotexist"),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Nothing was queued by the refused submissions.
	empty, err := client.ListQualitySuggestionJobs(fixture.ctx, withToken(token, &v1pb.ListQualitySuggestionJobsRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Empty(t, empty.Msg.GetQualitySuggestionJobs())
	_, err = client.ListQualitySuggestionJobs(fixture.ctx, withToken(token, &v1pb.ListQualitySuggestionJobsRequest{
		Parent: "workspaces/-", Status: v1pb.QualitySuggestionJobStatus(99),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Seed one RUNNING job (a PENDING one would be claimed by the live runner). Its
	// targets show one finished target with a queued suggestion and one still
	// pending.
	resourceID, err := common.RandomString(12)
	require.NoError(t, err)
	seedAt := time.Now().UTC()
	seedTarget := func(guid string, state storepb.QualitySuggestionJobTargetState, suggestions []string) *storepb.QualitySuggestionJobTarget {
		target := &storepb.QualitySuggestionJobTarget{
			TargetGuid: guid,
			TargetType: int32(storepb.MetaType_TABLE),
			Path: &storepb.QualityTargetPath{
				Instance: fixture.instanceID,
				Database: fixture.sourceDatabase,
				Object:   "users",
			},
			Columns:               []string{"email"},
			State:                 state,
			SuggestionResourceIds: suggestions,
		}
		// A target only has a start/finish stamp once it was visited.
		if state != storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING {
			target.StartedAtUnixMs = seedAt.UnixMilli()
			target.FinishedAtUnixMs = seedAt.UnixMilli()
		}
		return target
	}
	seeded, err := fixture.env.Store.CreateQualitySuggestionJob(fixture.ctx, &store.QualitySuggestionJobMessage{
		ResourceID:    resourceID,
		Status:        storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		Intent:        "email must not be empty",
		Language:      "en",
		Provider:      "profiles/integration",
		Model:         "integration-model",
		PromptVersion: "dq.rule_draft.v1",
		Targets: &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{
			seedTarget(fixture.usersGUID, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, []string{"sugg1"}),
			seedTarget("instances/missing;shop;public;orders", storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING, nil),
		}},
		CreatedBy: qualityPtr(int32(1)),
	})
	require.NoError(t, err)
	require.NotZero(t, seeded.ID)

	read, err := client.GetQualitySuggestionJob(fixture.ctx, withToken(token, &v1pb.GetQualitySuggestionJobRequest{
		Name: jobName(resourceID),
	}))
	require.NoError(t, err)
	require.Equal(t, jobName(resourceID), read.Msg.GetName())
	require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING, read.Msg.GetStatus())
	require.Equal(t, "email must not be empty", read.Msg.GetIntent())
	require.Equal(t, "en", read.Msg.GetLanguage())
	require.Equal(t, "profiles/integration", read.Msg.GetProvider())
	require.Equal(t, "integration-model", read.Msg.GetModel())
	require.Equal(t, "dq.rule_draft.v1", read.Msg.GetPromptVersion())
	require.Equal(t, "users/1", read.Msg.GetCreatedBy())
	require.Equal(t, int32(2), read.Msg.GetTargetCount())
	require.Equal(t, int32(1), read.Msg.GetCompletedTargetCount())
	require.Equal(t, int32(1), read.Msg.GetSuggestionCount())
	require.Len(t, read.Msg.GetTargets(), 2)
	require.Equal(t, fixture.usersGUID, read.Msg.GetTargets()[0].GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, read.Msg.GetTargets()[0].GetState())
	require.Equal(t, []string{common.FormatWorkspace(workspaceID) + "/qualitySuggestions/sugg1"},
		read.Msg.GetTargets()[0].GetSuggestions())
	require.Equal(t, fixture.sourceDatabase, read.Msg.GetTargets()[0].GetTarget().GetPath().GetDatabase())
	require.NotNil(t, read.Msg.GetTargets()[0].GetStartTime())
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING, read.Msg.GetTargets()[1].GetState())
	require.Nil(t, read.Msg.GetTargets()[1].GetFinishTime(), "a pending target has not finished")

	// The status filters select it or hide it.
	running, err := client.ListQualitySuggestionJobs(fixture.ctx, withToken(token, &v1pb.ListQualitySuggestionJobsRequest{
		Parent: "workspaces/-", Status: v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
	}))
	require.NoError(t, err)
	require.Equal(t, []string{read.Msg.GetName()}, qualitySuggestionJobNames(running.Msg.GetQualitySuggestionJobs()))
	completed, err := client.ListQualitySuggestionJobs(fixture.ctx, withToken(token, &v1pb.ListQualitySuggestionJobsRequest{
		Parent: "workspaces/-", Status: v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
	}))
	require.NoError(t, err)
	require.Empty(t, completed.Msg.GetQualitySuggestionJobs())

	// Canceling a RUNNING job records the request; the runner stops between targets
	// and finishes it. The API reports the current row.
	canceling, err := client.CancelQualitySuggestionJob(fixture.ctx, withToken(token, &v1pb.CancelQualitySuggestionJobRequest{
		Name: jobName(resourceID),
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING, canceling.Msg.GetStatus())

	// The runner finishes the seeded job outside this scenario; completing it here
	// makes the terminal-cancel idempotence observable.
	require.NoError(t, fixture.env.Store.FinishQualitySuggestionJob(fixture.ctx, seeded.ID,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, "stopped between targets", time.Now().UTC()))
	terminal, err := client.CancelQualitySuggestionJob(fixture.ctx, withToken(token, &v1pb.CancelQualitySuggestionJobRequest{
		Name: jobName(resourceID),
	}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, terminal.Msg.GetStatus(),
		"canceling a terminal job returns it unchanged")
	require.Equal(t, "stopped between targets", terminal.Msg.GetError())

	failed, err := client.ListQualitySuggestionJobs(fixture.ctx, withToken(token, &v1pb.ListQualitySuggestionJobsRequest{
		Parent: "workspaces/-", Status: v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
	}))
	require.NoError(t, err)
	require.Equal(t, []string{read.Msg.GetName()}, qualitySuggestionJobNames(failed.Msg.GetQualitySuggestionJobs()))
}

// qualitySuggestionJobNames returns the resource names of a job page.
func qualitySuggestionJobNames(jobs []*v1pb.QualitySuggestionJob) []string {
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.GetName())
	}
	return names
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

	execution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, execution.GetOutcome(), "the fixture's age is within 0..200")
	require.NotEmpty(t, execution.GetExecutedSql())
	require.Equal(t, int64(1), execution.GetTotalRows())
}

// TestDataQualityColumnProfileJsonRealServerIntegration covers a relation with a
// jsonb column: PostgreSQL has neither MIN(jsonb) nor CHAR_LENGTH(jsonb), so the
// aggregate must profile the column by counts and take its distinct count from the
// sample instead of failing the whole table's request.
func TestDataQualityColumnProfileJsonRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.audit_log (
  id BIGSERIAL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  payload JSONB NOT NULL,
  client_mac MACADDR NOT NULL
);
INSERT INTO public.audit_log (created_at, payload, client_mac) VALUES
  ('2026-01-01 00:00:00+00', '{"kind":"login"}', '08:00:2b:01:02:03'),
  ('2026-01-02 00:00:00+00', '{"kind":"audit"}', '08:00:2b:01:02:04'),
  ('2026-01-03 00:00:00+00', '{"kind":"login"}', '08:00:2b:01:02:03');
CREATE MATERIALIZED VIEW public.audit_log_tags AS
  SELECT payload, ARRAY['x']::varchar[] AS tags FROM public.audit_log;
`))

	_, err := env.CreateDataSource(ctx, common.FormatInstance(instanceID), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "postgres",
		Password: "postgres",
		Host:     env.PostgresHost,
		Port:     env.PostgresPort,
	}, "readonly", false)
	require.NoError(t, err)
	env.SyncDatabase(ctx, t, databaseName)

	tableGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_TABLE, "audit_log")

	client := v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 30 * time.Second}, env.BaseURL)
	resp, err := client.ProfileColumns(ctx, withToken(env.AdminToken(), &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{"id", "created_at", "payload", "client_mac"},
	}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetColumnProfiles(), 4)
	profiles := make(map[string]*v1pb.ColumnProfile, 4)
	for _, profile := range resp.Msg.GetColumnProfiles() {
		profiles[profile.GetColumn()] = profile
	}

	payload := profiles["payload"].GetProfile()
	require.Equal(t, int64(3), payload.GetTotalRows())
	require.Equal(t, int64(0), payload.GetNullCount())
	require.Equal(t, int64(2), payload.GetDistinctCount())
	require.False(t, payload.GetDistinctEstimated(), "the sample read every row")
	require.Empty(t, payload.GetMinValue())
	require.Empty(t, payload.GetMaxValue())
	require.Nil(t, payload.MinLength)
	require.Nil(t, payload.MaxLength)
	require.NotEmpty(t, payload.GetTopValues())

	// macaddr has equality but no MIN/MAX; profiling it must not fail the request.
	mac := profiles["client_mac"].GetProfile()
	require.Equal(t, int64(3), mac.GetTotalRows())
	require.Equal(t, int64(2), mac.GetDistinctCount())
	require.Empty(t, mac.GetMinValue())
	require.Empty(t, mac.GetMaxValue())
	require.Nil(t, mac.MinLength)

	// A materialized view stores its varchar array as "character varying[]"; it must
	// be profiled as an array (no CHAR_LENGTH) rather than a text scalar.
	viewGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_MATERIALIZED_VIEW, "audit_log_tags")
	viewProfiles, err := client.ProfileColumns(ctx, withToken(env.AdminToken(), &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: viewGUID,
		MetaType:  v1pb.MetaType_MATERIALIZED_VIEW,
		Columns:   []string{"payload", "tags"},
	}))
	require.NoError(t, err)
	require.Len(t, viewProfiles.Msg.GetColumnProfiles(), 2)
	viewByColumn := make(map[string]*v1pb.ColumnProfile, 2)
	for _, profile := range viewProfiles.Msg.GetColumnProfiles() {
		viewByColumn[profile.GetColumn()] = profile
		require.Nil(t, profile.GetProfile().MinLength, "an array column has no character length")
	}
	// The varchar[] must keep its array facts: exact distinct and MIN/MAX. A regression
	// to a zero-capability column would still satisfy the MinLength assertion above.
	arrayTags := viewByColumn["tags"].GetProfile()
	require.Equal(t, int64(1), arrayTags.GetDistinctCount())
	require.NotEmpty(t, arrayTags.GetMinValue())
	require.NotEmpty(t, arrayTags.GetMaxValue())

	// The ordinary columns still carry the facts their types support.
	require.Equal(t, "1", profiles["id"].GetProfile().GetMinValue())
	require.Equal(t, "3", profiles["id"].GetProfile().GetMaxValue())
	require.NotEmpty(t, profiles["created_at"].GetProfile().GetMinValue())
}

// qualityPtr is a local pointer helper for the optional proto fields.
func qualityPtr[T any](value T) *T { return &value }

// TestDataQualityRuleRejectsUnrunnableColumnTypesRealServerIntegration covers the
// types ColumnKind calls "string" but the engine cannot run text or equality
// operators on: the rule is refused at save time instead of being stored and
// recording ERROR on every execution. The cases the engines do support (jsonb
// equality, CEL) must still save.
func TestDataQualityRuleRejectsUnrunnableColumnTypesRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.opaque_probe (
  id BIGSERIAL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  payload JSONB NOT NULL,
  payload_json JSON NOT NULL,
  doc XML NOT NULL,
  uid UUID NOT NULL,
  tags TEXT[] NOT NULL
);
INSERT INTO public.opaque_probe (created_at, payload, payload_json, doc, uid, tags)
VALUES (now(), '{"k":1}', '{"k":1}', '<a/>', '11111111-1111-1111-1111-111111111111', '{a,b}');
CREATE TABLE public.code_ref (code TEXT PRIMARY KEY);
INSERT INTO public.code_ref (code) VALUES ('a');
CREATE VIEW public.code_numbers AS SELECT 1::bigint AS n;
`))
	_, err := env.CreateDataSource(ctx, common.FormatInstance(instanceID), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "postgres",
		Password: "postgres",
		Host:     env.PostgresHost,
		Port:     env.PostgresPort,
	}, "readonly", false)
	require.NoError(t, err)
	env.SyncDatabase(ctx, t, databaseName)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	probeGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "opaque_probe")
	refGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "code_ref")
	client := v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 30 * time.Second}, env.BaseURL)
	token := env.AdminToken()

	newRule := func(guid string, ruleType v1pb.QualityRuleType, config *v1pb.QualityRuleConfig) *v1pb.QualityRule {
		return &v1pb.QualityRule{
			Title:       "opaque type probe " + ruleType.String(),
			Dimension:   v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			RuleType:    ruleType,
			Target:      &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config:      config,
			Severity:    v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			Description: "created by the opaque-type integration scenario",
		}
	}
	filterEq := func(column string) *v1pb.QualityFilter {
		return &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
			Column: column,
			Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{`{"k":1}`},
		}}}
	}
	rowCountWithFilter := func(column string) *v1pb.QualityRuleConfig {
		return &v1pb.QualityRuleConfig{
			Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{MinRows: qualityPtr(int64(1))}},
			Filter: filterEq(column),
		}
	}

	rejected := []struct {
		name string
		rule *v1pb.QualityRule
		want string
	}{
		{
			name: "regex on jsonb",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_Regex{Regex: &v1pb.RegexConfig{Column: "payload", Pattern: "^x"}},
			}),
			want: "needs a text type",
		},
		{
			name: "string_length on uuid",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_StringLength{StringLength: &v1pb.StringLengthConfig{
					Column: "uid", MinLen: qualityPtr(int64(10)),
				}},
			}),
			want: "needs a text type",
		},
		{
			name: "unique on json",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{Column: "payload_json"}},
			}),
			want: "the engine cannot group values of this type",
		},
		{
			name: "unique_combination all columns",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_UniqueCombination{UniqueCombination: &v1pb.UniqueCombinationConfig{
					AllColumns: true,
				}},
			}),
			want: "the engine cannot group values of this type",
		},
		{
			name: "group_check on xml",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_GroupCheck{GroupCheck: &v1pb.GroupCheckConfig{
					Columns: []string{"doc"}, MinRowsPerGroup: qualityPtr(int64(1)),
				}},
			}),
			want: "the engine cannot group values of this type",
		},
		{
			name: "accepted_values on json",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_AcceptedValues{AcceptedValues: &v1pb.AcceptedValuesConfig{
					Column: "payload_json", Values: []string{`{"k":1}`},
				}},
			}),
			want: "the engine cannot compare it with a scalar value",
		},
		{
			name: "null_percent custom values on json",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_NullPercent{NullPercent: &v1pb.NullPercentConfig{
					Column: "payload_json", MaxNullCount: qualityPtr(int64(0)), NullValues: []string{`{}`},
				}},
			}),
			want: "the engine cannot compare it with a scalar value",
		},
		{
			name: "filter on json",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, rowCountWithFilter("payload_json")),
			want: "the engine cannot compare it with a scalar value",
		},
		{
			name: "aggregate MIN on a temporal column",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_Aggregate{Aggregate: &v1pb.AggregateConfig{
					Column: "created_at", Function: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
				}},
			}),
			want: "needs a numeric column",
		},
		{
			name: "cross_table MAX on a temporal right column",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
					RightTable:    "opaque_probe",
					LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
					RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MAX,
					RightColumn:   "created_at",
				}},
			}),
			want: "needs a numeric column",
		},
		{
			name: "cross_table MIN on a temporal left column",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
					RightTable:    "code_ref",
					LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
					LeftColumn:    "created_at",
					RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				}},
			}),
			want: "needs a numeric column",
		},
		{
			name: "referential between jsonb and text",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &v1pb.ReferentialIntegrityConfig{
					ChildColumn: "payload", ParentTable: "code_ref", ParentColumn: "code",
				}},
			}),
			want: "are not comparable",
		},
		{
			name: "cross_table missing right table",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
					RightTable:    "dropped_relation",
					LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
					RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				}},
			}),
			want: "does not exist or was dropped",
		},
		{
			name: "cross_table MIN on json",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
					RightTable:    "opaque_probe",
					LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
					RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_MIN,
					RightColumn:   "payload_json",
				}},
			}),
			want: "needs a numeric column",
		},
	}
	for _, test := range rejected {
		_, err := client.CreateQualityRule(ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
			Parent: "workspaces/-", Rule: test.rule,
		}))
		require.Errorf(t, err, "%s must be refused at save time", test.name)
		require.Equalf(t, connect.CodeInvalidArgument, connect.CodeOf(err), "%s: %v", test.name, err)
		require.Containsf(t, err.Error(), test.want, "%s: %v", test.name, err)
	}

	// The dry run rejects the same definitions the save does, so the editor shows
	// the validation message rather than a raw SQL error.
	_, err = client.PreviewQualityRule(ctx, withToken(token, &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, &v1pb.QualityRuleConfig{
			Config: &v1pb.QualityRuleConfig_Regex{Regex: &v1pb.RegexConfig{Column: "payload", Pattern: "^x"}},
		}),
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "needs a text type")

	accepted := []struct {
		name string
		rule *v1pb.QualityRule
	}{
		{
			name: "unique on jsonb",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{Column: "payload"}},
			}),
		},
		{
			name: "accepted_values on jsonb",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_AcceptedValues{AcceptedValues: &v1pb.AcceptedValuesConfig{
					Column: "payload", Values: []string{`{"k":1}`},
				}},
			}),
		},
		{
			name: "filter on jsonb",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
				rowCountWithFilter("payload")),
		},
		{
			name: "unique_combination on a bigint and a uuid",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_UniqueCombination{UniqueCombination: &v1pb.UniqueCombinationConfig{
					Columns: []string{"id", "uid"},
				}},
			}),
		},
		{
			name: "cel on jsonb",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CelExpression{CelExpression: &v1pb.CelExpressionConfig{
					Expression: "size(payload) > 0", Columns: []string{"payload"},
				}},
			}),
		},
		{
			name: "referential between two text columns",
			rule: newRule(refGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &v1pb.ReferentialIntegrityConfig{
					ChildColumn: "code", ParentTable: "code_ref", ParentColumn: "code",
				}},
			}),
		},
		{
			name: "cross_table SUM over a view",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
					RightTable:    "code_numbers",
					LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
					RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
					RightColumn:   "n",
				}},
			}),
		},
		{
			name: "cross_table COUNT reconciliation",
			rule: newRule(probeGUID, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_CrossTable{CrossTable: &v1pb.CrossTableConfig{
					RightTable:    "code_ref",
					LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
					RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				}},
			}),
		},
	}
	for _, test := range accepted {
		_, err := client.CreateQualityRule(ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
			Parent: "workspaces/-", Rule: test.rule,
		}))
		require.NoErrorf(t, err, "%s must remain saveable", test.name)
	}
}

// TestDataQualityNullPercentOnJSONRunsRealServerIntegration proves the failed-row
// sample is never stricter than the aggregate it is evidence for: a plain
// null_percent rule on a json column (no comparison capability, but IS NULL works)
// must finish with a verdict, not record ERROR because the sample query could not be
// compiled.
func TestDataQualityNullPercentOnJSONRunsRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.opaque_null (
  id BIGSERIAL PRIMARY KEY,
  payload_json JSON
);
INSERT INTO public.opaque_null (payload_json) VALUES ('{"a":1}'), (NULL);
`))
	_, err := env.CreateDataSource(ctx, common.FormatInstance(instanceID), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "postgres",
		Password: "postgres",
		Host:     env.PostgresHost,
		Port:     env.PostgresPort,
	}, "readonly", false)
	require.NoError(t, err)
	env.SyncDatabase(ctx, t, databaseName)
	guid := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_TABLE, "opaque_null")

	fixture := &qualityFixture{
		env:         env,
		ctx:         ctx,
		dataQuality: v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 30 * time.Second}, env.BaseURL),
	}
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		guid, &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_NullPercent{
			NullPercent: &v1pb.NullPercentConfig{Column: "payload_json", MaxNullCount: qualityPtr(int64(0))},
		}})
	execution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, execution.GetOutcome(),
		"one NULL exceeds max_null_count 0")
	require.NotEqual(t, v1pb.QualityStatus_QUALITY_STATUS_ERROR, execution.GetOutcome(),
		"a plain null check must never be an execution error: %s", execution.GetErrorMessage())
	require.Empty(t, execution.GetErrorMessage())
	require.Equal(t, int64(1), execution.GetFailedRows())
	require.Equal(t, int64(2), execution.GetTotalRows())
}

// TestDataQualityExecutionStoreLifecycle covers the execution state machine the
// API and runner rely on, against the real store: a stale planned row is swept
// to ABANDONED, a verdict written without a snapshot keeps the planned target
// and revision, and abandoning a batch closes only its unfinished rows.
func TestDataQualityExecutionStoreLifecycle(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		fixture.usersGUID, qualityRowCountConfig(1))
	resourceID := rule.GetName()[strings.LastIndex(rule.GetName(), "/")+1:]
	stored, err := fixture.env.Store.GetQualityRule(fixture.ctx, &store.FindQualityRuleMessage{ResourceID: &resourceID})
	require.NoError(t, err)
	require.NotNil(t, stored)

	newBatch := func(t *testing.T) *store.QualityRunMessage {
		t.Helper()
		batchResourceID, err := common.RandomString(12)
		require.NoError(t, err)
		batch, err := fixture.env.Store.CreateQualityRun(fixture.ctx, &store.QualityRunMessage{
			ResourceID:         batchResourceID,
			InstanceResourceID: fixture.instanceID,
			Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
			Status:             storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		})
		require.NoError(t, err)
		return batch
	}
	plan := func(t *testing.T, batch *store.QualityRunMessage, startedAt time.Time) *store.QualityExecutionMessage {
		t.Helper()
		planned := &store.QualityExecutionMessage{
			RunID:              batch.ID,
			RuleID:             stored.ID,
			RuleRevision:       stored.Revision,
			RuleEngineVersion:  "test",
			TargetGUID:         stored.TargetGUID,
			TargetType:         stored.TargetType,
			TargetPath:         stored.TargetPath,
			InstanceResourceID: fixture.instanceID,
			Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
			StartedAt:          startedAt,
		}
		require.NoError(t, fixture.env.Store.PlanQualityExecutions(fixture.ctx, []*store.QualityExecutionMessage{planned}))
		require.NotZero(t, planned.ID, "planning must fill the row id")
		return planned
	}

	// A planned row older than the cutoff is swept to ABANDONED, without a
	// verdict, and cannot be finished afterwards.
	staleAt := time.Now().UTC().Add(-30 * time.Minute)
	stale := plan(t, newBatch(t), staleAt)
	abandoned, err := fixture.env.Store.AbandonStaleQualityExecutions(fixture.ctx, staleAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), abandoned)
	staleRow, err := fixture.env.Store.GetQualityExecution(fixture.ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, staleRow.State)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, staleRow.Status)
	require.False(t, staleRow.FinishedAt.IsZero())
	require.Error(t, fixture.env.Store.FinishQualityExecution(fixture.ctx, &store.QualityExecutionMessage{
		ID: stale.ID, Status: storepb.QualityStatus_QUALITY_STATUS_PASS, FinishedAt: time.Now().UTC(),
	}), "an abandoned execution must not accept a verdict")

	// A verdict that failed before reading a snapshot keeps the planned target
	// and revision; a real status is required.
	running := plan(t, newBatch(t), time.Now().UTC())
	require.NoError(t, fixture.env.Store.MarkQualityExecutionRunning(fixture.ctx, running.ID, time.Now().UTC()))
	require.NoError(t, fixture.env.Store.FinishQualityExecution(fixture.ctx, &store.QualityExecutionMessage{
		ID:         running.ID,
		Status:     storepb.QualityStatus_QUALITY_STATUS_PASS,
		FailedRows: 0,
		TotalRows:  3,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}))
	finishedRow, err := fixture.env.Store.GetQualityExecution(fixture.ctx, running.ID)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, finishedRow.State)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, finishedRow.Status)
	require.Equal(t, stored.Revision, finishedRow.RuleRevision, "the planned revision survives a snapshot-less verdict")
	require.Equal(t, stored.TargetGUID, finishedRow.TargetGUID, "the planned target survives a snapshot-less verdict")
	require.Equal(t, fixture.instanceID, finishedRow.InstanceResourceID)

	// Abandoning a batch closes only PENDING/RUNNING rows: a verdict already
	// written stays COMPLETED and is not counted as abandoned.
	batch := newBatch(t)
	first := plan(t, batch, time.Now().UTC())
	second := plan(t, batch, time.Now().UTC())
	settled := plan(t, batch, time.Now().UTC())
	require.NoError(t, fixture.env.Store.MarkQualityExecutionRunning(fixture.ctx, first.ID, time.Now().UTC()))
	require.NoError(t, fixture.env.Store.MarkQualityExecutionRunning(fixture.ctx, settled.ID, time.Now().UTC()))
	require.NoError(t, fixture.env.Store.FinishQualityExecution(fixture.ctx, &store.QualityExecutionMessage{
		ID:         settled.ID,
		Status:     storepb.QualityStatus_QUALITY_STATUS_WARN,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}))
	closed, err := fixture.env.Store.AbandonQualityExecutions(fixture.ctx, batch.ID, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, int64(2), closed)
	for _, id := range []int64{first.ID, second.ID} {
		row, err := fixture.env.Store.GetQualityExecution(fixture.ctx, id)
		require.NoError(t, err)
		require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, row.State)
	}
	settledRow, err := fixture.env.Store.GetQualityExecution(fixture.ctx, settled.ID)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, settledRow.State)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, settledRow.Status)
}

// TestDataQualitySuggestionJobRunnerConfigFailureRealServerIntegration proves the
// missing seam for the async drafting flow: a job created through the public API is
// claimed by the server's registered qualitysuggestionrunner, and the runner closes
// it when the workspace LLM configuration cannot be resolved. It does not run in
// parallel: it flips a workspace-wide LLM switch that other scenarios assert is off,
// and restores it before returning.
//
// The submission fail-fasts when the provider configuration is unusable, so the job
// has to be queued while a provider exists and lose it before the runner claims it.
// The runner ticks every 5s; the profile is removed immediately after the create
// RPC, and a claim that wins the (tiny) race simply runs a target to a per-target
// failure, which is retried with a fresh provider and job.
func TestDataQualitySuggestionJobRunnerConfigFailureRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, false)
	token := fixture.env.AdminToken()
	client := fixture.dataQuality
	llmClient := v1connect.NewLLMServiceClient(&http.Client{Timeout: 30 * time.Second}, fixture.env.BaseURL)
	ctx := fixture.ctx

	updateLLMAnalysis := func(enabled bool) {
		t.Helper()
		var dataQuality *v1pb.DataQualitySetting
		if enabled {
			dataQuality = &v1pb.DataQualitySetting{EnableLlmAnalysis: true}
		}
		_, err := fixture.settings.UpdateWorkspaceProfileSetting(ctx, withToken(token, &v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{DataQuality: dataQuality},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"data_quality"}},
		}))
		require.NoError(t, err)
	}
	updateLLMAnalysis(true)
	t.Cleanup(func() { updateLLMAnalysis(false) })

	// The job rows this scenario creates use the workspace-wide metadata store, so
	// they must be removed: the suite must not leak rows the suggestion-job list
	// assertions do not expect.
	var createdJobs []string
	t.Cleanup(func() {
		for _, jobResourceID := range createdJobs {
			_, _ = fixture.env.Store.GetDB().ExecContext(context.Background(),
				"DELETE FROM quality_suggestion_job WHERE resource_id = $1", jobResourceID)
		}
	})

	createProvider := func() string {
		t.Helper()
		resp, err := llmClient.CreateLLMProviderProfile(ctx, withToken(token, &v1pb.CreateLLMProviderProfileRequest{
			Profile: &v1pb.LlmProviderProfile{
				Title:   "integration temporary provider",
				Type:    v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI,
				BaseUrl: "http://127.0.0.1:1/v1",
				ApiKey:  "integration-key",
				Models:  []*v1pb.LlmProviderModel{{Name: "integration-model", Enabled: true}},
			},
		}))
		require.NoError(t, err)
		name := resp.Msg.GetName()
		require.NotEmpty(t, name)
		// A failed attempt may leave the profile behind; deleting it again is a
		// harmless NotFound.
		t.Cleanup(func() {
			_, _ = llmClient.DeleteLLMProviderProfile(context.Background(), withToken(token, &v1pb.DeleteLLMProviderProfileRequest{Name: name}))
		})
		return name
	}
	deleteProvider := func(name string) {
		t.Helper()
		_, err := llmClient.DeleteLLMProviderProfile(ctx, withToken(token, &v1pb.DeleteLLMProviderProfileRequest{Name: name}))
		require.NoError(t, err)
	}
	waitForTerminal := func(name string) *v1pb.QualitySuggestionJob {
		t.Helper()
		var terminal *v1pb.QualitySuggestionJob
		require.Eventually(t, func() bool {
			resp, err := client.GetQualitySuggestionJob(ctx, withToken(token, &v1pb.GetQualitySuggestionJobRequest{Name: name}))
			if err != nil {
				return false
			}
			switch resp.Msg.GetStatus() {
			case v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
				v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING:
				return false
			default:
				terminal = resp.Msg
				return true
			}
		}, 15*time.Second, 100*time.Millisecond, "the drafting job never reached a terminal status")
		return terminal
	}

	var failed *v1pb.QualitySuggestionJob
	for attempt := 1; attempt <= 3; attempt++ {
		providerName := createProvider()
		created, err := client.CreateQualitySuggestionJob(ctx, withToken(token, &v1pb.CreateQualitySuggestionJobRequest{
			Parent: "workspaces/-",
			Intent: "email must not be empty",
			Targets: []*v1pb.QualityTarget{{
				MetaGuid: fixture.usersGUID,
				MetaType: v1pb.MetaType_TABLE,
			}},
		}))
		require.NoErrorf(t, err, "attempt %d: create the drafting job", attempt)
		require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING, created.Msg.GetStatus(),
			"the API queues the job and returns immediately")
		createdJobs = append(createdJobs, created.Msg.GetName()[strings.LastIndex(created.Msg.GetName(), "/")+1:])
		deleteProvider(providerName)

		terminal := waitForTerminal(created.Msg.GetName())
		if terminal.GetStatus() == v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED {
			failed = terminal
			break
		}
		// The runner claimed the job in the window before the provider was removed:
		// the model call then failed per target and the job completed. Retry with a
		// fresh provider so the config-failure path is exercised.
		t.Logf("attempt %d: runner claimed the job before the provider was removed (status %s); retrying",
			attempt, terminal.GetStatus())
	}

	require.NotNil(t, failed, "no attempt produced a job the runner failed on the missing provider")
	require.Equal(t, v1pb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED, failed.GetStatus())
	require.Contains(t, strings.ToLower(failed.GetError()), "provider")
	require.NotEmpty(t, failed.GetStartTime(), "the registered runner claimed the job")
	require.NotEmpty(t, failed.GetFinishTime())
	require.Len(t, failed.GetTargets(), 1)
	require.Equal(t, v1pb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_SKIPPED,
		failed.GetTargets()[0].GetState(), "a target the runner never asked is skipped, not left pending")
}
