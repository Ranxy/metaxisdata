//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
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

// qualityRowCountChangeConfig builds a row-count-change config with one bound. Its
// first execution has no baseline, so the default empty behavior makes it
// NOT_EVALUATED.
func qualityRowCountChangeConfig(maxIncreasePct float64) *v1pb.QualityRuleConfig {
	return &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCountChange{
		RowCountChange: &v1pb.RowCountChangeConfig{MaxIncreasePct: &maxIncreasePct},
	}}
}

// qualityColumnConfig builds a null-percent check on one column.
func qualityColumnConfig(column string) *v1pb.QualityRuleConfig {
	return &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_NullPercent{
		NullPercent: &v1pb.NullPercentConfig{Column: column, MaxNullCount: qualityPtr(int64(0))},
	}}
}

// seedSuggestion stores one pending suggestion directly: the LLM RPCs are off by
// default, so the review-queue scenarios seed the rows a model would have
// proposed.
func (f *qualityFixture) seedSuggestion(t *testing.T, kind storepb.QualitySuggestionKind, payload *storepb.QualitySuggestionPayload) *store.QualitySuggestionMessage {
	t.Helper()

	resourceID, err := common.RandomString(12)
	require.NoError(t, err)
	suggestion, queued, err := f.env.Store.QueueQualitySuggestion(f.ctx, &store.QualitySuggestionMessage{
		ResourceID:    resourceID,
		Kind:          kind,
		Source:        storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_NATURAL_LANGUAGE,
		TargetGUID:    f.usersGUID,
		TargetType:    storepb.MetaType_TABLE,
		Signature:     "seed:" + resourceID,
		Payload:       payload,
		Status:        storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Provider:      "profiles/integration",
		Model:         "integration-model",
		PromptVersion: "dq.rule_draft.v1",
	})
	require.NoError(t, err)
	require.True(t, queued)
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

	// The target and column errors are the caller's, not the switch's: the sync
	// drafting RPC resolves the target before it checks the provider, so a stale
	// target reports NotFound and an unknown column InvalidArgument even here.
	_, err = fixture.dataQuality.SuggestQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.SuggestQualityRuleRequest{
		Parent:    "workspaces/-",
		TableGuid: "doesnotexist;db;public;missing",
		MetaType:  v1pb.MetaType_TABLE,
		Intent:    "the email column must not be empty",
		Language:  "en",
	}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "an unknown target is NotFound even with the LLM off: %v", err)

	_, err = fixture.dataQuality.SuggestQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.SuggestQualityRuleRequest{
		Parent:    "workspaces/-",
		TableGuid: fixture.usersGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{"no_such_column"},
		Intent:    "the email column must not be empty",
		Language:  "en",
	}))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "an unknown column is InvalidArgument even with the LLM off: %v", err)

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

	// Two concurrent "create a rule from this draft" requests must not both write a
	// rule: the PENDING→ACCEPTED claim is the idempotency key, so exactly one wins
	// and the other is Aborted.
	raceTitle := "concurrent create from a draft"
	draftRace := fixture.seedSuggestion(t, storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		qualityDraftSuggestionPayload(raceTitle))
	createRaceRule := func() error {
		_, err := client.CreateQualityRule(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
			Parent: "workspaces/-",
			Rule: &v1pb.QualityRule{
				Title:     raceTitle,
				Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
				RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
				Target:    &v1pb.QualityTarget{MetaGuid: fixture.usersGUID, MetaType: v1pb.MetaType_TABLE},
				Config:    qualityRowCountConfig(1),
				Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			},
			Suggestion: name(draftRace),
		}))
		return err
	}
	var race sync.WaitGroup
	raceErrs := make([]error, 2)
	for index := range raceErrs {
		race.Add(1)
		go func(index int) {
			defer race.Done()
			raceErrs[index] = createRaceRule()
		}(index)
	}
	race.Wait()
	successes, aborted, unexpected := 0, 0, 0
	for _, raceErr := range raceErrs {
		switch {
		case raceErr == nil:
			successes++
		case connect.CodeOf(raceErr) == connect.CodeAborted:
			aborted++
		default:
			unexpected++
		}
	}
	require.Equal(t, 1, successes, "exactly one concurrent create may succeed")
	require.Equal(t, 1, aborted, "the losing create reports the draft as already claimed")
	require.Zero(t, unexpected, "no other failure is acceptable: %v", raceErrs)

	racedRules, err := client.ListQualityRules(fixture.ctx, withToken(token, &v1pb.ListQualityRulesRequest{
		Parent:   "workspaces/-",
		PageSize: 1000,
		Search:   raceTitle,
	}))
	require.NoError(t, err)
	require.Len(t, racedRules.Msg.GetQualityRules(), 1, "one draft may produce at most one rule")

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

	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{TargetGuid: fixture.usersGUID}), 5)
	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{Instance: fixture.instanceID}), 5)
	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{Instance: common.FormatInstance(fixture.instanceID)}), 5,
		"an instance resource name is accepted as its GUID segment")
	require.Len(t, list(t, &v1pb.ListQualitySuggestionsRequest{
		Instance: fixture.instanceID,
		Database: fixture.sourceDatabase,
	}), 5)

	tableRequest := func(table string) *v1pb.ListQualitySuggestionsRequest {
		return &v1pb.ListQualitySuggestionsRequest{
			Instance: fixture.instanceID,
			Database: fixture.sourceDatabase,
			Table:    table,
		}
	}
	require.Len(t, list(t, tableRequest("users")), 5, "a table filter selects that table's suggestions")
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
	seedTarget := func(guid string, state storepb.QualitySuggestionJobTargetState, suggestions []string, diagnostic *storepb.QualityLLMDiagnostic) *storepb.QualitySuggestionJobTarget {
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
			Diagnostic:            diagnostic,
		}
		// A target only has a start/finish stamp once it was visited.
		if state != storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING {
			target.StartedAtUnixMs = seedAt.UnixMilli()
			target.FinishedAtUnixMs = seedAt.UnixMilli()
		}
		return target
	}
	seeded, _, err := fixture.env.Store.QueueQualitySuggestionJob(fixture.ctx, &store.QualitySuggestionJobMessage{
		ResourceID:         resourceID,
		Status:             storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		Intent:             "email must not be empty",
		RequestFingerprint: "seed:" + resourceID,
		Language:           "en",
		Provider:           "profiles/integration",
		Model:              "integration-model",
		PromptVersion:      "dq.rule_draft.v1",
		// The first target's failure was not captured (runtime debug off); the second
		// says it was captured but has no row in the debug log any more, so the
		// retrieval reports the pruned case without error.
		Targets: &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{
			seedTarget(fixture.usersGUID, storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_DONE, []string{"sugg1"}, &storepb.QualityLLMDiagnostic{
				ErrorCode:       "TRUNCATED",
				Provider:        "profiles/integration",
				Model:           "integration-model",
				PromptVersion:   "dq.rule_draft.v1",
				MaxOutputTokens: 8192,
				FinishReason:    "length",
				PromptChars:     1234,
				CorrelationId:   "corr-not-captured",
				DebugCaptured:   false,
			}),
			seedTarget("instances/missing;shop;public;orders", storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING, nil, &storepb.QualityLLMDiagnostic{
				ErrorCode:       "TIMEOUT",
				Provider:        "profiles/integration",
				Model:           "integration-model",
				PromptVersion:   "dq.rule_draft.v1",
				MaxOutputTokens: 8192,
				CorrelationId:   "corr-pruned",
				DebugCaptured:   true,
			}),
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
	firstDiagnostic := read.Msg.GetTargets()[0].GetDiagnostic()
	require.NotNil(t, firstDiagnostic)
	require.Equal(t, "TRUNCATED", firstDiagnostic.GetErrorCode())
	require.Equal(t, "length", firstDiagnostic.GetFinishReason())
	require.Equal(t, int32(8192), firstDiagnostic.GetMaxOutputTokens())
	require.Equal(t, "corr-not-captured", firstDiagnostic.GetCorrelationId())
	require.False(t, firstDiagnostic.GetDebugPayloadAvailable())
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

	// Reading the raw bodies: runtime debug was off for the first target, and the
	// second capture was pruned, so both report unavailable with the classifier.
	debugNotCaptured, err := client.GetQualitySuggestionJobTargetDebug(fixture.ctx, withToken(token,
		&v1pb.GetQualitySuggestionJobTargetDebugRequest{Name: jobName(resourceID), TargetIndex: 0}))
	require.NoError(t, err)
	require.False(t, debugNotCaptured.Msg.GetAvailable())
	require.Equal(t, "corr-not-captured", debugNotCaptured.Msg.GetCorrelationId())
	require.Equal(t, "TRUNCATED", debugNotCaptured.Msg.GetErrorCode())
	require.Empty(t, debugNotCaptured.Msg.GetRequestBody())

	// The second target says its bodies were captured; they are readable while the
	// debug-log row exists and unavailable once retention prunes it.
	require.NoError(t, fixture.env.Store.InsertLLMDebugLog(fixture.ctx, &store.LLMDebugLogEntry{
		Provider:      "profiles/integration",
		Model:         "integration-model",
		Kind:          "dq_rule_draft",
		CorrelationID: "corr-pruned",
		ErrorCode:     "TIMEOUT",
		RequestBody:   `{"model":"integration-model"}`,
		ResponseBody:  `{"error":"timeout"}`,
	}))
	debugCaptured, err := client.GetQualitySuggestionJobTargetDebug(fixture.ctx, withToken(token,
		&v1pb.GetQualitySuggestionJobTargetDebugRequest{Name: jobName(resourceID), TargetIndex: 1}))
	require.NoError(t, err)
	require.True(t, debugCaptured.Msg.GetAvailable())
	require.Equal(t, "corr-pruned", debugCaptured.Msg.GetCorrelationId())
	require.Equal(t, "TIMEOUT", debugCaptured.Msg.GetErrorCode())
	require.JSONEq(t, `{"model":"integration-model"}`, debugCaptured.Msg.GetRequestBody())
	require.JSONEq(t, `{"error":"timeout"}`, debugCaptured.Msg.GetResponseBody())

	_, err = fixture.env.Store.GetDB().ExecContext(fixture.ctx, `DELETE FROM llm_debug_log WHERE correlation_id = $1`, "corr-pruned")
	require.NoError(t, err)
	debugPruned, err := client.GetQualitySuggestionJobTargetDebug(fixture.ctx, withToken(token,
		&v1pb.GetQualitySuggestionJobTargetDebugRequest{Name: jobName(resourceID), TargetIndex: 1}))
	require.NoError(t, err)
	require.False(t, debugPruned.Msg.GetAvailable(), "a pruned capture is unavailable, not an error")
	require.Equal(t, "corr-pruned", debugPruned.Msg.GetCorrelationId())
	require.Equal(t, "TIMEOUT", debugPruned.Msg.GetErrorCode())

	for _, index := range []int32{-1, 5} {
		_, err := client.GetQualitySuggestionJobTargetDebug(fixture.ctx, withToken(token,
			&v1pb.GetQualitySuggestionJobTargetDebugRequest{Name: jobName(resourceID), TargetIndex: index}))
		require.Error(t, err, "target_index %d", index)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "target_index %d", index)
	}

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

// TestDataQualityLLMLimitsRealServerIntegration exercises the configurable LLM
// limits end to end: a provider profile's output budget round-trips through the
// LLM API, the two data-quality limits are writable through the settings API, and
// the usage RPC reports the effective limits. It does not run in parallel: it
// changes workspace-wide settings, which other scenarios assert are at their
// defaults, and restores them before returning.
func TestDataQualityLLMLimitsRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, false)
	token := fixture.env.AdminToken()
	ctx := fixture.ctx
	llmClient := v1connect.NewLLMServiceClient(&http.Client{Timeout: 30 * time.Second}, fixture.env.BaseURL)

	// A workspace that never saved LLM limits reports the server defaults.
	defaults, err := fixture.dataQuality.GetQualityLLMUsage(ctx, withToken(token, &v1pb.GetQualityLLMUsageRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Equal(t, int32(dataquality.DefaultDailyLLMAnalysisLimit), defaults.Msg.GetDailyAnalysisLimit())
	require.Equal(t, int32(dataquality.DefaultDailyColumnAnalysisLimit), defaults.Msg.GetDailyColumnAnalysisLimit())
	require.Equal(t, int32(dataquality.DefaultLLMConcurrencyLimit), defaults.Msg.GetConcurrencyLimit())
	require.Zero(t, defaults.Msg.GetInFlight())
	require.NotNil(t, defaults.Msg.GetResetsAt())
	require.Equal(t, 0, defaults.Msg.GetResetsAt().AsTime().UTC().Hour(), "the reset is 00:00 UTC")
	require.True(t, defaults.Msg.GetResetsAt().AsTime().After(time.Now()))

	// The provider profile owns the output budget; zero clears it back to the
	// server default.
	created, err := llmClient.CreateLLMProviderProfile(ctx, withToken(token, &v1pb.CreateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Title:           "integration limits provider",
			Type:            v1pb.LLMProviderType_LLM_PROVIDER_TYPE_OPENAI,
			BaseUrl:         "http://127.0.0.1:1/v1",
			ApiKey:          "integration-key",
			MaxOutputTokens: 4096,
			Models:          []*v1pb.LlmProviderModel{{Name: "integration-model", Enabled: true}},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, int32(4096), created.Msg.GetMaxOutputTokens())
	t.Cleanup(func() {
		_, _ = llmClient.DeleteLLMProviderProfile(context.Background(), withToken(token, &v1pb.DeleteLLMProviderProfileRequest{Name: created.Msg.GetName()}))
	})

	updated, err := llmClient.UpdateLLMProviderProfile(ctx, withToken(token, &v1pb.UpdateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Name:            created.Msg.GetName(),
			MaxOutputTokens: 8192,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"max_output_tokens"}},
	}))
	require.NoError(t, err)
	require.Equal(t, int32(8192), updated.Msg.GetMaxOutputTokens())

	cleared, err := llmClient.UpdateLLMProviderProfile(ctx, withToken(token, &v1pb.UpdateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Name:            created.Msg.GetName(),
			MaxOutputTokens: 0,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"max_output_tokens"}},
	}))
	require.NoError(t, err)
	require.Zero(t, cleared.Msg.GetMaxOutputTokens(), "an explicit zero clears the budget to the server default")

	_, err = llmClient.UpdateLLMProviderProfile(ctx, withToken(token, &v1pb.UpdateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Name:            created.Msg.GetName(),
			MaxOutputTokens: 100,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"max_output_tokens"}},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// The daily and concurrency limits are writable and reported back resolved.
	_, err = fixture.settings.UpdateWorkspaceProfileSetting(ctx, withToken(token, &v1pb.UpdateWorkspaceProfileSettingRequest{
		Setting: &v1pb.WorkspaceProfileSetting{DataQuality: &v1pb.DataQualitySetting{
			EnableLlmAnalysis:        false,
			StoreFailureSamples:      true,
			FailureSampleLimit:       20,
			DailyColumnAnalysisLimit: 77,
			DailyLlmAnalysisLimit:    123,
			LlmConcurrencyLimit:      4,
			MaxColumnsPerAnalysis:    50,
		}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"data_quality"}},
	}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = fixture.settings.UpdateWorkspaceProfileSetting(context.Background(), withToken(token, &v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"data_quality"}},
		}))
	})

	custom, err := fixture.dataQuality.GetQualityLLMUsage(ctx, withToken(token, &v1pb.GetQualityLLMUsageRequest{
		Parent: "workspaces/-",
	}))
	require.NoError(t, err)
	require.Equal(t, int32(123), custom.Msg.GetDailyAnalysisLimit())
	require.Equal(t, int32(4), custom.Msg.GetConcurrencyLimit())
	require.Equal(t, int32(77), custom.Msg.GetDailyColumnAnalysisLimit())
	require.LessOrEqual(t, custom.Msg.GetDailyAnalysisUsed(), custom.Msg.GetDailyAnalysisLimit())
	require.Zero(t, custom.Msg.GetInFlight())
}

