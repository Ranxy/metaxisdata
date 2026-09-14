package starrocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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

func TestInferColumnAlias(t *testing.T) {
	t.Parallel()

	require.Equal(t, "id", inferColumnAlias("users.id"))
	require.Equal(t, "id", inferColumnAlias("`users`.`id`"))
	require.Equal(t, "id", inferColumnAlias("id"))
	require.Equal(t, "COUNT(*)", inferColumnAlias("COUNT(*)"))
}

// TestUnimplementedStatementsFailLoudly pins that a shape this phase does not
// handle yet is an explicit error, never a wrong or partial lineage. Narrow
// this list as the corresponding phases land.
func TestUnimplementedStatementsFailLoudly(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE VIEW v AS SELECT id FROM t",
		"WITH c AS (SELECT id FROM t) SELECT id FROM c",
		"SELECT * FROM (SELECT id FROM t) x",
		"SELECT id FROM t1 UNION ALL SELECT id FROM t2",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.Error(t, err, "expected an explicit failure for %q", sql)
		require.Nil(t, relations, "expected no partial result for %q", sql)
	}
}
