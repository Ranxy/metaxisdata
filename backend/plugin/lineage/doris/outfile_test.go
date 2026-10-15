package doris

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStripIntoOutfile pins both halves of the shim: the clause it recognizes and the
// statements it must leave alone. Only `INTO OUTFILE <string>` is a result sink;
// `INSERT INTO` and `COPY INTO` name a relation after the keyword, and an INTO inside
// a parenthesized query is not the statement's own clause.
func TestStripIntoOutfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "the clause at the end of a select",
			sql:  "SELECT k FROM t INTO OUTFILE 'file:///tmp/x.csv'",
			want: "SELECT k FROM t",
		},
		{
			name: "the clause with its FORMAT AS",
			sql:  "SELECT k FROM t INTO OUTFILE 'file:///tmp/x.csv' FORMAT AS CSV",
			want: "SELECT k FROM t",
		},
		{
			name: "the clause after a WITH clause",
			sql:  "WITH c AS (SELECT k FROM t) SELECT k FROM c INTO OUTFILE 'file:///tmp/x.csv'",
			want: "WITH c AS (SELECT k FROM t) SELECT k FROM c",
		},
		{name: "INSERT INTO names a relation", sql: "INSERT INTO t SELECT k FROM t", want: ""},
		{name: "COPY INTO names a relation", sql: "COPY INTO t FROM 's3://bucket/x.csv'", want: ""},
		{name: "the clause needs its literal", sql: "SELECT k FROM t INTO OUTFILE", want: ""},
		{name: "an INTO inside a subquery is not the statement's", sql: "SELECT * FROM (SELECT k FROM t INTO OUTFILE 'x') d", want: ""},
		{name: "no clause at all", sql: "SELECT k FROM t", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := stripIntoOutfile(tc.sql)
			require.Equal(t, tc.want != "", ok, "sql=%q", tc.sql)
			require.Equal(t, tc.want, got, "sql=%q", tc.sql)
		})
	}
}

// TestAnIntoOutfileStatementKeepsItsProjection pins the recovery. The engine accepts
// the clause — on 4.1.4 the statement fails later, on the export policy
// (`Exporting results to local disk is not allowed`), not on the syntax — and the sink
// carries no column lineage, so the projection's edges are the whole lineage of the
// statement. That is the answer the StarRocks analyzer gives for the same statement,
// whose AST carries IntoOutfileClause and whose traversal ignores it.
//
// A statement that still does not parse keeps its hard failure, and an INSERT INTO is
// never read as a sink.
func TestAnIntoOutfileStatementKeepsItsProjection(t *testing.T) {
	t.Parallel()

	const sink = " INTO OUTFILE 'file:///tmp/export.csv'"
	requireEdges(t, "SELECT k AS n FROM ev2"+sink, []string{"ev2.k -> __result__.n"})
	requireEdges(t, "SELECT k AS n FROM ev2"+sink+" FORMAT AS CSV", []string{"ev2.k -> __result__.n"})
	requireEdges(t, "WITH c AS (SELECT k FROM ev2) SELECT k AS n FROM c"+sink, []string{"ev2.k -> __result__.n"})

	for _, sql := range []string{"SELECT k FROM ev2 INTO OUTFILE", "SELECT FROM ev2"} {
		relations, err := analyzeSQL(sql, nil)
		require.ErrorContains(t, err, "failed to parse Doris SQL", "sql=%q", sql)
		require.Empty(t, relations)
	}

	requireEdges(t, "INSERT INTO ev2 (name) SELECT k FROM ev2", []string{"ev2.k -> ev2.name"})
}
