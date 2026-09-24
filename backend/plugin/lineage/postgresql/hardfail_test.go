package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
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

// TestRejectsMultiStatement pins the single-statement contract. A script is split
// by the caller and fed to this analyzer one statement at a time, so one
// statement's scope can never leak into the next.
func TestRejectsMultiStatement(t *testing.T) {
	relations, err := analyzeSQL("SELECT a FROM t1; SELECT b FROM t2", nil)
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
