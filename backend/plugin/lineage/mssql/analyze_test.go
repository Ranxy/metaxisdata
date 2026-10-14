package mssql

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestAnalyzeYAML runs every corpus case through the analyzer and compares the
// edges exactly.
func TestAnalyzeYAML(t *testing.T) {
	RunLineageYAMLTestSuites(t)
}

// TestCorpusIsFullyAnnotated keeps every corpus case asserting the fields a
// consumer reads, so a new case cannot widen the contract by omission.
func TestCorpusIsFullyAnnotated(t *testing.T) {
	testutil.RequireFullEdgeAnnotations(t, testdataPath("analyze"))
}
