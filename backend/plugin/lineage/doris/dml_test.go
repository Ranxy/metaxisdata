package doris

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// DML that reads no column must return no edges and no error: an INSERT ...
// VALUES carries only literals, and a DELETE with no WHERE removes every row.
func TestDMLWithoutLineage(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"INSERT INTO t2 VALUES (1, 'a')",
		"DELETE FROM t1",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.NoError(t, err, "sql=%q", sql)
		require.Empty(t, relations, "sql=%q", sql)
	}
}

// An assignment whose target qualifier names no relation the statement introduced
// writes no column this analysis can attribute, so it is a reported gap rather
// than a silently empty result. Doris accepts the spelling; the qualifier is only
// checked, never used to name the target.
func TestAnUnattributableAssignmentTargetIsAGap(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("UPDATE t SET x.a = 1", nil)

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "unresolved reference in an assignment target: x.a")
	require.Empty(t, relations)
}
