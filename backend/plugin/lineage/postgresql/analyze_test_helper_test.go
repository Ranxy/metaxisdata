package postgresql

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// analyzeSQL is the PostgreSQL-specific implementation of the analyze function.
func analyzeSQL(sql string, cat catalog.Provide) ([]model.ColumnRelation, error) {
	ctx := context.TODO()
	analyzer := NewAnalyzer(ctx, sql, cat)
	return analyzer.AnalyzeRelations()
}

func testdataPath(elem ...string) string {
	_, filename, _, _ := runtime.Caller(0)
	parts := append([]string{filepath.Dir(filename), "testdata"}, elem...)
	return filepath.Join(parts...)
}

// RunLineageYAMLTestSuites executes all YAML-backed lineage suites for PostgreSQL.
func RunLineageYAMLTestSuites(t *testing.T) {
	testutil.RunLineageTestSuitesFromYAMLDir(t, testdataPath("analyze"), analyzeSQL)
}