// Suggestions are keyed by proposal identity, not appended: queueing the same
// label twice refreshes the one pending row, and closing it as SUPERSEDED lets a
// later pass queue a fresh row. This exercises the partial unique index and the
// upsert against the real database.
func TestQualitySuggestionQueueIsIdempotentRealServerIntegration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// A fresh metadata database, so the shared workspace's other scenarios cannot
	// see this test's rows (and vice versa).
	stores := newIsolatedMetadataStore(ctx, t)
	targetGUID := "instances/it;shop;;users"
	signature := dataquality.LabelSuggestionSignature("email")
	payload := qualityLabelSuggestionPayload("email", storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL)
	queue := func(resourceID string) (*store.QualitySuggestionMessage, bool) {
		queued, created, err := stores.QueueQualitySuggestion(ctx, &store.QualitySuggestionMessage{
			ResourceID: resourceID,
			Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
			Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
			TargetGUID: targetGUID,
			TargetType: storepb.MetaType_TABLE,
			Signature:  signature,
			Payload:    payload,
			Status:     storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
			Provider:   "profiles/integration",
			Model:      "integration-model",
		})
		require.NoError(t, err)
		return queued, created
	}

	firstID, err := common.RandomString(12)
	require.NoError(t, err)
	first, created := queue(firstID)
	require.True(t, created)
	require.Nil(t, first.RefreshedAt)

	secondID, err := common.RandomString(12)
	require.NoError(t, err)
	second, created := queue(secondID)
	require.False(t, created, "the same proposal must refresh the pending row, not append")
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.ResourceID, second.ResourceID)
	require.NotNil(t, second.RefreshedAt)

	pending, err := stores.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
		TargetGUID: &targetGUID,
		Statuses:   []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
		Kinds:      []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL},
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)

	states, err := stores.QualitySuggestionSignatureStates(ctx, targetGUID,
		storepb.MetaType_TABLE, []string{signature})
	require.NoError(t, err)
	require.True(t, states[signature].Pending)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED, states[signature].LastDecision)

	superseded, err := stores.SupersedePendingQualitySuggestions(ctx, targetGUID,
		storepb.MetaType_TABLE, []string{signature}, "the column already has a semantic label", time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(1), superseded)

	// Closing a proposal is not a reviewer decision: the signature has no last
	// decision, so a later pass may still propose it.
	states, err = stores.QualitySuggestionSignatureStates(ctx, targetGUID,
		storepb.MetaType_TABLE, []string{signature})
	require.NoError(t, err)
	require.False(t, states[signature].Pending)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED, states[signature].LastDecision)

	againID, err := common.RandomString(12)
	require.NoError(t, err)
	_, created = queue(againID)
	require.True(t, created, "a superseded proposal may be proposed again")

	// A different identity is a different pending proposal.
	otherID, err := common.RandomString(12)
	require.NoError(t, err)
	other, created, err := stores.QueueQualitySuggestion(ctx, &store.QualitySuggestionMessage{
		ResourceID: otherID,
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGUID: targetGUID,
		TargetType: storepb.MetaType_TABLE,
		Signature:  dataquality.LabelSuggestionSignature("phone"),
		Payload:    qualityLabelSuggestionPayload("phone", storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_PHONE),
		Status:     storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
	})
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, first.ID, other.ID)
}

