package doris

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRewriteLateralOverDerivedTable pins the rewrite and, just as important, the
// statements it refuses. Hoisting a name to the front of the statement gives it the
// rank of a CTE, which outranks a base table of the same name, so a statement that also
// holds a relation by that name has to keep its hard failure.
func TestRewriteLateralOverDerivedTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "a derived table with a bare alias",
			sql:  "SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag",
			want: "WITH d AS (SELECT v FROM ev) SELECT * FROM d LATERAL VIEW explode(d.v) u AS tag",
		},
		{
			name: "a derived table with AS",
			sql:  "SELECT * FROM (SELECT v FROM ev) AS d LATERAL VIEW explode(d.v) u AS tag",
			want: "WITH d AS (SELECT v FROM ev) SELECT * FROM d LATERAL VIEW explode(d.v) u AS tag",
		},
		{
			name: "a statement that already has a WITH clause",
			sql:  "WITH x AS (SELECT 1 AS k) SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag",
			want: "WITH x AS (SELECT 1 AS k) , d AS (SELECT v FROM ev) SELECT * FROM d LATERAL VIEW explode(d.v) u AS tag",
		},
		{
			name: "the name is also another relation",
			sql:  "SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag, ev3 AS d",
			want: "",
		},
		{
			name: "no LATERAL VIEW at all",
			sql:  "SELECT * FROM (SELECT v FROM ev) d",
			want: "",
		},
		{
			name: "a LATERAL VIEW on a plain table",
			sql:  "SELECT * FROM ev LATERAL VIEW explode(ev.v) u AS tag",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := rewriteLateralOverDerivedTable(tc.sql)
			require.Equal(t, tc.want != "", ok, "sql=%q", tc.sql)
			require.Equal(t, tc.want, got, "sql=%q", tc.sql)
		})
	}
}

// TestALateralViewOverADerivedTableKeepsItsLineage pins what the rewrite recovers. omni
// stops at LATERAL, while the engine runs the statement on 4.1.4 and returns the same
// rows, under the same column names, as the CTE form the rewrite produces; without it
// the whole statement lost its lineage to a hard failure. The view case reaches the
// rewrite through the view-body fallback, since the definition itself does not parse.
//
// A statement whose name cannot be hoisted safely keeps its hard failure.
func TestALateralViewOverADerivedTableKeepsItsLineage(t *testing.T) {
	t.Parallel()

	requireEdgesWithCatalog(t, "SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag",
		variantCatalog(), []string{"ev.v -> __result__.v", "ev.v -> __result__.tag"})
	requireEdgesWithCatalog(t, "CREATE VIEW vw AS SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag",
		variantCatalog(), []string{"ev.v -> vw.v", "ev.v -> vw.tag"})

	relations, err := analyzeSQL(
		"SELECT * FROM (SELECT v FROM ev) d LATERAL VIEW explode(d.v) u AS tag, ev3 AS d", variantCatalog())
	require.ErrorContains(t, err, "failed to parse Doris SQL")
	require.Empty(t, relations)
}
