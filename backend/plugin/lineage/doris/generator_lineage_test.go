package doris

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// edgeText renders one relation as "source -> target", which is enough for the
// shapes here: every case names one source column and one generated or declared
// column, and the transformation is not what they pin.
func edgeText(relation model.ColumnRelation) string {
	return relation.Source.Table.FullName() + "." + relation.Source.Name +
		" -> " + relation.Target.Table.FullName() + "." + relation.Target.Name
}

// requireEdges asserts the exact edge set, order-insensitive, for an analyzer
// with no catalog.
func requireEdges(t *testing.T, sql string, want []string) {
	t.Helper()

	requireEdgesWithCatalog(t, sql, nil, want)
}

// requireEdgesWithCatalog is requireEdges for a statement whose reading depends on
// catalog metadata.
func requireEdgesWithCatalog(t *testing.T, sql string, cat catalog.Provide, want []string) {
	t.Helper()

	relations, err := analyzeSQL(sql, cat)
	require.NoError(t, err)
	got := make([]string, 0, len(relations))
	for _, relation := range relations {
		got = append(got, edgeText(relation))
	}
	if want == nil {
		want = []string{}
	}
	slices.Sort(got)
	slices.Sort(want)
	require.Equal(t, want, got, "sql=%q", sql)
}

// TestLateralViewGeneratorLineage pins Doris's LATERAL VIEW, which StarRocks has
// no equivalent of. A generator builds its rows from its arguments and its AS list
// names the columns it produces, so every declared column traces back to every
// column the generator reads — and the relation it declares answers a reference to
// it instead of reporting an unknown name.
func TestLateralViewGeneratorLineage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "one declared column",
			sql:  "SELECT u.elem FROM t LATERAL VIEW explode(t.arr) u AS elem",
			want: []string{"t.arr -> __result__.elem"},
		},
		{
			name: "several declared columns share the same argument",
			sql:  "SELECT u.c1, u.c2 FROM t LATERAL VIEW explode(t.arr) u AS c1, c2",
			want: []string{"t.arr -> __result__.c1", "t.arr -> __result__.c2"},
		},
		{
			name: "a wildcard over the base relation keeps its own edge",
			sql:  "SELECT * FROM t LATERAL VIEW explode(t.arr) u AS c1, c2",
			want: []string{"t.* -> __result__.*", "t.arr -> __result__.c1", "t.arr -> __result__.c2"},
		},
		{
			name: "a chain of generators contributes one relation each",
			sql:  "SELECT * FROM t LATERAL VIEW explode(t.arr) u AS elem LATERAL VIEW explode(t.arr) v AS elem2",
			want: []string{"t.* -> __result__.*", "t.arr -> __result__.elem", "t.arr -> __result__.elem2"},
		},
		{
			// The generators are applied left to right, so the second one reads a
			// column the first declared — and the trace flattens through that
			// relation back to the stored column.
			name: "a later generator reads the column an earlier one declared",
			sql:  "SELECT v.elem2 FROM t LATERAL VIEW explode(t.arr) u AS elem LATERAL VIEW explode(u.elem) v AS elem2",
			want: []string{"t.arr -> __result__.elem2"},
		},
		{
			// The generator's argument is an expression subquery, which omni keeps as
			// raw text: the columns it reads are the sources of the columns the
			// generator declares, not a dropped branch.
			name: "an expression-subquery argument reads what the subquery reads",
			sql:  "SELECT * FROM t LATERAL VIEW explode((SELECT arr FROM t)) u AS elem",
			want: []string{"t.* -> __result__.*", "t.arr -> __result__.elem"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireEdges(t, tc.sql, slices.Clone(tc.want))
		})
	}
}

// TestTableValuedFunctionLineage pins the second Doris shape a generator takes: a
// table-valued function in FROM. Doris gives it no column list, so it exposes the
// column its own name gives it, and a call over a column sources that column —
// which is what makes `FROM t, explode(t.arr) u` trace `t.arr`.
//
// A literal-only call reads nothing this analysis can name, so it contributes no
// edge and no gap: `numbers(...)` is a table the statement built, not a stored
// relation.
func TestTableValuedFunctionLineage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "a call over a column sources it",
			sql:  "SELECT * FROM t, explode(t.arr) u",
			want: []string{"t.* -> __result__.*", "t.arr -> __result__.explode"},
		},
		{
			// A literal-only call has no nameable source; a call over a query does,
			// and that query's sources are collected the way a select item's are.
			name: "a call over an expression subquery sources what it reads",
			sql:  "SELECT * FROM explode((SELECT arr FROM t)) u",
			want: []string{"t.arr -> __result__.explode"},
		},
		{
			name: "the function's own name addresses the column it exposes",
			sql:  "SELECT u.explode FROM t, explode(t.arr) u",
			want: []string{"t.arr -> __result__.explode"},
		},
		{
			name: "a literal-only call has no lineage",
			sql:  `SELECT * FROM numbers("number" = "10")`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireEdges(t, tc.sql, slices.Clone(tc.want))
		})
	}
}

// TestCTASTargetColumnsAreTheDeclaredNames pins the field this dialect differs on:
// Doris has no name-only CTAS column list, so `CREATE TABLE t (a INT) AS SELECT …`
// carries the target names in its typed column definitions, and they must win over
// the query's own output aliases.
func TestCTASTargetColumnsAreTheDeclaredNames(t *testing.T) {
	t.Parallel()

	requireEdges(t,
		"CREATE TABLE t3 (k INT, n VARCHAR(50)) AS SELECT id, name FROM users",
		[]string{"users.id -> t3.k", "users.name -> t3.n"})

	// With no declaration the query's own aliases name the columns.
	requireEdges(t,
		"CREATE TABLE t3 AS SELECT id, name AS full_name FROM users",
		[]string{"users.id -> t3.id", "users.name -> t3.full_name"})
}
