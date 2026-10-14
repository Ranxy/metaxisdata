package mariadb

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// knownParserGaps lists shared-corpus cases omni's MariaDB parser cannot parse
// yet. These statements hard-fail in production too, so the gap is recorded here
// rather than hidden. Remove an entry once the parser accepts the statement.
//
// The parser used to stop at two levels of parenthesized join tree — `FROM
// (((a JOIN b ON ...)))`, which is what mysqldump emits for views — where omni's
// MySQL parser accepts any depth. It accepts three now, matching MySQL.
var knownParserGaps = map[string]bool{
	// A row alias in INSERT ... VALUES ... ON DUPLICATE KEY UPDATE is MySQL
	// 8.0.19 syntax; MariaDB has no such form, so its parser rejects it.
	"an upsert assignment reads the proposed value through its row alias": true,
}

// TestAnalyzeYAML runs the shared MySQL-family golden corpus through the MariaDB
// analyzer, minus statements the MariaDB parser cannot parse yet.
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
