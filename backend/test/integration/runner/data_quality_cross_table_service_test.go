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

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// TestDataQualityCrossDatabaseReconciliationRealServerIntegration proves P2-5 end
// to end on PostgreSQL: a reconciliation whose reference relation lives in another
// database of the same instance reads each side over its own read-only session,
// records both statements as evidence, and the dry run shows both. A reference in
// the target's own database stays one statement, which the second rule pins beside
// it.
func TestDataQualityCrossDatabaseReconciliationRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	referenceDatabase := sourceDatabase + "_ref"
	referenceDatabaseName := common.FormatDatabase(instanceID, referenceDatabase)
	terminateReferenceDatabase := func() {
		_ = env.ExecPostgres(context.Background(), "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(referenceDatabase)))
	}
	terminateReferenceDatabase()
	require.NoError(t, env.ExecPostgres(ctx, "postgres",
		fmt.Sprintf("CREATE DATABASE %s;", quotePostgresIdentifier(referenceDatabase))))
	t.Cleanup(func() {
		terminateReferenceDatabase()
		_ = env.ExecPostgres(context.Background(), "postgres",
			fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(referenceDatabase)))
	})

	// The target side and a same-database reference live in the fixture database;
	// the other-database reference lives in the second one.
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.cross_orders (
  id BIGSERIAL PRIMARY KEY,
  amount NUMERIC(12,2) NOT NULL
);
INSERT INTO public.cross_orders (amount) VALUES (10), (20), (30);
CREATE TABLE public.cross_local_ref (
  id BIGSERIAL PRIMARY KEY,
  amount NUMERIC(12,2) NOT NULL
);
INSERT INTO public.cross_local_ref (amount) VALUES (10), (20), (30);
`))
	require.NoError(t, env.ExecPostgres(ctx, referenceDatabase, `
