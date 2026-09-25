// Package engines_test pins the assembly itself: every engine this build
// analyzes is in the list, each is claimed once, and the engines that are
// deliberately absent report the sentinel the runner records as a skip.
package engines_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
)

// supported are the engines a deployment can analyze SQL for, one per dialect
// package in this module.
var supported = []storepb.Engine{
	storepb.Engine_MYSQL,
	storepb.Engine_TIDB,
	storepb.Engine_MARIADB,
	storepb.Engine_POSTGRES,
	storepb.Engine_STARROCKS,
}

// unanalyzable are the engines that have a driver but deliberately no analyzer:
// the runner records a per-object skip for them rather than reading their SQL
// with another engine's grammar.
var unanalyzable = []storepb.Engine{
	storepb.Engine_OCEANBASE,
	storepb.Engine_DORIS,
	storepb.Engine_MSSQL,
}

// TestAssemblyCoversEveryAnalyzedEngine pins the list the process builds its
// analyzer from. A dialect nobody lists is a dialect that silently never runs —
// which is exactly the failure a compile-time assembly is meant to make
// impossible, so the set is checked rather than assumed.
func TestAssemblyCoversEveryAnalyzedEngine(t *testing.T) {
	t.Parallel()

	claimed := make(map[storepb.Engine]bool, len(supported))
	analyzer := lineage.NewAnalyzer(nil, engines.Registrations()...)
	for _, engine := range supported {
		relations, err := analyzer.Analyze(context.TODO(), engine, "SELECT a.id FROM users a")
		require.NoError(t, err, "engine %s has no analyzer in the assembly", engine)
		require.NotEmpty(t, relations, "engine %s analyzed the statement to nothing", engine)
		claimed[engine] = true
	}
	require.Len(t, claimed, len(supported))
}

// TestEachRegistrationIsCompleteAndUnique pins that every entry names one engine
// and both halves of its seam. A nil splitter would make a multi-statement script
// silently analyzed whole, and a nil analyzer would panic at the first statement.
func TestEachRegistrationIsCompleteAndUnique(t *testing.T) {
	t.Parallel()

	registrations := engines.Registrations()
	require.Len(t, registrations, len(supported))

	claimed := make([]storepb.Engine, 0, len(registrations))
	for _, registration := range registrations {
		require.NotNil(t, registration.Analyze, "engine %s has no analyzer", registration.Engine)
		require.NotNil(t, registration.Split, "engine %s has no script splitter", registration.Engine)
		claimed = append(claimed, registration.Engine)
	}
	require.ElementsMatch(t, supported, claimed)
}

// TestUnanalyzedEnginesReportTheSentinel pins the boundary the runner reads: an
// engine outside the assembly is a skip, not a failure and not an analysis with
// another engine's grammar.
func TestUnanalyzedEnginesReportTheSentinel(t *testing.T) {
	t.Parallel()

	analyzer := lineage.NewAnalyzer(nil, engines.Registrations()...)
	for _, engine := range unanalyzable {
		_, err := analyzer.Analyze(context.TODO(), engine, "SELECT id FROM t")
		require.ErrorIs(t, err, lineage.ErrorEngineNotSupported, "engine %s", engine)
	}
}

// TestDuplicateRegistrationIsRefused pins that the assembly list cannot quietly
// keep the last entry: two dialects claiming one engine is a build mistake.
func TestDuplicateRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	registration := engines.Registrations()[0]
	require.Panics(t, func() {
		lineage.NewAnalyzer(nil, registration, registration)
	})
}
