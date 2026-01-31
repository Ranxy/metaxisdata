package mysql

import (
	"context"
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// Re-export types from testutil for convenience
type ExpectedEdge = testutil.ExpectedEdge
type LineageTestCase = testutil.LineageTestCase

// Re-export helper functions from testutil
var (
	Bool    = testutil.Bool
	Int     = testutil.Int
	RelType = testutil.RelType

	// Catalog creation
	CreateSimpleCatalog = testutil.CreateSimpleCatalog
)

// analyzeSQL is the MySQL-specific implementation of the analyze function.
func analyzeSQL(sql string, cat catalog.Provide) ([]model.ColumnRelation, error) {
	ctx := context.TODO()
	analyzer := NewAnalyzer(ctx, sql, cat)
	return analyzer.AnalyzeRelations()
}

// RunLineageTests executes a slice of lineage test cases using the MySQL analyzer.
func RunLineageTests(t *testing.T, testCases []LineageTestCase) {
	testutil.RunLineageTests(t, testCases, analyzeSQL)
}

// RunLineageTest executes a single lineage test case using the MySQL analyzer.
func RunLineageTest(t *testing.T, tc LineageTestCase) {
	testutil.RunLineageTest(t, tc, analyzeSQL)
}