// An identical drafting request is idempotent while the first job is active: the
// second call returns the existing job instead of inserting a second row. The
// partial unique index is what makes this hold under concurrent submissions.
func TestQualitySuggestionJobQueueIsIdempotentRealServerIntegration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stores := newIsolatedMetadataStore(ctx, t)
	targetGUID := "instances/it;shop;;users"

	fingerprint := "integration-fingerprint"
	queue := func(resourceID string) (*store.QualitySuggestionJobMessage, bool) {
		job, created, err := stores.QueueQualitySuggestionJob(ctx, &store.QualitySuggestionJobMessage{
			ResourceID:         resourceID,
			Status:             storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
			Intent:             "email must not be empty",
			Language:           "en",
			RequestFingerprint: fingerprint,
			Targets: &storepb.QualitySuggestionJobTargets{Targets: []*storepb.QualitySuggestionJobTarget{{
				TargetGuid: targetGUID,
				TargetType: int32(storepb.MetaType_TABLE),
				State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
			}}},
		})
		require.NoError(t, err)
		return job, created
	}

	firstID, err := common.RandomString(12)
	require.NoError(t, err)
	first, created := queue(firstID)
	require.True(t, created)

	secondID, err := common.RandomString(12)
	require.NoError(t, err)
	second, created := queue(secondID)
	require.False(t, created)
	require.Equal(t, first.ResourceID, second.ResourceID)

	active, err := stores.GetActiveQualitySuggestionJobByFingerprint(ctx, fingerprint)
	require.NoError(t, err)
	require.NotNil(t, active)
	require.Equal(t, first.ResourceID, active.ResourceID)
}

