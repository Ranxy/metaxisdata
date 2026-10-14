package mssql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHardFailOnUnparseableSQL pins the decided parse policy: a statement omni cannot
// parse is an error with no partial result, never a silent empty lineage. The
// parenthesized view body and the OPENROWSET(BULK ...) source are shapes omni's T-SQL
// grammar does not accept; they are recorded in .agents/docs/omni-upstream-defects.md
// and are hard failures here, not gaps.
func TestHardFailOnUnparseableSQL(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT FROM WHERE",
		"NOT SQL AT ALL",
		"SELECT * FROM",
		"CREATE VIEW v AS (SELECT a FROM t)",
		"SELECT * FROM OPENROWSET(BULK 'file.txt', SINGLE_CLOB) AS t",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.Error(t, err, "expected a hard failure for %q", sql)
		require.Nil(t, relations, "expected no partial result for %q", sql)
	}
}

// TestRejectsMultiStatement pins the single-statement contract: the script splitter
// lives above the dialects, so a dialect handed two statements refuses rather than
// analyzing them in one scope.
func TestRejectsMultiStatement(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT a FROM t; SELECT b FROM t", nil)
	require.Error(t, err)
	require.Nil(t, relations)
}

// TestRejectsEmptyInput pins that a statement carrying no SQL is not analyzed into an
// empty result that reads like "this object has no lineage".
func TestRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"", "   ", "/* Definition of view dbo.v is encrypted. */"} {
		relations, err := analyzeSQL(sql, nil)
		require.Error(t, err, "expected a failure for %q", sql)
		require.Nil(t, relations)
	}
}
