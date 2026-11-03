package dataquality

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// passingSampleFor builds the passing-row statement of one rule type, failing the
// test when the type has none.
func passingSampleFor(t *testing.T, ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig, engine storepb.Engine) CompiledQuery {
	t.Helper()
	spec, ok := ruleTypeSpecFor(ruleType)
	require.True(t, ok)
	require.NotNil(t, spec.Passing, "%s has no passing-row statement", ruleType)
	dialect, err := DialectFor(engine)
	require.NoError(t, err)
	query, err := spec.Passing(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(engine),
		Type:    ruleType,
		Config:  config,
	}, 20)
	require.NoError(t, err)
	return query
}

// TestCompilePassingRowsCoversEveryTypeWithOne pins that the passing statement is
// the failing predicate negated for the single-relation row checks: the two
// statements read the same relation with complementary WHERE clauses.
func TestCompilePassingRowsCoversEveryTypeWithOne(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	target := compileTestTarget(storepb.Engine_MYSQL)
	tests := []struct {
		name     string
		ruleType storepb.QualityRuleType
		config   *storepb.QualityRuleConfig
		failing  string
		passing  string
	}{
		{
			name:     "null_percent",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullCount: int64Ptr(0)},
			}},
			failing: "SELECT `id`, `email` FROM `shop`.`orders` WHERE (`email` IS NULL) LIMIT 20",
			passing: "SELECT `id`, `email` FROM `shop`.`orders` WHERE (NOT (`email` IS NULL)) LIMIT 20",
		},
		{
			name:     "range",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
				Range: &storepb.RangeConfig{Column: "amount", Min: validationPtr("0"), Max: validationPtr("100")},
			}},
			failing: "SELECT `id`, `amount` FROM `shop`.`orders` WHERE (`amount` <= ? OR `amount` >= ?) LIMIT 20",
			passing: "SELECT `id`, `amount` FROM `shop`.`orders` WHERE (NOT (`amount` <= ? OR `amount` >= ?)) LIMIT 20",
		},
		{
			name:     "accepted_values_field",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
				AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status", Field: "name"},
			}},
			failing: "SELECT `id`, `status` FROM `shop`.`orders` WHERE (`status` IS NULL OR `status` NOT IN " +
				"(SELECT `name` FROM `shop`.`orders` WHERE `name` IS NOT NULL)) LIMIT 20",
			passing: "SELECT `id`, `status` FROM `shop`.`orders` WHERE (NOT (`status` IS NULL OR `status` NOT IN " +
				"(SELECT `name` FROM `shop`.`orders` WHERE `name` IS NOT NULL))) LIMIT 20",
		},
		{
			name:     "cross_column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{
					LeftColumn:  "qty",
					RightColumn: "amount",
					Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE,
				},
			}},
			failing: "SELECT `id`, `qty`, `amount` FROM `shop`.`orders` WHERE (NOT (`qty` >= `amount`)) LIMIT 20",
			passing: "SELECT `id`, `qty`, `amount` FROM `shop`.`orders` WHERE (NOT (NOT (`qty` >= `amount`))) LIMIT 20",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			failing, err := CompileFailureRows(CompileInput{
				Dialect: dialect, Target: target, Type: test.ruleType, Config: test.config,
			}, 20)
			require.NoError(t, err)
			require.Equal(t, test.failing, failing.SQL)
			require.Equal(t, test.passing, passingSampleFor(t, test.ruleType, test.config, storepb.Engine_MYSQL).SQL)
		})
	}
}

// TestCompilePassingRowsReferentialUsesInnerJoin pins that a passing referential
// row is the row that found a parent: the LEFT JOIN flips to an INNER JOIN rather
// than negating a predicate the join produced.
func TestCompilePassingRowsReferentialUsesInnerJoin(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
		ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
			ChildColumn:  "id",
			ParentTable:  "shop.customers",
			ParentColumn: "id",
		},
	}}
	query := passingSampleFor(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, config, storepb.Engine_MYSQL)
	require.Contains(t, query.SQL, " INNER JOIN ")
	require.NotContains(t, query.SQL, "LEFT JOIN")
	require.Contains(t, query.SQL, "ON c.`id` = p.`id`")
}

// TestCompilePassingRowsSkipsMeasuredTypes pins that the types whose failure is a
// measurement or a value keep no passing rows.
func TestCompilePassingRowsSkipsMeasuredTypes(t *testing.T) {
	t.Parallel()

	for _, ruleType := range []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
	} {
		spec, ok := ruleTypeSpecFor(ruleType)
		require.True(t, ok)
		require.Nil(t, spec.Passing, "%s must not keep passing rows", ruleType)
	}
}

// TestCompilePassingRowsOnlyOnFailure pins the executor's decision: a statement is
// compiled only for a check that failed at least one row, so a healthy check never
// reserves a passing statement.
func TestCompilePassingRowsOnlyOnFailure(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
		Range: &storepb.RangeConfig{Column: "amount", Min: validationPtr("0")},
	}}
	input := CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		Config:  config,
	}
	passing := &Execution{Observation: Observation{FailedRows: 2}}
	require.NoError(t, compilePassingRows(input, passing, ExecOptions{SampleLimit: 5}))
	require.NotNil(t, passing.PassingRows)

	clean := &Execution{Observation: Observation{FailedRows: 0}}
	require.NoError(t, compilePassingRows(input, clean, ExecOptions{}))
	require.Nil(t, clean.PassingRows)
}

// TestAttachPassingRowsStoresAndRedacts pins the runner-side fetch: the rows land
// in the evidence, are bounded by the sample limit, and personal columns are
// redacted.
func TestAttachPassingRowsStoresAndRedacts(t *testing.T) {
	t.Parallel()

	rule := RuleSnapshot{
		Type:   storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{Column: "amount"}}},
	}
	execution := &Execution{
		Observation: Observation{FailedRows: 1},
		PassingRows: &CompiledQuery{SQL: "SELECT passing", Kind: QuerySample, Columns: []string{"email"}},
	}
	queryer := &execFakeQueryer{results: []*db.QueryResult{{
		Columns: []string{"email"},
		Rows:    [][]any{{"someone@example.com"}},
	}}}
	require.NoError(t, AttachPassingRows(context.Background(), queryer, execution, rule, ExecOptions{
		SampleLimit: 20,
		MaskColumns: []string{"email"},
	}))
	require.Len(t, queryer.queries, 1)
	passing := execution.Evidence.GetPassingSamples()
	require.Len(t, passing, 1)
	require.True(t, passing[0].GetColumns()[0].GetMasked())
	require.NotEqual(t, "someone@example.com", passing[0].GetColumns()[0].GetValue())

	// A failed query is not fatal: the verdict stands and the comparison is empty.
	queryer = &execFakeQueryer{errs: []error{errors.New("the sample query failed")}}
	require.NoError(t, AttachPassingRows(context.Background(), queryer, execution, rule, ExecOptions{}))

	// Dropping clears the passing rows but keeps the failed ones.
	execution.Evidence.Samples = passing
	DropPassingRows(execution)
	require.Empty(t, execution.Evidence.GetPassingSamples())
	require.Len(t, execution.Evidence.GetSamples(), 1)
}
