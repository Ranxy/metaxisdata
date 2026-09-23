package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
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

// TestUnsupportedStatementKeepsOtherStatements pins what a parsed statement the
// analyzer cannot model does to the rest of the input. It used to fail the whole
// text, so one MERGE beside a SELECT discarded the SELECT's lineage and the
// runner then cleared the object's stored lineage; the edges are now kept and the
// gap is reported instead.
func TestUnsupportedStatementKeepsOtherStatements(t *testing.T) {
	const sql = "SELECT a FROM t1; MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET a = s.a"
	relations, err := analyzeSQL(sql, nil)

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported, "a MERGE has to be reported, not silently skipped")
	require.Contains(t, unsupported.Error(), "MERGE")
	require.True(t, hasResultEdge(relations, "t1", "a", "a"),
		"the other statement's edge must survive: %s", testutil.FormatRelations(relations))
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

	require.True(t, hasResultEdge(relations, "t1", "a", "a"),
		"first statement's edge missing: %s", testutil.FormatRelations(relations))
	require.True(t, hasResultEdge(relations, "t2", "b", "b"),
		"second statement's edge missing: %s", testutil.FormatRelations(relations))
}

// TestParseErrorFailsWholeInput asserts a parse error anywhere rejects the whole
// input rather than returning the statements that did parse.
func TestParseErrorFailsWholeInput(t *testing.T) {
	relations, err := analyzeSQL("SELECT a FROM t1; THIS IS NOT SQL", nil)
	require.Error(t, err)
	require.Nil(t, relations)
}

// hasResultEdge reports whether an edge exists from sourceTable.sourceColumn to
// __result__.targetColumn.
func hasResultEdge(relations []model.ColumnRelation, sourceTable, sourceColumn, targetColumn string) bool {
	for _, rel := range relations {
		if rel.Source.Table.Name == sourceTable && rel.Source.Name == sourceColumn &&
			rel.Target.Table.Name == resultTableName && rel.Target.Name == targetColumn {
			return true
		}
	}
	return false
}
