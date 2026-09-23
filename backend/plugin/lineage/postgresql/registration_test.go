package postgresql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// The package registers its engine through the shared seam, so the lineage
// runner and Explain SQL resolve it without an engine-specific branch.
func TestRegistersEngine(t *testing.T) {
	t.Parallel()

	relations, err := lineage.GetAnalyzeRelation(context.TODO(), storepb.Engine_POSTGRES, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// An engine with no analyzer must report the shared sentinel, so the runner can
// record a per-object skip instead of a failure. Doris and OceanBase have drivers
// but no lineage analyzer, which is what makes them the engines to check.
func TestUnsupportedEngineIsNotRegistered(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_DORIS, storepb.Engine_OCEANBASE} {
		_, err := lineage.GetAnalyzeRelation(context.TODO(), engine, "SELECT id FROM t")
		require.ErrorIs(t, err, lineage.ErrorEngineNotSupported, "engine %s", engine)
	}
}
