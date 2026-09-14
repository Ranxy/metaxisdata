package mariadb

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// knownParserGaps lists shared-corpus cases omni's MariaDB parser cannot parse
// yet: a join tree nested in three or more parentheses, e.g.
// `FROM (((a JOIN b ON ...)))`, which is what mysqldump emits for views. omni's
// MySQL parser accepts any depth; the MariaDB parser stops at two. These
// statements hard-fail in production too, so the gap is recorded here rather
// than hidden. Remove an entry once the parser accepts the statement.
var knownParserGaps = map[string]bool{
	"select with parenthesized join tree":                    true,
	"INSERT SELECT with parenthesized join tree":             true,
	"CREATE TABLE AS SELECT with parenthesized join tree":    true,
	"CREATE VIEW with deeply nested parenthesized join tree": true,
}

// TestAnalyzeYAML runs the shared MySQL-family golden corpus through the MariaDB
// analyzer, minus statements the MariaDB parser cannot parse yet.
func TestAnalyzeYAML(t *testing.T) {
	testutil.RunLineageTestSuitesFromYAMLDirSkipping(t, sharedCorpusDir(), analyzeSQL, knownParserGaps)
}
