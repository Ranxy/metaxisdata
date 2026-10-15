package starrocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// thisEngine is this dialect's registration on its own. The process assembles the
// whole set; a test here only pins which engine this package claims, which is what
// keeps the runner and Explain SQL from needing an engine-specific branch.
var thisEngine = lineage.NewAnalyzer(nil, Registration())

// The package's Registration binds STARROCKS.
func TestRegistersStarRocksEngine(t *testing.T) {
	t.Parallel()

	relations, err := thisEngine.Analyze(context.TODO(), storepb.Engine_STARROCKS, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// Doris is not registered by this package: it has its own analyzer, because omni's
// Doris grammar differs from this one, so it must never be read with the StarRocks
// grammar.
func TestDorisIsNotRegistered(t *testing.T) {
	t.Parallel()

	_, err := thisEngine.Analyze(context.TODO(), storepb.Engine_DORIS, "SELECT id FROM t")
	require.ErrorIs(t, err, lineage.ErrorEngineNotSupported)
}
