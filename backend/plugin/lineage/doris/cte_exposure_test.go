package doris

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
)

// TestACTEsExposedColumnsComeFromItsBody pins that a reference to a CTE resolves
// against the names its body exposes: a declared list renames them positionally, and a
// column that carries no source — a constant, which has no lineage to record — is still
// a column of the relation. Describing the CTE from its lineage targets alone left that
// column out, so `WITH c AS (SELECT 1 AS x, k FROM ev2) SELECT x FROM c, ev2` was a gap
// for a statement the engine runs (checked on 4.1.4), while the same reference in a
// single-relation scope only resolved through the fallback a lone relation keeps.
func TestACTEsExposedColumnsComeFromItsBody(t *testing.T) {
	t.Parallel()

	t.Run("a constant column is exposed even though it has no lineage", func(t *testing.T) {
		t.Parallel()

		relations, err := analyzeSQL("WITH c AS (SELECT 1 AS x, k FROM ev2) SELECT x AS n FROM c, ev2", variantCatalog())

		require.NoError(t, err)
		require.Empty(t, relations, "a column built from a constant has no source to record")
	})

	t.Run("a column the body does not expose is still a gap", func(t *testing.T) {
		t.Parallel()

		relations, err := analyzeSQL("WITH c AS (SELECT k FROM ev2) SELECT x AS n FROM c, ev2", variantCatalog())

		var unsupported *lineage.UnsupportedStatementError
		require.ErrorAs(t, err, &unsupported)
		require.Contains(t, unsupported.Error(), "unresolved reference in an output column: x")
		require.Empty(t, relations)
	})

	t.Run("a declared column list renames the body positionally", func(t *testing.T) {
		t.Parallel()

		requireEdgesWithCatalog(t, "WITH c (a) AS (SELECT k FROM ev2) SELECT a AS n FROM c, ev2",
			variantCatalog(), []string{"ev2.k -> __result__.n"})
	})

	t.Run("a list shorter than the body leaves the rest at their alias", func(t *testing.T) {
		t.Parallel()

		// Doris renames from the left and leaves the outputs a short list does not
		// reach at their query alias, which is why the exposure is built positionally
		// rather than through scope.ExposedColumnNames: on 4.1.4 both
		// `WITH c (a) AS (SELECT k, name FROM t) SELECT a FROM c` and `SELECT name FROM
		// c` run, so neither name may become a gap.
		requireEdgesWithCatalog(t, "WITH c (a) AS (SELECT k, name FROM ev2) SELECT a AS n FROM c, ev2",
			variantCatalog(), []string{"ev2.k -> __result__.n"})
		requireEdgesWithCatalog(t, "WITH c (a) AS (SELECT k, name FROM ev2) SELECT name AS n FROM c, ev2",
			variantCatalog(), []string{"ev2.name -> __result__.n"})
	})

	t.Run("the body's alias is the name that resolves", func(t *testing.T) {
		t.Parallel()

		requireEdgesWithCatalog(t, "WITH c AS (SELECT k AS renamed FROM ev2) SELECT renamed AS n FROM c",
			variantCatalog(), []string{"ev2.k -> __result__.n"})
	})
}
