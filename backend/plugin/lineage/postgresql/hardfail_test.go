package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestHardFailOnUnparseableSQL pins the decided parse policy: a statement omni
// cannot parse is an error with no partial result, never a silent empty lineage.
func TestHardFailOnUnparseableSQL(t *testing.T) {
	for _, sql := range []string{
		"SELECT FROM WHERE",
		"THIS IS NOT SQL AT ALL",
		"SELECT * FROM",
		"INSERT INTO",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.Error(t, err, "expected a hard failure for %q", sql)
		require.Nil(t, relations, "expected no partial result for %q", sql)
	}
}

// TestMultiStatementAnalyzed pins the decided multi-statement policy. Unlike the
// MySQL analyzer (which rejects multi-statement input), the PostgreSQL analyzer
// processes every well-formed statement on shared analyzer state, matching the
// legacy stmtmulti behavior that MANUAL_SQL relied on.
//
// Qualified references are used deliberately: because all statements share the
// root scope, an unqualified column in a later statement resolves against the
// tables accumulated by earlier statements (legacy behavior, preserved here).
func TestMultiStatementAnalyzed(t *testing.T) {
	relations, err := analyzeSQL("SELECT t1.a FROM t1; SELECT t2.b FROM t2", nil)
	require.NoError(t, err)

	require.True(t, hasEdge(relations, "t1", "a", resultTableName, "a"),
		"first statement's edge missing: %s", testutil.FormatRelations(relations))
	require.True(t, hasEdge(relations, "t2", "b", resultTableName, "b"),
		"second statement's edge missing: %s", testutil.FormatRelations(relations))
}

// TestParseErrorFailsWholeInput asserts a parse error anywhere rejects the whole
// input rather than returning the statements that did parse.
func TestParseErrorFailsWholeInput(t *testing.T) {
	relations, err := analyzeSQL("SELECT a FROM t1; THIS IS NOT SQL", nil)
	require.Error(t, err)
	require.Nil(t, relations)
}

func hasEdge(relations []model.ColumnRelation, sourceTable, sourceColumn, targetTable, targetColumn string) bool {
	for _, rel := range relations {
		if rel.Source.Table.Name == sourceTable && rel.Source.Name == sourceColumn &&
			rel.Target.Table.Name == targetTable && rel.Target.Name == targetColumn {
			return true
		}
	}
	return false
}
