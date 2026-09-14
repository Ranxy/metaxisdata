package postgresql

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	omnipg "github.com/bytebase/omni/pg"
	pgast "github.com/bytebase/omni/pg/ast"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// parseTarget parses `SELECT <expr> AS x FROM t` and returns the target
// expression node plus the exact SQL (needed by exprText/Loc-based extraction).
func parseTarget(t *testing.T, expr string) (pgast.Node, string) {
	t.Helper()
	sql := "SELECT " + expr + " AS x FROM t"
	stmts, err := omnipg.Parse(sql)
	require.NoError(t, err, "parse %q", expr)
	require.Len(t, stmts, 1)
	sel, ok := stmts[0].AST.(*pgast.SelectStmt)
	require.True(t, ok, "expected a SelectStmt for %q", expr)
	rt, ok := sel.TargetList.Items[0].(*pgast.ResTarget)
	require.True(t, ok)
	return rt.Val, sql
}

// TestIsExpressionDerived pins the structural, explicit-allow-list predicate.
func TestIsExpressionDerived(t *testing.T) {
	notDerived := []string{
		"id",
		"'x'",
		"'CASE WHEN x'",
		"'a(b'",
		"'2020-01-01'",
		"CURRENT_DATE",
		"$1",
	}
	for _, expr := range notDerived {
		node, _ := parseTarget(t, expr)
		require.False(t, isExpressionDerived(node), "expected %q NOT derived", expr)
	}

	derived := []string{
		"a = b",
		"a + b",
		"data->>'k'",
		"a IS NULL",
		"created_at::date",
		"CAST(created_at AS date)",
		"COALESCE(a, b)",
		"GREATEST(a, b)",
		"CASE WHEN a > 1 THEN 1 ELSE 2 END",
		"lower(name)",
		"count(*)",
		"(SELECT count(*) FROM o)",
		"a[1]",
		"first_name || ' ' || last_name",
	}
	for _, expr := range derived {
		node, _ := parseTarget(t, expr)
		require.True(t, isExpressionDerived(node), "expected %q derived", expr)
	}
}

// TestClassifyExpression pins the structural classification, including the
// decisions from plan/postgresql_expression_transformation_plan.md.
func TestClassifyExpression(t *testing.T) {
	cases := []struct {
		expr        string
		operation   model.OperationType
		function    string
		opType      string
		arguments   []string
		partitionBy []string
		orderBy     []string
	}{
		// Operators emit the source token.
		{"a + b", model.OperationOperator, "", "+", nil, nil, nil},
		{"a - b", model.OperationOperator, "", "-", nil, nil, nil},
		{"a * b", model.OperationOperator, "", "*", nil, nil, nil},
		{"a / b", model.OperationOperator, "", "/", nil, nil, nil},
		{"-a", model.OperationOperator, "", "-", nil, nil, nil},
		{"data->>'k'", model.OperationOperator, "", "->>", nil, nil, nil},
		{"a = b", model.OperationOperator, "", "=", nil, nil, nil},
		{"a > b", model.OperationOperator, "", ">", nil, nil, nil},
		{"first_name || ' ' || last_name", model.OperationOperator, "", "||", nil, nil, nil},
		{"a IN (1, 2)", model.OperationOperator, "", "IN", nil, nil, nil},
		{"a NOT IN (1, 2)", model.OperationOperator, "", "NOT IN", nil, nil, nil},
		{"a BETWEEN 1 AND 2", model.OperationOperator, "", "BETWEEN", nil, nil, nil},
		{"a IS NULL", model.OperationOperator, "", "IS NULL", nil, nil, nil},
		{"a IS NOT NULL", model.OperationOperator, "", "IS NOT NULL", nil, nil, nil},
		{"a AND b", model.OperationOperator, "", "AND", nil, nil, nil},
		{"NOT a", model.OperationOperator, "", "NOT", nil, nil, nil},

		// Named FUNCTIONs.
		{"lower(name)", model.OperationFunction, "lower", "", []string{"name"}, nil, nil},
		{"pg_catalog.count(*)", model.OperationAggregate, "COUNT", "", nil, nil, nil},
		{"count(*)", model.OperationAggregate, "COUNT", "", nil, nil, nil},
		{"SUM(amount)", model.OperationAggregate, "SUM", "", nil, nil, nil},
		{"COALESCE(a, b)", model.OperationFunction, "COALESCE", "", []string{"a", "b"}, nil, nil},
		{"GREATEST(a, b)", model.OperationFunction, "GREATEST", "", []string{"a", "b"}, nil, nil},
		{"LEAST(a, b)", model.OperationFunction, "LEAST", "", []string{"a", "b"}, nil, nil},
		{"NULLIF(a, b)", model.OperationFunction, "NULLIF", "", []string{"a", "b"}, nil, nil},
		{"created_at::date", model.OperationFunction, "CAST", "", []string{"created_at"}, nil, nil},

		// A window clause wins over the aggregate name set.
		{"ROW_NUMBER() OVER (PARTITION BY a ORDER BY b)", model.OperationWindow, "ROW_NUMBER", "", nil, []string{"a"}, []string{"b"}},
		{"SUM(x) OVER (PARTITION BY a ORDER BY b)", model.OperationWindow, "SUM", "", nil, []string{"a"}, []string{"b"}},
		{"LEAD(age) OVER (ORDER BY id)", model.OperationWindow, "LEAD", "", nil, nil, []string{"id"}},

		// CASE and PROJECT fallbacks.
		{"CASE WHEN a > 1 THEN 1 ELSE 2 END", model.OperationCase, "", "", nil, nil, nil},
		{"(SELECT count(*) FROM o)", model.OperationProject, "", "", nil, nil, nil},
		{"a[1]", model.OperationProject, "", "", nil, nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			node, sql := parseTarget(t, tc.expr)
			analyzer := NewAnalyzer(t.Context(), sql, nil)
			transform, ok := analyzer.classifyExpression(node)
			require.True(t, ok)
			require.Equal(t, tc.operation, transform.Operation, "operation")
			require.Equal(t, tc.function, transform.FunctionName, "function")
			require.Equal(t, tc.opType, transform.OpType, "op type")
			require.Equal(t, sorted(tc.arguments), sorted(transform.Arguments), "arguments")
			require.Equal(t, sorted(tc.partitionBy), sorted(transform.PartitionBy), "partition by")
			require.Equal(t, sorted(tc.orderBy), sorted(transform.OrderBy), "order by")
		})
	}
}

// TestSourceLessFallback pins the wildcard fallback gate: function calls may
// depend on the whole relation, but a constant must not fabricate an edge.
func TestSourceLessFallback(t *testing.T) {
	relations, err := analyzeSQL("SELECT count(*) AS c FROM t", nil)
	require.NoError(t, err)
	require.True(t, hasResultEdge(relations, "t", wildcardColumn, "c"),
		"function call should keep the wildcard fallback")

	relations, err = analyzeSQL("SELECT 'x'::text AS s FROM t", nil)
	require.NoError(t, err)
	require.False(t, hasResultEdge(relations, "t", wildcardColumn, "s"),
		"a cast of a constant must not fabricate a table.* edge")
	require.Empty(t, relations)
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	slices.Sort(out)
	return out
}
