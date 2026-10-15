package doris

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRewriteWithBeforeDML pins where the clause goes and what is refused. The engine runs
// all three statements; omni reads the clause only inside the query an INSERT writes, so an
// INSERT's clause moves in front of that query's SELECT or VALUES, and an UPDATE's or
// DELETE's clause goes only when the statement reads none of its names.
func TestRewriteWithBeforeDML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "an insert takes the clause in front of its query",
			sql:  "WITH c AS (SELECT 1 AS a) INSERT INTO t (a) SELECT a FROM c",
			want: "INSERT INTO t (a) WITH c AS (SELECT 1 AS a) SELECT a FROM c",
		},
		{
			name: "a clause of several CTEs moves whole",
			sql:  "WITH c AS (SELECT 1 AS a), d AS (SELECT a FROM c) INSERT INTO t (a) SELECT a FROM d",
			want: "INSERT INTO t (a) WITH c AS (SELECT 1 AS a), d AS (SELECT a FROM c) SELECT a FROM d",
		},
		{
			name: "an overwrite takes it too",
			sql:  "WITH c AS (SELECT 1 AS a) INSERT OVERWRITE TABLE t (a) SELECT a FROM c",
			want: "INSERT OVERWRITE TABLE t (a) WITH c AS (SELECT 1 AS a) SELECT a FROM c",
		},
		{
			name: "a partition clause keeps the clause's own place",
			sql:  "WITH c AS (SELECT 9 AS x) INSERT INTO t PARTITION (p1) (a) SELECT x FROM c",
			want: "INSERT INTO t PARTITION (p1) (a) WITH c AS (SELECT 9 AS x) SELECT x FROM c",
		},
		{
			// omni's grammar takes a WITH only in front of a SELECT, so a VALUES insert's
			// clause is never moved — only dropped, like an UPDATE's or a DELETE's.
			name: "an unused clause before a VALUES insert goes",
			sql:  "WITH c AS (SELECT 1 AS x) INSERT INTO t (a) VALUES (1)",
			want: "INSERT INTO t (a) VALUES (1)",
		},
		{
			name: "a VALUES row that reads the clause stays",
			sql:  "WITH c AS (SELECT 1 AS x) INSERT INTO t (a) VALUES ((SELECT x FROM c))",
			want: "",
		},
		{
			// The engine runs it — `WITH `type` AS (…) UPDATE t SET b = (SELECT a FROM type)`
			// reads the CTE — so dropping it here would answer with an edge from a table the
			// statement never names. A bare reference to a keyword-spelled CTE lexes as that
			// keyword, which is why every token counts as a read.
			name: "a clause a statement reads by a keyword spelling stays",
			sql:  "WITH `type` AS (SELECT 1 AS a) UPDATE t SET b = (SELECT a FROM type)",
			want: "",
		},
		{
			name: "a clause named with a keyword the statement does not read goes",
			sql:  "WITH `type` AS (SELECT 1 AS a) UPDATE t SET b = 1",
			want: "UPDATE t SET b = 1",
		},
		{
			// `insert(` is the string function, and omni lexes it with the statement
			// keyword's token: only the depth-0 scan keeps it out of the statement search.
			name: "a string function named like a statement keyword",
			sql:  "WITH c AS (SELECT insert('ab', 2, 0, 'xy') AS y) UPDATE t SET a = 'aa'",
			want: "UPDATE t SET a = 'aa'",
		},
		{
			name: "a recursive clause the statement does not read goes",
			sql:  "WITH RECURSIVE c AS (SELECT 1 AS a) DELETE FROM t WHERE a = 1",
			want: "DELETE FROM t WHERE a = 1",
		},
		{
			// The engine runs `INSERT INTO t (a) WITH LABEL l WITH c AS (…) SELECT …`; omni
			// rejects it, so there is no place for the clause and the statement keeps its
			// parse error.
			name: "a label clause is left to the parser",
			sql:  "WITH c AS (SELECT 1 AS x) INSERT INTO t (a) WITH LABEL l SELECT x FROM c",
			want: "",
		},
		{
			name: "an unused clause before an update goes",
			sql:  "WITH c AS (SELECT 1 AS a) UPDATE t SET a = 1",
			want: "UPDATE t SET a = 1",
		},
		{
			name: "an unused clause before a delete goes",
			sql:  "WITH c AS (SELECT 1 AS a) DELETE FROM t WHERE a = 1",
			want: "DELETE FROM t WHERE a = 1",
		},
		{
			name: "a clause a delete reads stays",
			sql:  "WITH c AS (SELECT 1 AS a) DELETE FROM t WHERE a = (SELECT a FROM c)",
			want: "",
		},
		{
			name: "a clause an update reads stays",
			sql:  "WITH c AS (SELECT 1 AS a) UPDATE t SET a = (SELECT a FROM c)",
			want: "",
		},
		{name: "a query that already parses is left alone", sql: "WITH c AS (SELECT 1 AS a) SELECT a FROM c", want: ""},
		{name: "an insert without a clause is left alone", sql: "INSERT INTO t (a) SELECT a FROM t", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := rewriteWithBeforeDML(tc.sql)
			require.Equal(t, tc.want != "", ok, "sql=%q", tc.sql)
			require.Equal(t, tc.want, got, "sql=%q", tc.sql)
		})
	}
}