// Reopening is the rollback of a rule-creation claim: it returns an ACCEPTED draft
// to PENDING and clears the refresh stamp, so the reviewer can retry it. This
// exercises the ACCEPTED-only guard against the real database.
func TestQualitySuggestionReopenRollsBackTheClaimRealServerIntegration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stores := newIsolatedMetadataStore(ctx, t)
	targetGUID := "instances/it;shop;;users"
	signature := dataquality.LabelSuggestionSignature("email")
	payload := qualityLabelSuggestionPayload("email", storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL)

	resourceID, err := common.RandomString(12)
	require.NoError(t, err)
	queued, created, err := stores.QueueQualitySuggestion(ctx, &store.QualitySuggestionMessage{
		ResourceID: resourceID,
		Kind:       storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		Source:     storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGUID: targetGUID,
		TargetType: storepb.MetaType_TABLE,
		Signature:  signature,
		Payload:    payload,
		Status:     storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
	})
	require.NoError(t, err)
	require.True(t, created)

	decided, err := stores.DecideQualitySuggestion(ctx, queued.ResourceID,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
		int32(1), "", time.Now())
	require.NoError(t, err)
	require.Nil(t, decided.RefreshedAt)

	reopened, err := stores.ReopenQualitySuggestion(ctx, queued.ResourceID)
	require.NoError(t, err)
	require.Equal(t, storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, reopened.Status)
	require.Nil(t, reopened.DecidedBy)
	require.Nil(t, reopened.DecidedAt)
	require.Empty(t, reopened.DecisionNote)
	require.Nil(t, reopened.RefreshedAt)

	// Reopening is guarded: a PENDING row is not reopened a second time.
	_, err = stores.ReopenQualitySuggestion(ctx, queued.ResourceID)
	require.Error(t, err)
}

