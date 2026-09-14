package starrocks

import (
	"testing"

	"github.com/stretchr/testify/require"
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
