package tidb

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// knownParserGaps lists shared-corpus cases omni's TiDB parser cannot parse yet.
// A VALUES row set is parsed as a standalone statement but not as a query primary
// (`FROM (VALUES ROW(1)) v`), so the shape hard-fails in production too and is
// recorded here rather than hidden. Remove an entry once the parser accepts it.
var knownParserGaps = map[string]bool{
	"a derived table of VALUES rows reads a subquery row": true,
}

// TestAnalyzeYAML runs the shared MySQL-family golden corpus through this
// dialect's analyzer, minus statements its parser cannot parse yet.
func TestAnalyzeYAML(t *testing.T) {
	testutil.RunLineageTestSuitesFromYAMLDirSkipping(t, sharedCorpusDir(), analyzeSQL, knownParserGaps)
	testutil.RunLineageTestSuitesFromYAMLDir(t, dialectCorpusDir(), analyzeSQL)
}

// TestCorpusIsFullyAnnotated keeps every corpus case asserting the fields a
// consumer reads, so a new case cannot widen the contract by omission.
func TestCorpusIsFullyAnnotated(t *testing.T) {
	testutil.RequireFullEdgeAnnotations(t, sharedCorpusDir())
	testutil.RequireFullEdgeAnnotations(t, dialectCorpusDir())
}
