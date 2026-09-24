package starrocks

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestCatalogIsReadOncePerRelation pins the analyzer-level catalog cache. It used
// to be per reference, so a statement that named a relation twice read the store
// twice while the MySQL-family analyzers read it once.
func TestCatalogIsReadOncePerRelation(t *testing.T) {
	t.Parallel()

	cat := testutil.NewMemoryCatalogProvide()
	cat.AddTable(&catalog.TableMeta{
		ID:      model.ObjectIdentifier{Name: "t"},
		Columns: []catalog.ColumnMeta{{Name: "a"}, {Name: "b"}},
	})

	_, err := analyzeSQL("SELECT * FROM t x JOIN t y ON x.a = y.b", cat)
	require.NoError(t, err)
	require.Equal(t, 1, cat.Calls(), "the same relation was read more than once")
}

// TestCatalogFailureIsReportedAsAGap pins that a lookup the catalog could not
// answer is visible in the result. The analysis still falls back to a wildcard
// edge, but it says the lookup failed, so a catalog outage can no longer produce a
// result indistinguishable from a complete one.
func TestCatalogFailureIsReportedAsAGap(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT * FROM t", testutil.NewFailingCatalogProvide(errors.New("connection reset")))

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "catalog lookup failed for t: connection reset")
	require.NotEmpty(t, relations, "the wildcard fallback edge is still produced")
}
