package doris

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHardFailOnUnparseableSQL pins the decided parse policy: a statement omni
// cannot parse is an error with no partial result, never a silent empty
// lineage.
func TestHardFailOnUnparseableSQL(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT FROM WHERE",
		"THIS IS NOT SQL AT ALL",
		"SELECT * FROM",
		// A view statement whose extracted body is itself malformed must still
		// fail rather than silently yielding no lineage.
		"CREATE VIEW v AS SELECT * FROM (SELECT",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.Error(t, err, "expected a hard failure for %q", sql)
		require.Nil(t, relations, "expected no partial result for %q", sql)
	}
}

// TestRejectsMultiStatement pins the single-statement contract.
func TestRejectsMultiStatement(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT a FROM t; SELECT b FROM t", nil)
	require.Error(t, err)
	require.Nil(t, relations)
}

// TestAFailedStatementKeepsItsOwnError pins that the view-body fallback only takes
// over view DDL, and that a statement it refuses fails with the parser's own
// message. The keyword VIEW also appears inside an ordinary statement's LATERAL
// VIEW, so without that gate two things went wrong: a SELECT was reported as a
// broken "view query", and an identifier in an unmodelled view header
// (`REFRESH ASYNC START(...)`, `DEFINER = 'u'`) was read as the target name — the
// second silently, because the fallback then succeeded against a fabricated object.
func TestAFailedStatementKeepsItsOwnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sql  string
		want string
	}{
		{
			sql:  "SELECT * FROM (SELECT arr FROM t) d LATERAL VIEW explode(d.arr) u AS elem",
			want: "syntax error at or near LATERAL",
		},
		{
			sql:  "CREATE MATERIALIZED VIEW mv REFRESH ASYNC START('2022-07-25 10:00:00') EVERY(INTERVAL 1 DAY) AS SELECT id FROM t1",
			want: "syntax error at or near 2022-07-25 10:00:00",
		},
		{
			sql:  "CREATE VIEW v DEFINER = 'u' AS SELECT id FROM t1",
			want: "syntax error at or near DEFINER",
		},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			t.Parallel()

			relations, err := analyzeSQL(tt.sql, nil)
			require.Error(t, err)
			require.Nil(t, relations, "no edge may be recorded for a statement that did not parse")
			require.ErrorContains(t, err, tt.want)
			require.NotContains(t, err.Error(), "view query")
		})
	}
}
