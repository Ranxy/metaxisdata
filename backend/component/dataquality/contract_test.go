package dataquality

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// contractClause builds one stored filter clause for the contract tests.
func contractClause(column string, op storepb.QualityFilterOperator, values ...string) *storepb.QualityFilterClause {
	return &storepb.QualityFilterClause{Column: column, Op: op, Values: values}
}

// contractFilter builds a stored filter from clauses.
func contractFilter(clauses ...*storepb.QualityFilterClause) *storepb.QualityFilter {
	return &storepb.QualityFilter{Clauses: clauses}
}

// contractRowCountConfig is the simplest rule config: no config argument competes
// with the filter's, so argument order is entirely the two filters' order.
func contractRowCountConfig() *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
	}}
}

// TestCompileMergesTheContractFilterAfterTheRuleFilter pins the contract filter's
// contract: its clauses follow the rule's own, and both share one placeholder
// sequence (numbered for PostgreSQL, positional for MySQL).
func TestCompileMergesTheContractFilterAfterTheRuleFilter(t *testing.T) {
	t.Parallel()

	ruleFilter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	contract := Filter{Clauses: []FilterClause{
		{Column: "amount", Op: FilterGTE, Values: []string{"10"}},
		{Column: "email", Op: FilterISNotNull},
	}}
	tests := []struct {
		name     string
		engine   storepb.Engine
		wantSQL  string
		wantArgs []any
	}{
		{
			name:    "mysql",
			engine:  storepb.Engine_MYSQL,
			wantSQL: "SELECT COUNT(*) AS total_rows FROM `shop`.`orders` WHERE `status` = ? AND `amount` >= ? AND `email` IS NOT NULL",
			// `email` IS NOT NULL binds no value; the argument list stays positional.
			wantArgs: []any{"paid", int64(10)},
		},
		{
			name:     "postgres",
			engine:   storepb.Engine_POSTGRES,
			wantSQL:  `SELECT COUNT(*) AS total_rows FROM "public"."orders" WHERE "status" = $1 AND "amount" >= $2 AND "email" IS NOT NULL`,
			wantArgs: []any{"paid", int64(10)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query, err := Compile(CompileInput{
				Dialect:        mustDialect(t, test.engine),
				Target:         compileTestTarget(test.engine),
				Type:           storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
				Config:         contractRowCountConfig(),
				Filter:         ruleFilter,
				ContractFilter: contract,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Equal(t, test.wantArgs, query.Args)
		})
	}
}

// TestCompileWithoutAContractFilterIsUnchanged pins the no-op case: an absent or
// empty contract filter leaves the statement exactly what it was before contracts
// existed.
func TestCompileWithoutAContractFilterIsUnchanged(t *testing.T) {
	t.Parallel()

	ruleFilter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	base := func(contract Filter) CompiledQuery {
		query, err := Compile(CompileInput{
			Dialect:        mustDialect(t, storepb.Engine_MYSQL),
			Target:         compileTestTarget(storepb.Engine_MYSQL),
			Type:           storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Config:         contractRowCountConfig(),
			Filter:         ruleFilter,
			ContractFilter: contract,
		})
		require.NoError(t, err)
		return query
	}

	without := base(Filter{})
	emptyContract := base(Filter{Clauses: []FilterClause{}})
	require.Equal(t, without.SQL, emptyContract.SQL)
	require.Equal(t, without.Args, emptyContract.Args)
	require.Equal(t, "SELECT COUNT(*) AS total_rows FROM `shop`.`orders` WHERE `status` = ?", without.SQL)
}

// TestCompileRejectsAContractFilterOnAMissingColumn keeps the contract filter
// under the same column check as the rule's own: a contract naming a column the
// target does not have is an Invalid rule, not a SQL failure at execution.
func TestCompileRejectsAContractFilterOnAMissingColumn(t *testing.T) {
	t.Parallel()

	_, err := Compile(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config:  contractRowCountConfig(),
		ContractFilter: Filter{Clauses: []FilterClause{
			{Column: "missing", Op: FilterEQ, Values: []string{"x"}},
		}},
	})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), `"missing"`)
}

