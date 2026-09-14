package starrocks

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
