package sqlsanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// canonicalOf sanitizes and returns the fingerprint's input, which is where the foldings apply.
func canonicalOf(t *testing.T, sql string) string {
	t.Helper()
	got, err := Sanitize(storepb.Engine_MYSQL, sql)
	require.NoError(t, err)
	return got.Canonical
}

// TestFoldingMergesOneStatementWrittenDifferently is the point of the three rules: an application
// that writes the same query with a different IN list, a different batch size or a differently
// spelled constant ran one statement, and must not become one row per shape.
func TestFoldingMergesOneStatementWrittenDifferently(t *testing.T) {
	t.Parallel()

	groups := []struct {
		name string
		sqls []string
		want string
	}{
		{
			name: "an IN list of any length",
			sqls: []string{
				"SELECT a FROM t WHERE x IN (1)",
				"SELECT a FROM t WHERE x IN (1, 2)",
				"SELECT a FROM t WHERE x IN (1, 2, 3, 4, 5)",
			},
			want: "SELECT a FROM t WHERE x IN ( ? )",
		},
		{
			name: "a NOT IN list of any length",
			sqls: []string{
				"SELECT a FROM t WHERE x NOT IN (1)",
				"SELECT a FROM t WHERE x NOT IN (1, 2, 3)",
			},
			want: "SELECT a FROM t WHERE x NOT IN ( ? )",
		},
		{
			name: "a batch insert of any size",
			sqls: []string{
				"INSERT INTO t (a, b) VALUES (1, 2)",
				"INSERT INTO t (a, b) VALUES (1, 2), (3, 4)",
				"INSERT INTO t (a, b) VALUES (1, 2), (3, 4), (5, 6)",
			},
			want: "INSERT INTO t ( a , b ) VALUES ( ? , ? )",
		},
		{
			name: "a constant written differently",
			sqls: []string{
				"SELECT a FROM t WHERE x = 2",
				"SELECT a FROM t WHERE x = 1 + 1",
				"SELECT a FROM t WHERE x = 5 - 3",
			},
			want: "SELECT a FROM t WHERE x = ?",
		},
	}

	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			t.Parallel()

			fingerprints := make(map[string]string, len(group.sqls))
			for _, sql := range group.sqls {
				got, err := Sanitize(storepb.Engine_MYSQL, sql)
				require.NoError(t, err)
				require.Equal(t, group.want, got.Canonical, sql)
				fingerprints[sql] = got.Fingerprint
			}
			for sql, fingerprint := range fingerprints {
				require.Equal(t, fingerprints[group.sqls[0]], fingerprint,
					"%q must share a fingerprint with %q", sql, group.sqls[0])
			}
		})
	}
}