// TestDataQualityTargetDirectoryRealServerIntegration covers the P1 aggregate
// reads: the directory groups rules by target in SQL, and one Get returns the
// target's rules and health without the client paging through every rule.
func TestDataQualityTargetDirectoryRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, true)
	ordersGUID := waitForMetaGUIDByName(fixture.ctx, t, fixture.env,
		fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase),
		storepb.MetaType_TABLE, "orders")

	fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		fixture.usersGUID, qualityRowCountConfig(1))
	fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, qualityColumnConfig("name"))
	fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID, qualityColumnConfig("amount"))

	list := func(request *v1pb.ListQualityTargetsRequest) *v1pb.ListQualityTargetsResponse {
		t.Helper()
		response, err := fixture.dataQuality.ListQualityTargets(fixture.ctx,
			withToken(fixture.env.AdminToken(), request))
		require.NoError(t, err, "list the quality targets")
		return response.Msg
	}

	response := list(&v1pb.ListQualityTargetsRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
		OrderBy:  v1pb.QualityTargetOrder_QUALITY_TARGET_ORDER_NAME,
	})
	require.Len(t, response.GetQualityTargets(), 2, "one row per target, not per rule")
	byGUID := make(map[string]*v1pb.QualityTargetSummary, len(response.GetQualityTargets()))
	for _, target := range response.GetQualityTargets() {
		byGUID[target.GetTarget().GetMetaGuid()] = target
	}
	users := byGUID[fixture.usersGUID]
	require.NotNil(t, users)
	require.Equal(t, int32(2), users.GetRuleCount())
	require.Equal(t, int32(2), users.GetEnabledRuleCount())
	require.Equal(t, int32(0), users.GetPausedRuleCount())
	require.Equal(t, int32(1), users.GetColumnCount(),
		"the checked column is read from the config, not the path snapshot")
	require.Equal(t, "users", users.GetTarget().GetPath().GetObject())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, users.GetWorstStatus(),
		"no rule has run yet")
	require.Nil(t, users.GetLastRunTime())
	require.False(t, users.GetSchemaChanged())

	orders := byGUID[ordersGUID]
	require.NotNil(t, orders)
	require.Equal(t, int32(1), orders.GetRuleCount())
	require.Equal(t, int32(1), orders.GetColumnCount())
	require.False(t, users.GetOrphaned(), "a listed target still resolves")
	require.False(t, orders.GetOrphaned())

	// A worst-status value outside the enum is refused instead of silently matching
	// the "never run" rank.
	_, invalidErr := fixture.dataQuality.ListQualityTargets(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.ListQualityTargetsRequest{
			Parent:      "workspaces/-",
			PageSize:    100,
			WorstStatus: v1pb.QualityStatus(99),
		}))
	require.Error(t, invalidErr)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(invalidErr))

	// The search and the "worst status" filter both narrow the directory.
	require.Len(t, list(&v1pb.ListQualityTargetsRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
		Search:   "orders",
	}).GetQualityTargets(), 1)
	require.Empty(t, list(&v1pb.ListQualityTargetsRequest{
		Parent:      "workspaces/-",
		PageSize:    100,
		WorstStatus: v1pb.QualityStatus_QUALITY_STATUS_FAIL,
	}).GetQualityTargets(), "nothing has run, so no target is failing")

	// The page token pages the directory rather than the rules behind it.
	paged := list(&v1pb.ListQualityTargetsRequest{Parent: "workspaces/-", PageSize: 1})
	require.Len(t, paged.GetQualityTargets(), 1)
	require.NotEmpty(t, paged.GetNextPageToken())
	next := list(&v1pb.ListQualityTargetsRequest{Parent: "workspaces/-", PageSize: 1, PageToken: paged.GetNextPageToken()})
	require.Len(t, next.GetQualityTargets(), 1)
	require.NotEqual(t, paged.GetQualityTargets()[0].GetTarget().GetMetaGuid(), next.GetQualityTargets()[0].GetTarget().GetMetaGuid())

	// One Get returns the target's rules, its summary and its issues.
	get, err := fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
		}))
	require.NoError(t, err, "get the quality target")
	require.Len(t, get.Msg.GetQualityRules(), 2)
	require.Equal(t, int32(2), get.Msg.GetSummary().GetRuleCount())
	require.Equal(t, int32(1), get.Msg.GetSummary().GetColumnCount(),
		"the workspace summary counts the config's checked column")
	require.Equal(t, "users", get.Msg.GetSummary().GetTarget().GetPath().GetObject())
	require.False(t, get.Msg.GetSummary().GetOrphaned())
	require.Empty(t, get.Msg.GetOrphanedReason())
	require.Empty(t, get.Msg.GetOpenIssues())
	// The resolved metadata supplies the target's columns, even for object types
	// that have no COLUMN registry rows (views, external tables).
	columnNames := make([]string, 0, len(get.Msg.GetTargetColumns()))
	for _, column := range get.Msg.GetTargetColumns() {
		require.NotEmpty(t, column.GetType(), "a resolved column carries its engine type")
		columnNames = append(columnNames, column.GetName())
	}
	require.ElementsMatch(t, []string{"id", "name", "age"}, columnNames)

	// A target whose registry row is gone still has rules, so the directory lists it
	// and marks it orphaned; the workspace stays readable from the path snapshot but
	// has no resolved columns.
	orphanGUID := "orphan-instance;missing-db;public;gone"
	orphanResourceID, err := common.RandomString(12)
	require.NoError(t, err)
	minRows := int64(1)
	_, err = fixture.env.Store.CreateQualityRule(fixture.ctx, &store.QualityRuleMessage{
		ResourceID: orphanResourceID,
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
		Severity:         storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		State:            storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		ScheduleInterval: time.Hour,
	})
	require.NoError(t, err)
	withOrphan := list(&v1pb.ListQualityTargetsRequest{Parent: "workspaces/-", PageSize: 100})
	orphanSummary := (*v1pb.QualityTargetSummary)(nil)
	for _, target := range withOrphan.GetQualityTargets() {
		if target.GetTarget().GetMetaGuid() == orphanGUID {
			orphanSummary = target
		}
	}
	require.NotNil(t, orphanSummary, "a target with rules stays in the directory")
	require.True(t, orphanSummary.GetOrphaned(), "the directory marks a target whose registry row is gone")
	orphanTarget, err := fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: orphanGUID,
			MetaType:   v1pb.MetaType_TABLE,
		}))
	require.NoError(t, err, "an orphaned target with rules stays readable")
	require.True(t, orphanTarget.Msg.GetSummary().GetOrphaned())
	require.NotEmpty(t, orphanTarget.Msg.GetOrphanedReason())
	require.Len(t, orphanTarget.Msg.GetQualityRules(), 1)
	require.Empty(t, orphanTarget.Msg.GetTargetColumns(), "an orphaned target has no resolved columns")

	// NOT_EVALUATED and SKIPPED are distinct worst results: a row-count-change check
	// with no baseline is NOT_EVALUATED, so filtering SKIPPED must not match it.
	fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID, qualityRowCountChangeConfig(100))
	// The last rule of a target with no execution is the one just created; find it
	// by its title prefix instead of guessing the resource id.
	ordersRules, err := fixture.dataQuality.ListQualityRules(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.ListQualityRulesRequest{
			Parent:     "workspaces/-",
			TargetGuid: ordersGUID,
		}))
	require.NoError(t, err)
	var changeRule string
	for _, rule := range ordersRules.Msg.GetQualityRules() {
		if rule.GetRuleType() == v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE {
			changeRule = rule.GetName()
		}
	}
	require.NotEmpty(t, changeRule)
	firstExecution := fixture.runToCompletion(t, changeRule)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, firstExecution.GetOutcome())

	notEvaluated := list(&v1pb.ListQualityTargetsRequest{
		Parent:      "workspaces/-",
		PageSize:    100,
		WorstStatus: v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	})
	require.Len(t, notEvaluated.GetQualityTargets(), 1)
	require.Equal(t, ordersGUID, notEvaluated.GetQualityTargets()[0].GetTarget().GetMetaGuid())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		notEvaluated.GetQualityTargets()[0].GetWorstStatus())
	require.Empty(t, list(&v1pb.ListQualityTargetsRequest{
		Parent:      "workspaces/-",
		PageSize:    100,
		WorstStatus: v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
	}).GetQualityTargets(), "a not-evaluated target is not a skipped one")

	// An archived check keeps its open issue in the ledger, but the target workspace
	// no longer counts or lists it (and every listed issue still resolves its rule
	// name).
	failing := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		fixture.usersGUID, qualityRowCountConfig(1_000_000))
	fixture.runToCompletion(t, failing.GetName())
	fixture.waitForIssue(t, failing.GetName(), v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN)
	withIssue, err := fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
		}))
	require.NoError(t, err)
	require.Equal(t, int32(1), withIssue.Msg.GetSummary().GetOpenIssueCount())
	require.Len(t, withIssue.Msg.GetOpenIssues(), 1)
	require.Equal(t, failing.GetName(), withIssue.Msg.GetOpenIssues()[0].GetRule(),
		"an open issue always resolves its rule name")

	_, err = fixture.dataQuality.DeleteQualityRule(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.DeleteQualityRuleRequest{Name: failing.GetName()}))
	require.NoError(t, err)
	afterArchive, err := fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
		}))
	require.NoError(t, err)
	require.Equal(t, int32(2), afterArchive.Msg.GetSummary().GetRuleCount())
	require.Equal(t, int32(0), afterArchive.Msg.GetSummary().GetOpenIssueCount(),
		"an archived rule's issue is not part of the live workspace")
	require.Empty(t, afterArchive.Msg.GetOpenIssues())
	require.Empty(t, list(&v1pb.ListQualityTargetsRequest{
		Parent:       "workspaces/-",
		PageSize:     100,
		HasOpenIssue: true,
	}).GetQualityTargets(), "the directory counts only live rules' issues")

	// A target that neither has rules nor exists in the registry is NotFound.
	_, err = fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: fmt.Sprintf("%s;%s;%s;%s", fixture.instanceID, fixture.sourceDatabase, "public", "missing_table"),
			MetaType:   v1pb.MetaType_TABLE,
		}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestDataQualityContractRealServerIntegration covers the target-level contract:
// settings validation, publish/version/diff, the no-op republish, history and the
// filter the runner ANDs into every rule on the target.
func TestDataQualityContractRealServerIntegration(t *testing.T) {
	fixture := setupQualityMySQLFixture(t, true)
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		fixture.usersGUID, qualityRowCountConfig(1))

	getContract := func() *v1pb.QualityContract {
		t.Helper()
		response, err := fixture.dataQuality.GetQualityContract(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.GetQualityContractRequest{
				Parent:     "workspaces/-",
				TargetGuid: fixture.usersGUID,
				MetaType:   v1pb.MetaType_TABLE,
			}))
		require.NoError(t, err, "get the quality contract")
		return response.Msg
	}
	publish := func(note string) *v1pb.PublishQualityContractResponse {
		t.Helper()
		response, err := fixture.dataQuality.PublishQualityContract(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.PublishQualityContractRequest{
				Parent:     "workspaces/-",
				TargetGuid: fixture.usersGUID,
				MetaType:   v1pb.MetaType_TABLE,
				Note:       note,
			}))
		require.NoError(t, err, "publish the quality contract")
		return response.Msg
	}

	contract := getContract()
	require.Equal(t, int32(0), contract.GetPublishedVersion())
	require.True(t, contract.GetHasUnpublishedChanges())
	require.Equal(t, int32(1), contract.GetCheckCount())
	require.Equal(t, int32(1), contract.GetChangeSummary().GetAdded())

	// The target filter is validated against the target's columns when saved.
	updateSettings := func(settings *v1pb.QualityContractSettings) *v1pb.QualityContract {
		t.Helper()
		response, err := fixture.dataQuality.UpdateQualityContract(fixture.ctx,
			withToken(fixture.env.AdminToken(), &v1pb.UpdateQualityContractRequest{
				Parent:     "workspaces/-",
				TargetGuid: fixture.usersGUID,
				MetaType:   v1pb.MetaType_TABLE,
				Settings:   settings,
			}))
		require.NoError(t, err, "update the quality contract settings")
		return response.Msg
	}
	settings := &v1pb.QualityContractSettings{
		Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
			Column: "id",
			Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT,
			Values: []string{"0"},
		}}},
		DefaultScheduleIntervalSeconds: 3600,
		DefaultSeverity:                v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
	}
	updated := updateSettings(settings)
	require.Len(t, updated.GetSettings().GetFilter().GetClauses(), 1)
	require.Equal(t, int64(3600), updated.GetSettings().GetDefaultScheduleIntervalSeconds())
	require.True(t, updated.GetHasUnpublishedChanges(), "settings changed after the first read")

	_, err := fixture.dataQuality.UpdateQualityContract(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.UpdateQualityContractRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
			Settings: &v1pb.QualityContractSettings{
				Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
					Column: "no_such_column",
					Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
					Values: []string{"x"},
				}}},
			},
		}))
	require.Error(t, err, "a filter naming a missing column is rejected")
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	first := publish("first")
	require.False(t, first.GetNoChanges())
	require.Equal(t, int32(1), first.GetRevision().GetVersion())
	require.Equal(t, int32(1), first.GetRevision().GetCheckCount())
	require.Equal(t, int32(1), first.GetRevision().GetChangeSummary().GetAdded())
	require.Equal(t, int32(1), first.GetContract().GetPublishedVersion())
	require.False(t, first.GetContract().GetHasUnpublishedChanges())
	require.NotEmpty(t, first.GetContract().GetPublishedBy())
	require.Equal(t, "first", first.GetContract().GetPublishNote())

	republished := publish("again")
	require.True(t, republished.GetNoChanges())
	require.Equal(t, int32(1), republished.GetRevision().GetVersion(), "republishing unchanged state keeps the version")

	// A new rule is an unpublished addition until the next publish.
	fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		fixture.usersGUID, qualityColumnConfig("name"))
	drifted := getContract()
	require.True(t, drifted.GetHasUnpublishedChanges())
	require.Equal(t, int32(2), drifted.GetCheckCount())
	require.Equal(t, int32(1), drifted.GetChangeSummary().GetAdded())

	second := publish("second")
	require.False(t, second.GetNoChanges())
	require.Equal(t, int32(2), second.GetRevision().GetVersion())
	require.Equal(t, int32(2), second.GetRevision().GetCheckCount())
	// The diff counts are computed inside the publish transaction against the
	// previous version, so this version records exactly one addition.
	require.Equal(t, int32(1), second.GetRevision().GetChangeSummary().GetAdded())
	require.Equal(t, int32(0), second.GetRevision().GetChangeSummary().GetModified())
	require.Equal(t, int32(0), second.GetRevision().GetChangeSummary().GetRemoved())
	require.Equal(t, int32(0), second.GetContract().GetChangeSummary().GetAdded(),
		"the returned view compares the live rules with the version just published")

	// History is newest first and a version can be read with its checks.
	history, err := fixture.dataQuality.ListQualityContractRevisions(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.ListQualityContractRevisionsRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
			PageSize:   10,
		}))
	require.NoError(t, err)
	require.Len(t, history.Msg.GetRevisions(), 2)
	require.Equal(t, int32(2), history.Msg.GetRevisions()[0].GetVersion())
	require.Equal(t, int32(1), history.Msg.GetRevisions()[1].GetVersion())

	version2, err := fixture.dataQuality.GetQualityContractRevision(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityContractRevisionRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
			Version:    2,
		}))
	require.NoError(t, err)
	require.Len(t, version2.Msg.GetChecks(), 2)
	require.Contains(t, []string{version2.Msg.GetChecks()[0].GetColumn(), version2.Msg.GetChecks()[1].GetColumn()}, "name",
		"a published check records the config's checked column")
	version1, err := fixture.dataQuality.GetQualityContractRevision(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityContractRevisionRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
			Version:    1,
		}))
	require.NoError(t, err)
	require.Len(t, version1.Msg.GetChecks(), 1)

	// The target filter reaches the runner's compiled statement: a row-count rule
	// reads COUNT(*) over the filtered rows.
	execution := fixture.runToCompletion(t, rule.GetName())
	require.Contains(t, execution.GetExecutedSql(), "`id` > ?", "the contract filter is ANDed into the rule's SQL")

	// The workspace gets each rule's recent executions in the same read.
	detail, err := fixture.dataQuality.GetQualityTarget(fixture.ctx,
		withToken(fixture.env.AdminToken(), &v1pb.GetQualityTargetRequest{
			Parent:     "workspaces/-",
			TargetGuid: fixture.usersGUID,
			MetaType:   v1pb.MetaType_TABLE,
		}))
	require.NoError(t, err)
	historyByRule := make(map[string]*v1pb.QualityRuleHistory, len(detail.Msg.GetRuleHistory()))
	for _, history := range detail.Msg.GetRuleHistory() {
		historyByRule[history.GetRule()] = history
	}
	require.Contains(t, historyByRule, rule.GetName())
	require.Len(t, historyByRule[rule.GetName()].GetExecutions(), 1)
}
