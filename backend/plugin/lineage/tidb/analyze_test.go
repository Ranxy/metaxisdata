package tidb

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestAnalyzeYAML runs the shared MySQL-family golden corpus through this
// dialect's analyzer.
func TestAnalyzeYAML(t *testing.T) {
	testutil.RunLineageTestSuitesFromYAMLDir(t, sharedCorpusDir(), analyzeSQL)
	testutil.RunLineageTestSuitesFromYAMLDir(t, dialectCorpusDir(), analyzeSQL)
}

// TestCorpusIsFullyAnnotated keeps every corpus case asserting the fields a
// consumer reads, so a new case cannot widen the contract by omission.
func TestCorpusIsFullyAnnotated(t *testing.T) {
	testutil.RequireFullEdgeAnnotations(t, sharedCorpusDir())
	testutil.RequireFullEdgeAnnotations(t, dialectCorpusDir())
}
