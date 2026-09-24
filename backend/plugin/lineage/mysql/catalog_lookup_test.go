package mysql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestCatalogIsReadOncePerRelation pins the analyzer-level catalog cache, which
// every dialect now shares: a statement that names a relation twice reads it once
// however many references it makes.
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
// answer is visible in the result. It used to be swallowed, which made a catalog
// outage indistinguishable from a complete analysis.
func TestCatalogFailureIsReportedAsAGap(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT * FROM t", testutil.NewFailingCatalogProvide(errors.New("connection reset")))

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "catalog lookup failed for t: connection reset")
	require.NotEmpty(t, relations, "the wildcard fallback edge is still produced")
}
