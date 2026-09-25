package mariadb

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// thisEngine is this dialect's registration on its own. The process assembles the
// whole set; a test here only pins which engine this package claims, which is what
// keeps the runner and Explain SQL from needing an engine-specific branch.
var thisEngine = lineage.NewAnalyzer(nil, Registration())

// The package's Registration binds MARIADB.
func TestRegistersEngine(t *testing.T) {
	t.Parallel()

	relations, err := thisEngine.Analyze(context.TODO(), storepb.Engine_MARIADB, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// An engine with no analyzer must report the shared sentinel, so the runner can
// record a per-object skip instead of a failure.
func TestUnsupportedEngineIsNotRegistered(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_DORIS, storepb.Engine_OCEANBASE} {
		_, err := thisEngine.Analyze(context.TODO(), engine, "SELECT id FROM t")
		require.ErrorIs(t, err, lineage.ErrorEngineNotSupported, "engine %s", engine)
	}
}

// TestKnownParserGapsAreStillGaps pins the skip list: if omni's MariaDB parser
// starts accepting one of these statements this fails, so the entry is
// re-examined instead of being silently skipped forever.
func TestKnownParserGapsAreStillGaps(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(sharedCorpusDir())
	require.NoError(t, err)

	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		suite, err := testutil.LoadLineageTestSuiteFromYAML(filepath.Join(sharedCorpusDir(), entry.Name()))
		require.NoError(t, err)
		for _, tc := range suite.Cases {
			if !knownParserGaps[tc.Name] {
				continue
			}
			seen[tc.Name] = true
			_, err := analyzeSQL(tc.SQL, tc.Catalog)
			require.Error(t, err, "case %q parses now; remove it from knownParserGaps", tc.Name)
		}
	}

	for name := range knownParserGaps {
		require.True(t, seen[name], "knownParserGaps entry %q does not match any shared corpus case", name)
	}
}
