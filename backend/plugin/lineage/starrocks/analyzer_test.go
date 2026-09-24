package starrocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"

	nodes "github.com/bytebase/omni/starrocks/ast"
	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

func TestColumnRefFromObjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parts []string
		want  scope.ColumnRef
		ok    bool
	}{
		{parts: []string{"id"}, want: scope.ColumnRef{Column: "id"}, ok: true},
		{parts: []string{"t", "id"}, want: scope.ColumnRef{Table: "t", Column: "id"}, ok: true},
		{parts: []string{"db", "t", "id"}, want: scope.ColumnRef{Schema: "db", Table: "t", Column: "id"}, ok: true},
		// A catalog qualifier is dropped; the registry has no catalog dimension.
		{parts: []string{"cat", "db", "t", "id"}, want: scope.ColumnRef{Schema: "db", Table: "t", Column: "id"}, ok: true},
		{parts: nil, ok: false},
		{parts: []string{""}, ok: false},
	}
	for _, tt := range tests {
		got, ok := columnRefFromObjectName(&nodes.ObjectName{Parts: tt.parts})
		require.Equal(t, tt.ok, ok, "parts=%v", tt.parts)
		if tt.ok {
			require.Equal(t, tt.want, got, "parts=%v", tt.parts)
		}
	}

	_, ok := columnRefFromObjectName(nil)
	require.False(t, ok)
}

func TestTableRefFromObjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parts  []string
		schema string
		table  string
		ok     bool
	}{
		{parts: []string{"t"}, table: "t", ok: true},
		{parts: []string{"db", "t"}, schema: "db", table: "t", ok: true},
		// A catalog qualifier is dropped.
		{parts: []string{"cat", "db", "t"}, schema: "db", table: "t", ok: true},
		{parts: nil, ok: false},
		{parts: []string{""}, ok: false},
	}
	for _, tt := range tests {
		schema, table, ok := tableRefFromObjectName(&nodes.ObjectName{Parts: tt.parts})
		require.Equal(t, tt.ok, ok, "parts=%v", tt.parts)
		if tt.ok {
			require.Equal(t, tt.schema, schema, "parts=%v", tt.parts)
			require.Equal(t, tt.table, table, "parts=%v", tt.parts)
		}
	}
}

// TestExprTextReconstruction pins the whitespace-free rendering used by
// transformation metadata, which mirrors the other analyzers.
func TestExprTextReconstruction(t *testing.T) {
	t.Parallel()

	a := NewAnalyzer(context.TODO(), "SELECT a.id + 1 AS x FROM t", nil)
	file, errs := starrocksparser.Parse(a.sql)
	require.Empty(t, errs)
	stmt, ok := file.Stmts[0].(*nodes.SelectStmt)
	require.True(t, ok)
	require.Equal(t, "a.id+1", a.exprTextOf(stmt.Items[0].Expr))
}

// TestInferredColumnAlias pins the name an unaliased select item gets. Only a real
// column reference is shortened to its column name; an expression that merely
// contains a dot keeps its whole text.
func TestInferredColumnAlias(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ sql, want string }{
		{"SELECT users.id FROM users", "id"},
		{"SELECT `users`.`id` FROM users", "id"},
		{"SELECT id FROM users", "id"},
		{"SELECT COUNT(*) FROM users", "COUNT(*)"},
		{"SELECT t.a + 1 FROM t", "t.a+1"},
		{"SELECT CASE WHEN x > 0 THEN a ELSE t.b END FROM t", "CASEWHENx>0THENaELSEt.bEND"},
		{"SELECT t.x IS NULL FROM t", "t.xISNULL"},
		{"SELECT [t.a, t.b] FROM t", "[t.a,t.b]"},
	} {
		a := NewAnalyzer(context.TODO(), tc.sql, nil)
		file, errs := starrocksparser.Parse(tc.sql)
		require.Empty(t, errs)
		stmt, ok := file.Stmts[0].(*nodes.SelectStmt)
		require.True(t, ok)
		expr := stmt.Items[0].Expr
		require.Equal(t, tc.want, inferredColumnAlias(expr, a.exprTextOf(expr)), tc.sql)
	}
}

// TestUnmodelledStatementsAreGaps pins that a statement shape this phase does not
// handle yet is reported as a gap beside the edges, never as a wrong result and
// never as a silent empty one. Narrow this list as the corresponding phases land.
func TestUnmodelledStatementsAreGaps(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"MERGE INTO t1 USING t2 ON t1.id = t2.id WHEN MATCHED THEN UPDATE SET t1.name = t2.tag",
	} {
		relations, err := analyzeSQL(sql, nil)

		var unsupported *lineage.UnsupportedStatementError
		require.ErrorAs(t, err, &unsupported, "expected a reported gap for %q", sql)
		require.Contains(t, unsupported.Error(), "not modelled")
		require.Empty(t, relations, "expected no edges for %q", sql)
	}
}
