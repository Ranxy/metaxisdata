package tidb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHardFailOnUnparseableSQL pins the parse policy: a statement this dialect's
// parser cannot handle is an error with no partial result.
func TestHardFailOnUnparseableSQL(t *testing.T) {
	for _, sql := range []string{
		"SELECT FROM WHERE",
		"THIS IS NOT SQL AT ALL",
		"SELECT * FROM",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.Error(t, err, "expected a hard failure for %q", sql)
		require.Nil(t, relations, "expected no partial result for %q", sql)
	}
}

// TestRejectsMultiStatement pins the single-statement contract.
func TestRejectsMultiStatement(t *testing.T) {
	relations, err := analyzeSQL("SELECT a FROM t; SELECT b FROM t", nil)
	require.Error(t, err)
	require.Nil(t, relations)
}
