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
			name: "a VALUES insert takes it too",
			sql:  "WITH c AS (SELECT 1 AS a) INSERT INTO t (a) VALUES (1)",
			want: "INSERT INTO t (a) WITH c AS (SELECT 1 AS a) VALUES (1)",
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
		{
			name: "a partition clause is not guessed at",
			sql:  "WITH c AS (SELECT 1 AS a) INSERT INTO t PARTITION (p1) (a) SELECT a FROM c",
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

	// The two recoveries compose: the clause moves first, then the mixed-spelling path is
	// rewritten inside the query it moved in front of.
	requireEdgesWithCatalog(t, "WITH c AS (SELECT v FROM ev) INSERT INTO t2 (k) SELECT v['a'].b FROM c",
		variantCatalog(), []string{"ev.v -> t2.k"})

	relations, err := analyzeSQL("WITH c AS (SELECT max(k) AS m FROM ev2) UPDATE t2 SET name = 'a' WHERE k = (SELECT m FROM c)", variantCatalog())
	require.ErrorContains(t, err, "failed to parse Doris SQL")
	require.Empty(t, relations)
}
