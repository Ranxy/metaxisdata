package doris

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

// The package's Registration binds DORIS.
func TestRegistersDorisEngine(t *testing.T) {
	t.Parallel()

	relations, err := thisEngine.Analyze(context.TODO(), storepb.Engine_DORIS, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// StarRocks has its own analyzer package, so this one must not claim it: two
// dialects serving one engine is an assembly mistake the process refuses.
func TestStarRocksIsNotRegistered(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_STARROCKS, storepb.Engine_OCEANBASE} {
		_, err := thisEngine.Analyze(context.TODO(), engine, "SELECT id FROM t")
		require.ErrorIs(t, err, lineage.ErrorEngineNotSupported, "engine %s", engine)
	}
}
