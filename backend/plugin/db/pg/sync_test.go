package pg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestViewDependenciesQueryBindsNames guards the view-dependency lookup: the
// schema and view names read from the target catalog must stay bound
// parameters, never interpolated into string literals.
func TestViewDependenciesQueryBindsNames(t *testing.T) {
	t.Parallel()

	require.Contains(t, viewDependenciesQuery, "dependency_ns.nspname = $1")
	require.Contains(t, viewDependenciesQuery, "dependency_view.relname = $2")
	require.NotContains(t, viewDependenciesQuery, "%s", "query must not contain format verbs")
}
