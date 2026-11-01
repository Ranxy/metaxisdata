//go:build integration

package runner

import (
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// This scenario pins the execution semantics added with P1-3 against a real
// server and a real MySQL source: a time variable in a filter is stored as the
// token and rendered per execution, a sampled check reads a bounded relation and
// records that its verdict came from a sample, a forbidden sampling is refused at
// save time, and a per-check timeout round-trips and is enforced as a bound.
//
// The source rows are chosen so the sampled verdict is deterministic: every
// accepted-values row fails, so the sample always fails whatever rows it reads.

const executionSemanticsSeedDDL = `
CREATE TABLE dq_semantics (
  id INT PRIMARY KEY,
  email VARCHAR(64) NULL,
  created_at DATETIME NOT NULL
);
INSERT INTO dq_semantics (id, email, created_at) VALUES
  (1, 'a@example.com', NOW()),
  (2, NULL,            NOW()),
  (3, NULL,            NOW()),
  (4, 'd@example.com', '2020-01-01 00:00:00');
`

func TestDataQualityExecutionSemanticsMySQLRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";"+executionSemanticsSeedDDL), "seed the execution-semantics source")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)
	prefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	guid := waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_semantics")

	// A filter value that is a time variable is stored as the token and rendered
	// from the execution's own instant. The dry run shows the rendering, and the
	// stored rule keeps the token so the next run resolves it again.
	variableConfig := &v1pb.QualityRuleConfig{
		Config: &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{MinRows: proto.Int64(1)}},
		Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
			Column: "created_at",
			Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE,
			Values: []string{"${today-7d}"},
		}}},
	}
	before := time.Now().UTC().Format("2006-01-02")
	preview, err := fixture.dataQuality.PreviewQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			Target:    &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config:    variableConfig,
		},
	}))
	require.NoError(t, err, "preview a filter with a time variable")
	require.Contains(t, preview.Msg.GetCompiledSql(), "`created_at` >=")
	wantRendering := []string{
		mustShiftDate(t, before, -7),
		mustShiftDate(t, time.Now().UTC().Format("2006-01-02"), -7),
	}
	require.Contains(t, wantRendering, preview.Msg.GetParameters()[0],
		"the preview renders ${today-7d} as the date seven days ago")

	variableRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid, variableConfig)
	require.Equal(t, "${today-7d}", variableRule.GetConfig().GetFilter().GetClauses()[0].GetValues()[0],
		"the stored rule keeps the token, not the rendered date")
	execution := fixture.runToCompletion(t, variableRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, execution.Outcome)
	require.Contains(t, execution.ExecutedSql, "`created_at` >=")
	require.NotContains(t, execution.ExecutedSql, "${", "the executed statement carries the rendered value")

	// A custom SQL check keeps its token on the rule but executes a generated date
	// literal; the compiled statement is the same statement the save path parsed.
	customConfig := &v1pb.QualityRuleConfig{
		Config: &v1pb.QualityRuleConfig_CustomSql{CustomSql: &v1pb.CustomSQLConfig{
			Sql: "SELECT id FROM dq_semantics WHERE created_at >= ${today-7d}",
		}},
	}
	customPreview, err := fixture.dataQuality.PreviewQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.PreviewQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			Target:    &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config:    customConfig,
		},
	}))
	require.NoError(t, err, "preview a custom SQL check with a time variable")
	require.Regexp(t, `created_at >= '[0-9]{4}-[0-9]{2}-[0-9]{2}'`, customPreview.Msg.GetCompiledSql())
	customRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid, customConfig)
	customExecution := fixture.runToCompletion(t, customRule.Name)
	require.Regexp(t, `created_at >= '[0-9]{4}-[0-9]{2}-[0-9]{2}'`, customExecution.ExecutedSql,
		"the executed statement carries the rendered date, not the token")
	require.NotContains(t, customExecution.ExecutedSql, "${")

	// The single-pass duplicate rewrite keeps NULL keys out of the duplicate count
	// while still counting them in the total: four rows, two of them NULL, one
	// duplicate pair of NULLs only when count_nulls asks for it.
	uniqueConfig := &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Unique{
		Unique: &v1pb.UniqueConfig{Column: "email"},
	}}
	uniqueRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid, uniqueConfig)
	uniqueExecution := fixture.runToCompletion(t, uniqueRule.Name)
	require.Equal(t, int64(0), uniqueExecution.FailedRows, "NULL keys are not duplicates")
	require.Equal(t, int64(4), uniqueExecution.TotalRows, "NULL rows still count in the total")
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, uniqueExecution.Outcome)

	countNullsConfig := &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Unique{
		Unique: &v1pb.UniqueConfig{Column: "email", CountNulls: true},
	}}
	countNullsRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid, countNullsConfig)
	countNullsExecution := fixture.runToCompletion(t, countNullsRule.Name)
	require.Equal(t, int64(1), countNullsExecution.FailedRows, "the second NULL is the duplicate")
	require.Equal(t, int64(4), countNullsExecution.TotalRows)

	// A row limit reads a bounded relation and records the sample. Three of the
	// four rows are recent, so the row count filter alone would pass; the sampled
	// accepted-values check fails on every row it reads.
	sampledConfig := &v1pb.QualityRuleConfig{
		Config: &v1pb.QualityRuleConfig_AcceptedValues{AcceptedValues: &v1pb.AcceptedValuesConfig{
			Column: "email", Values: []string{"never@example.com"},
		}},
		// A sampled and filtered rule at once: the filter binds inside the bounded
		// relation, which is the arg-order path that only a real execution proves.
		Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
			Column: "created_at",
			Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE,
			Values: []string{"${today-7d}"},
		}}},
		Sampling: &v1pb.QualityRuleSampling{Method: &v1pb.QualityRuleSampling_MaxRows{MaxRows: 2}},
	}
	sampledRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid, sampledConfig)
	require.Equal(t, int64(2), sampledRule.GetConfig().GetSampling().GetMaxRows(),
		"the sampling intent round-trips through the rule")
	sampledExecution := fixture.runToCompletion(t, sampledRule.Name)
	require.True(t, sampledExecution.Sampled, "the execution records that its verdict came from a sample")
	require.Equal(t, int64(2), sampledExecution.SampleRows)
	require.Equal(t, int64(2), sampledExecution.TotalRows)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, sampledExecution.Outcome)
	require.Contains(t, sampledExecution.ExecutedSql, "`created_at` >=")
	require.Contains(t, sampledExecution.ExecutedSql, "LIMIT 2")

	// Sampling a rule type whose verdict describes the whole relation is refused at
	// save time, with the reason, rather than producing a meaningless verdict.
	_, err = fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "integration sampled row count",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:    &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config: &v1pb.QualityRuleConfig{
				Config:   &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{MinRows: proto.Int64(1)}},
				Sampling: &v1pb.QualityRuleSampling{Method: &v1pb.QualityRuleSampling_MaxRows{MaxRows: 2}},
			},
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "not the table's row count")

	// The MySQL family has no table sampling clause, so a percentage sample is
	// refused with a pointer to the row limit.
	_, err = fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "integration percentage sample",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			Target:    &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config: &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_AcceptedValues{AcceptedValues: &v1pb.AcceptedValuesConfig{
					Column: "email", Values: []string{"a@example.com"},
				}},
				Sampling: &v1pb.QualityRuleSampling{Method: &v1pb.QualityRuleSampling_Percent{Percent: 10}},
			},
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "use a row limit instead")

	// A per-check timeout round-trips, and one outside the range is refused.
	timedConfig := &v1pb.QualityRuleConfig{
		Config:         &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{MinRows: proto.Int64(1)}},
		TimeoutSeconds: 30,
	}
	timedRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid, timedConfig)
	require.Equal(t, int32(30), timedRule.GetConfig().GetTimeoutSeconds())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, fixture.runToCompletion(t, timedRule.Name).Outcome)

	_, err = fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "integration timeout above the cap",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Target:    &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
			Config: &v1pb.QualityRuleConfig{
				Config:         &v1pb.QualityRuleConfig_RowCount{RowCount: &v1pb.RowCountConfig{MinRows: proto.Int64(1)}},
				TimeoutSeconds: 301,
			},
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "timeout_seconds must be between 1 and 300")
}

// mustShiftDate shifts a YYYY-MM-DD date by days and returns the same form.
func mustShiftDate(t *testing.T, date string, days int) string {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", date)
	require.NoError(t, err)
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}
