package mysql

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// dialectBodyMarker is where the MySQL-family analyzers stop differing: their
// header, package clause, omni import paths and init block are per dialect, and
// everything after this line is the shared traversal.
const dialectBodyMarker = "// Analyzer performs direct lineage analysis on MySQL queries.\n"

// The MariaDB and TiDB analyzers are copies of this one, kept in sync by
// regenerating them from it. Everything from the Analyzer type onward has to stay
// byte-identical, so a hand-edit to one copy cannot silently drift from the
// others — which is how the MySQL copy came to ignore a CTE's column list while
// PostgreSQL honoured it, and how the derived-table alias list went missing in two
// engines at once.
func TestDialectCopiesStayInSync(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	body := func(dialect string) string {
		path := filepath.Join(filepath.Dir(filename), "..", dialect, "analyzer.go")
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		index := bytes.Index(content, []byte(dialectBodyMarker))
		require.NotEqualf(t, -1, index, "%s does not carry the shared body marker", path)
		return string(content[index:])
	}

	mysqlBody := body("mysql")
	for _, dialect := range []string{"mariadb", "tidb"} {
		require.Equalf(t, mysqlBody, body(dialect),
			"the %s analyzer has drifted from the MySQL one; regenerate it from mysql/analyzer.go", dialect)
	}
}
