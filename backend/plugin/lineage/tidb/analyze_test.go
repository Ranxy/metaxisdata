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
