package mysql

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

// The package's Registration binds MYSQL.
func TestRegistersMySQLEngine(t *testing.T) {
	t.Parallel()

	relations, err := thisEngine.Analyze(context.TODO(), storepb.Engine_MYSQL, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// OceanBase is deliberately left unregistered: the runner records a per-object
// skip for it rather than analyzing OceanBase SQL with the MySQL grammar.
func TestOceanBaseIsNotRegistered(t *testing.T) {
	t.Parallel()

	_, err := thisEngine.Analyze(context.TODO(), storepb.Engine_OCEANBASE, "SELECT id FROM t")
	require.ErrorIs(t, err, lineage.ErrorEngineNotSupported)
}