// TestAWithBeforeDMLKeepsItsLineage pins the recovery. On 4.1.4 the engine runs all three
// statements — the UPDATE updates the row, the INSERT inserts, the DELETE deletes, each
// read back — while omni stops at the statement keyword, which used to cost the whole
// statement its lineage.
//
// A clause the UPDATE or DELETE really reads cannot be placed anywhere, so that statement
// keeps its hard failure: that is the gap this recovery does not close, and it is pinned
// here so it stays visible.
func TestAWithBeforeDMLKeepsItsLineage(t *testing.T) {
	t.Parallel()

	requireEdgesWithCatalog(t, "WITH c AS (SELECT k FROM ev2) INSERT INTO t2 (k) SELECT k FROM c",
		variantCatalog(), []string{"ev2.k -> t2.k"})
	requireEdgesWithCatalog(t, "WITH c AS (SELECT 1 AS x) UPDATE t2 SET name = 'a'",
		variantCatalog(), []string{"t2.* -> t2.name"})
	requireEdgesWithCatalog(t, "WITH c AS (SELECT 1 AS x) DELETE FROM t2 WHERE k = 1",
		variantCatalog(), []string{"t2.k -> t2.__deletion__"})
	requireEdgesWithCatalog(t, "WITH c AS (SELECT k FROM ev2) INSERT INTO t2 PARTITION (p1) (k) SELECT k FROM c",
		variantCatalog(), []string{"ev2.k -> t2.k"})

	// The two recoveries compose: the clause moves first, then the mixed-spelling path is
	// rewritten inside the query it moved in front of.
	requireEdgesWithCatalog(t, "WITH c AS (SELECT v FROM ev) INSERT INTO t2 (k) SELECT v['a'].b FROM c",
		variantCatalog(), []string{"ev.v -> t2.k"})

	// A clause the statement reads through a name that lexes as a keyword keeps the hard
	// failure too: dropping it would leave an edge from a table the statement never named.
	relations, err := analyzeSQL("WITH c AS (SELECT max(k) AS m FROM ev2) UPDATE t2 SET name = 'a' WHERE k = (SELECT m FROM c)", variantCatalog())
	require.ErrorContains(t, err, "failed to parse Doris SQL")
	require.Empty(t, relations)
	relations, err = analyzeSQL("WITH `type` AS (SELECT k FROM ev2) UPDATE u2 SET b = (SELECT k FROM type) WHERE k = 1", variantCatalog())
	require.ErrorContains(t, err, "failed to parse Doris SQL")
	require.Empty(t, relations)
}
