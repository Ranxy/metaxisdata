package tidb

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

	relations, err := lineage.GetAnalyzeRelation(context.TODO(), storepb.Engine_TIDB, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}
