package mysql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// The package registers MYSQL through the shared seam, so the lineage runner and
// Explain SQL resolve it without an engine-specific branch.
func TestRegistersMySQLEngine(t *testing.T) {
	t.Parallel()

	relations, err := lineage.GetAnalyzeRelation(context.TODO(), storepb.Engine_MYSQL, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// OceanBase is deliberately left unregistered: the runner records a per-object
// skip for it rather than analyzing OceanBase SQL with the MySQL grammar.
func TestOceanBaseIsNotRegistered(t *testing.T) {
	t.Parallel()

	_, err := lineage.GetAnalyzeRelation(context.TODO(), storepb.Engine_OCEANBASE, "SELECT id FROM t")
	require.ErrorIs(t, err, lineage.ErrorEngineNotSupported)
}