// TestMergeFilters pins the combination rules directly, including the two-budget
// case: each filter may fill MaxFilterClauses, so the merged predicate may carry
// twice as many clauses.
func TestMergeFilters(t *testing.T) {
	t.Parallel()

	ruleFilter := Filter{Clauses: []FilterClause{{Column: "status", Op: FilterEQ, Values: []string{"paid"}}}}
	contract := Filter{Clauses: []FilterClause{{Column: "amount", Op: FilterGTE, Values: []string{"10"}}}}

	require.Empty(t, mergeFilters(Filter{}, Filter{}).Clauses)
	require.Equal(t, ruleFilter, mergeFilters(ruleFilter, Filter{}))
	require.Equal(t, contract, mergeFilters(Filter{}, contract))
	require.Equal(t, []FilterClause{ruleFilter.Clauses[0], contract.Clauses[0]},
		mergeFilters(ruleFilter, contract).Clauses)

	tenClauses := func(column string) Filter {
		clauses := make([]FilterClause, 0, MaxFilterClauses)
		for range MaxFilterClauses {
			clauses = append(clauses, FilterClause{Column: column, Op: FilterISNotNull})
		}
		return Filter{Clauses: clauses}
	}
	merged := mergeFilters(tenClauses("status"), tenClauses("email"))
	require.Len(t, merged.Clauses, 2*MaxFilterClauses)
}

// TestCompileRendersTwoFilterBudgets runs the merged predicate through the
// compiler: a rule filter and a contract filter that each fill MaxFilterClauses
// render together, with every clause bound in order.
func TestCompileRendersTwoFilterBudgets(t *testing.T) {
	t.Parallel()

	fill := func(column string) Filter {
		clauses := make([]FilterClause, 0, MaxFilterClauses)
		for range MaxFilterClauses {
			clauses = append(clauses, FilterClause{Column: column, Op: FilterISNotNull})
		}
		return Filter{Clauses: clauses}
	}
	query, err := Compile(CompileInput{
		Dialect:        mustDialect(t, storepb.Engine_MYSQL),
		Target:         compileTestTarget(storepb.Engine_MYSQL),
		Type:           storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config:         contractRowCountConfig(),
		Filter:         fill("status"),
		ContractFilter: fill("email"),
	})
	require.NoError(t, err)
	require.Equal(t, 2*MaxFilterClauses-1, strings.Count(query.SQL, " AND "))
	require.Empty(t, query.Args, "IS NOT NULL binds no values")
}

// TestCompileCelSampleCarriesTheContractFilter pins that the CEL path, which is a
// sample query rather than an aggregate, sees the contract filter too.
func TestCompileCelSampleCarriesTheContractFilter(t *testing.T) {
	t.Parallel()

	query, err := Compile(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  celTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		Config:  celRuleConfig("amount > 100.0", []string{"amount"}, 100),
		Filter:  Filter{Clauses: []FilterClause{{Column: "status", Op: FilterEQ, Values: []string{"paid"}}}},
		ContractFilter: Filter{Clauses: []FilterClause{
			{Column: "active", Op: FilterEQ, Values: []string{"true"}},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT `amount` FROM `shop`.`orders` WHERE `status` = ? AND `active` = ? LIMIT 100", query.SQL)
	require.Equal(t, []any{"paid", true}, query.Args)
}

// TestCompileFailureRowsCarriesTheContractFilter pins the failed-row sample: the
// evidence query must select the same rows the aggregate counted, so the contract
// filter is part of it as well.
func TestCompileFailureRowsCarriesTheContractFilter(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "id", MaxNullPct: proto.Float64(5)},
	}}
	query, err := CompileFailureRows(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  config,
		Filter:  Filter{Clauses: []FilterClause{{Column: "status", Op: FilterEQ, Values: []string{"paid"}}}},
		ContractFilter: Filter{Clauses: []FilterClause{
			{Column: "amount", Op: FilterGT, Values: []string{"0"}},
		}},
	}, 20)
	require.NoError(t, err)
	require.Equal(t, "SELECT `id` FROM `shop`.`orders` WHERE `status` = ? AND `amount` > ? AND (`id` IS NULL) LIMIT 20", query.SQL)
	require.Equal(t, []any{"paid", int64(0)}, query.Args)
}

// TestValidateTargetFilterAcceptsAnEmptyFilterWithoutAStore pins the short-circuit:
// an empty contract filter adds nothing, so validating it must not touch the
// registry (a nil store would otherwise be a server fault).
func TestValidateTargetFilterAcceptsAnEmptyFilterWithoutAStore(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateTargetFilter(t.Context(), nil, "inst;app;orders", storepb.MetaType_TABLE, nil))
	require.NoError(t, ValidateTargetFilter(t.Context(), nil, "inst;app;orders", storepb.MetaType_TABLE, contractFilter()))
}

// TestValidateTargetFilterResolvesANonEmptyFilter pins that a contract filter with
// clauses does need the registry: without a store the resolver refuses, so an
// unvalidated contract cannot slip through as valid.
func TestValidateTargetFilterResolvesANonEmptyFilter(t *testing.T) {
	t.Parallel()

	err := ValidateTargetFilter(t.Context(), nil, "inst;app;orders", storepb.MetaType_TABLE,
		contractFilter(contractClause("status", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, "paid")))
	require.Error(t, err)
	require.Equal(t, common.Internal, common.ErrorCode(err))
}
