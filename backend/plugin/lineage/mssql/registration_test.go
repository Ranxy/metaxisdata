package mssql

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

// The package's Registration binds MSSQL.
func TestRegistersMSSQLEngine(t *testing.T) {
	t.Parallel()

	relations, err := thisEngine.Analyze(context.TODO(), storepb.Engine_MSSQL, "SELECT a.id FROM users a")
	require.NoError(t, err)
	require.NotEmpty(t, relations)
}

// A neighbouring engine is not claimed by this package: each dialect binds exactly
// the engine whose grammar it reads.
func TestDoesNotClaimAnotherEngine(t *testing.T) {
	t.Parallel()

	_, err := thisEngine.Analyze(context.TODO(), storepb.Engine_POSTGRES, "SELECT id FROM t")
	require.ErrorIs(t, err, lineage.ErrorEngineNotSupported)
}

// TestBatchSeparatedScriptIsSplitIntoStatements pins that `GO`, which is not SQL at
// all, neither disappears with the statements around it nor becomes a statement of
// its own: both statements it separates are analyzed and their edges merge.
func TestBatchSeparatedScriptIsSplitIntoStatements(t *testing.T) {
	t.Parallel()

	relations, err := thisEngine.Analyze(context.TODO(), storepb.Engine_MSSQL,
		"SELECT a FROM t1;\nGO\nSELECT b FROM t2\nGO")
	require.NoError(t, err)
	require.Len(t, relations, 2)
	require.Equal(t, "t1", relations[0].Source.Table.Name)
	require.Equal(t, "t2", relations[1].Source.Table.Name)
}
