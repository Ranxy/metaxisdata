package starrocks

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// analyzeSQL adapts the analyzer to the shared lineage test harness.
func analyzeSQL(sql string, cat catalog.Provide) ([]model.ColumnRelation, error) {
	return NewAnalyzer(context.TODO(), sql, cat).AnalyzeRelations()
}

// testdataPath resolves a path under this package's testdata directory.
func testdataPath(elem ...string) string {
	_, filename, _, _ := runtime.Caller(0)
	parts := append([]string{filepath.Dir(filename), "testdata"}, elem...)
	return filepath.Join(parts...)
}

// RunLineageYAMLTestSuites executes all YAML-backed lineage suites for
// StarRocks.
func RunLineageYAMLTestSuites(t *testing.T) {
	testutil.RunLineageTestSuitesFromYAMLDir(t, testdataPath("analyze"), analyzeSQL)
}