CREATE TABLE public.cross_remote_ref (
  id BIGSERIAL PRIMARY KEY,
  amount NUMERIC(12,2) NOT NULL
);
INSERT INTO public.cross_remote_ref (amount) VALUES (10), (20), (30);
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
	_ = env.EnsureDatabaseVisible(ctx, t, common.FormatInstance(instanceID), referenceDatabase)
	env.SyncDatabase(ctx, t, referenceDatabaseName)

	ordersGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_TABLE, "cross_orders")
	localRefGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_TABLE, "cross_local_ref")
	remoteRefGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, referenceDatabase), storepb.MetaType_TABLE, "cross_remote_ref")

	fixture := &qualityFixture{
		env:         env,
		ctx:         ctx,
		dataQuality: v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 30 * time.Second}, env.BaseURL),
	}
	reconciliation := func(referenceGUID string) *v1pb.QualityRuleConfig {
		return &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_CrossTable{
			CrossTable: &v1pb.CrossTableConfig{
				Reference:     &v1pb.CrossTableReference{MetaGuid: referenceGUID, MetaType: v1pb.MetaType_TABLE},
				LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
			},
		}}
	}

	// The dry run proves both statements executable and shows both, which is what
	// makes the editor honest about a reconciliation reading another database.
	previewConfig := reconciliation(remoteRefGUID)
	preview, err := fixture.dataQuality.PreviewQualityRule(ctx, withToken(env.AdminToken(), &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "cross-database reconciliation preview",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			Target:    &v1pb.QualityTarget{MetaGuid: ordersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    previewConfig,
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.NoError(t, err, "the two-statement reconciliation must dry-run")
	require.Contains(t, preview.Msg.GetCompiledSql(), "cross_orders")
	require.NotContains(t, preview.Msg.GetCompiledSql(), "cross_remote_ref",
		"the reference side runs on its own connection, not in the target's statement")
	require.Contains(t, preview.Msg.GetReferenceSql(), "cross_remote_ref")

	remoteRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID, reconciliation(remoteRefGUID))
	remoteExecution := fixture.runToCompletion(t, remoteRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, remoteExecution.GetOutcome(),
		"two equal row counts reconcile: %s", remoteExecution.GetErrorMessage())
	require.Contains(t, remoteExecution.GetExecutedSql(), "cross_orders")
	require.Contains(t, remoteExecution.GetReferenceExecutedSql(), "cross_remote_ref",
		"the execution records the statement that read the other database")

	// The same-database reference shares the target's connection: one statement,
	// no second session, no reference evidence.
	localRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID, reconciliation(localRefGUID))
	localExecution := fixture.runToCompletion(t, localRule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, localExecution.GetOutcome(),
		"two equal row counts reconcile: %s", localExecution.GetErrorMessage())
	require.Contains(t, localExecution.GetExecutedSql(), "cross_orders")
	require.Contains(t, localExecution.GetExecutedSql(), "cross_local_ref")
	require.Empty(t, localExecution.GetReferenceExecutedSql())

	// The reference reads a second connection on the target's own instance, and the
	// target's scan holds one slot for its whole query phase. With a ceiling of one the
	// check could never open that session, so saving it is refused instead of being
	// skipped forever.
	require.NoError(t, env.SetInstanceMaximumConnections(ctx, common.FormatInstance(instanceID), 1))
	_, err = fixture.dataQuality.CreateQualityRule(ctx, withToken(env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "reconciliation without a second connection slot",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			Target:    &v1pb.QualityTarget{MetaGuid: ordersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    reconciliation(remoteRefGUID),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "at least 2")
	require.NoError(t, env.SetInstanceMaximumConnections(ctx, common.FormatInstance(instanceID), 10))

	// A reference the registry does not have is refused when the rule is saved: a
	// rule that could only ever record ERROR(UNSUPPORTED) never reaches the queue.
	missingGUID := fmt.Sprintf("%s;%s;public;cross_missing", instanceID, referenceDatabase)
	_, err = fixture.dataQuality.CreateQualityRule(ctx, withToken(env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "reconciliation against a missing reference",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			Target:    &v1pb.QualityTarget{MetaGuid: ordersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    reconciliation(missingGUID),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "does not exist or was dropped")
}

// TestDataQualityCrossInstanceReconciliationRealServerIntegration proves the
// cross-instance half of P2-5: the reference relation belongs to a second instance
// row, so the dry run and the execution each open a read-only session on that
// instance (the second one under its own connection lease).
func TestDataQualityCrossInstanceReconciliationRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	// The second instance keeps a short ID of its own: the shared fixture's generated
	// instance ID is already at the identifier length limit.
	referenceInstanceID := "it-pg-ref-" + strings.TrimPrefix(postgresServiceSourceDatabaseName(t), "it_app_")
	_, err := env.CreatePostgresInstance(ctx, referenceInstanceID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = env.DeleteInstance(context.Background(), common.FormatInstance(referenceInstanceID))
	})
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.cross_instance_orders (
  id BIGSERIAL PRIMARY KEY,
  amount NUMERIC(12,2) NOT NULL
);
INSERT INTO public.cross_instance_orders (amount) VALUES (10), (20), (30);
`))
	for _, id := range []string{instanceID, referenceInstanceID} {
		_, err := env.CreateDataSource(ctx, common.FormatInstance(id), &v1pb.DataSource{
			Type:     v1pb.DataSourceType_READ_ONLY,
			Username: "postgres",
			Password: "postgres",
			Host:     env.PostgresHost,
			Port:     env.PostgresPort,
		}, "readonly", false)
		require.NoError(t, err)
	}
	env.SyncDatabase(ctx, t, databaseName)
	_ = env.EnsureDatabaseVisible(ctx, t, common.FormatInstance(referenceInstanceID), sourceDatabase)
	env.SyncDatabase(ctx, t, common.FormatDatabase(referenceInstanceID, sourceDatabase))

	ordersGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_TABLE, "cross_instance_orders")
	referenceGUID := waitForMetaGUIDByName(ctx, t, env,
		fmt.Sprintf("%s;%s", referenceInstanceID, sourceDatabase), storepb.MetaType_TABLE, "cross_instance_orders")
	require.NotEqual(t, ordersGUID, referenceGUID)

	fixture := &qualityFixture{
		env:         env,
		ctx:         ctx,
		dataQuality: v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 30 * time.Second}, env.BaseURL),
	}
	config := &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_CrossTable{
		CrossTable: &v1pb.CrossTableConfig{
			Reference:     &v1pb.CrossTableReference{MetaGuid: referenceGUID, MetaType: v1pb.MetaType_TABLE},
			LeftFunction:  v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
			RightFunction: v1pb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
		},
	}}
	preview, err := fixture.dataQuality.PreviewQualityRule(ctx, withToken(env.AdminToken(), &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "cross-instance reconciliation preview",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			Target:    &v1pb.QualityTarget{MetaGuid: ordersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    config,
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.NoError(t, err, "a cross-instance reconciliation must dry-run")
	require.NotEmpty(t, preview.Msg.GetReferenceSql())

	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
		v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		ordersGUID, config)
	execution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, execution.GetOutcome(),
		"the same table in another instance reconciles: %s", execution.GetErrorMessage())
	require.Contains(t, execution.GetReferenceExecutedSql(), "cross_instance_orders")

	// The reference instance must hold a READ_ONLY data source: without it the
	// second session could only ever fail, so creating the rule is refused.
	require.NoError(t, env.DeleteDataSource(ctx, common.FormatDataSource(referenceInstanceID, "readonly")))
	_, err = fixture.dataQuality.CreateQualityRule(ctx, withToken(env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "cross-instance reconciliation without a read-only source",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			Target:    &v1pb.QualityTarget{MetaGuid: ordersGUID, MetaType: v1pb.MetaType_TABLE},
			Config:    config,
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "reference")
}
