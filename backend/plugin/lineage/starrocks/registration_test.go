package starrocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// The package registers STARROCKS through the shared seam, so the lineage
// runner and Explain SQL resolve it without an engine-specific branch.
func TestRegistersStarRocksEngine(t *testing.T) {
	t.Parallel()

	relations, err := lineage.GetAnalyzeRelation(context.TODO(), storepb.Engine_STARROCKS, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// DORIS is deliberately left unregistered: the runner records a per-object skip
// for it rather than analyzing Doris SQL with the StarRocks grammar.
func TestDorisIsNotRegistered(t *testing.T) {
	t.Parallel()

	_, err := lineage.GetAnalyzeRelation(context.TODO(), storepb.Engine_DORIS, "SELECT id FROM t")
	require.ErrorIs(t, err, lineage.ErrorEngineNotSupported)
}