// TestFoldingKeepsDifferentStatementsApart is the other half of the contract, and the more
// important one: over-merging corrupts two statements at once, so every rule has to stay inside
// the shape it is meant to fold.
func TestFoldingKeepsDifferentStatementsApart(t *testing.T) {
	t.Parallel()

	groups := []struct {
		name string
		sqls []string
	}{
		{
			// A keyword ends a run, so two comparisons stay two comparisons.
			name: "two comparisons against one",
			sqls: []string{
				"SELECT a FROM t WHERE a = 1",
				"SELECT a FROM t WHERE a = 1 AND b = 2",
				"SELECT a FROM t WHERE a = 1 OR b = 2",
			},
		},
		{
			// A comma ends a run, so a call of two arguments is not a call of one.
			name: "different call shapes",
			sqls: []string{
				"SELECT a FROM t WHERE f(1) = 3",
				"SELECT a FROM t WHERE f(1, 2) = 3",
			},
		},
		{
			// NULL is a keyword, and it says something a placeholder does not.
			name: "NULL against a value",
			sqls: []string{
				"SELECT a FROM t WHERE x = NULL",
				"SELECT a FROM t WHERE x = 1",
				"SELECT a FROM t WHERE x IS NULL",
			},
		},
		{
			// Only an all-placeholder list folds: a column list is a different statement per
			// length, because it names different columns.
			name: "an IN list of columns",
			sqls: []string{
				"SELECT a FROM t WHERE x IN (a)",
				"SELECT a FROM t WHERE x IN (a, b)",
				"SELECT a FROM t WHERE x IN (a, b, c)",
			},
		},
		{
			// A mixed list is left alone.
			name: "a mixed IN list",
			sqls: []string{
				"SELECT a FROM t WHERE x IN (1, b)",
				"SELECT a FROM t WHERE x IN (1, 2, b)",
			},
		},
		{
			// The first group keeps its arity, so a different column count stays distinct.
			name: "batch inserts of different arity",
			sqls: []string{
				"INSERT INTO t (a) VALUES (1)",
				"INSERT INTO t (a, b) VALUES (1, 2)",
			},
		},
		{
			// VALUES only groups a VALUES list, not any parenthesized one.
			name: "a VALUES-free insert",
			sqls: []string{
				"INSERT INTO t (a) SELECT 1",
				"INSERT INTO t (a) SELECT 1, 2",
			},
		},
	}

	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			t.Parallel()

			seen := map[string]string{}
			for _, sql := range group.sqls {
				got, err := Sanitize(storepb.Engine_MYSQL, sql)
				require.NoError(t, err)
				if other, ok := seen[got.Fingerprint]; ok {
					t.Fatalf("%q and %q share a fingerprint %q", sql, other, got.Canonical)
				}
				seen[got.Fingerprint] = sql
			}
		})
	}
}

// TestConstantRunMustBeginAtThePlaceholder pins the trap: a run that may begin at an operator
// swallows the identifier before it. `WHERE a - 1 > 0` is `a - ? > ?`, and folding `- ? > ?` would
// leave `a ?` — which collides with statements that share nothing but the identifier.
func TestConstantRunMustBeginAtThePlaceholder(t *testing.T) {
	t.Parallel()

	require.Equal(t, "SELECT a FROM t WHERE a - ?", canonicalOf(t, "SELECT a FROM t WHERE a - 1 > 0"))
	require.Equal(t, "SELECT a FROM t WHERE a = ?", canonicalOf(t, "SELECT a FROM t WHERE a = 1"))

	// The two are different statements, and stay different.
	left, err := Sanitize(storepb.Engine_MYSQL, "SELECT a FROM t WHERE a - 1 > 0")
	require.NoError(t, err)
	right, err := Sanitize(storepb.Engine_MYSQL, "SELECT a FROM t WHERE a = 1")
	require.NoError(t, err)
	require.NotEqual(t, left.Fingerprint, right.Fingerprint)
	require.False(t, strings.Contains(left.Canonical, "a ?"),
		"the identifier must not be joined by a folded run: %q", left.Canonical)
}

// TestFoldingIsIdempotent pins that folding an already folded form changes nothing. Without it a
// statement reported twice would be two rows, which is the failure the rules exist to prevent.
func TestFoldingIsIdempotent(t *testing.T) {
	t.Parallel()

	sqls := []string{
		"SELECT a FROM t WHERE x IN (1, 2, 3)",
		"INSERT INTO t (a, b) VALUES (1, 2), (3, 4)",
		"SELECT a FROM t WHERE x = 1 + 1",
		"SELECT a FROM t WHERE a - 1 > 0 AND b IN (1, 2)",
		"INSERT INTO t (a) VALUES (1) ON DUPLICATE KEY UPDATE a = 2",
	}
	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS} {
		for _, sql := range sqls {
			once, err := Sanitize(engine, sql)
			if err != nil {
				continue
			}
			require.Equal(t, once.Canonical, canonicalForm(engine, once.Text), "%s %s", engine, sql)
			require.Equal(t, once.Canonical, canonicalForm(engine, once.Canonical), "%s %s", engine, sql)
		}
	}
}
