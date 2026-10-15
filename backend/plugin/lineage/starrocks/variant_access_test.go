package starrocks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestVARIANTIsReadByFunctionOnStarRocks pins how a VARIANT column is actually
// read here. StarRocks exposes its subcolumns through functions taking a JSONPath —
// `variant_query(v, '$.user.name')`, `get_variant` — and the documented access is
// that function form, not a dotted path:
//
//	starrocks/allin1 4.1: SELECT v.user.name FROM ev   -> Column 'v.user.name' cannot be resolved
//	                      SELECT v['user'] FROM ev      -> cannot subscript type VARIANT
//	                      SELECT variant_query(v, '$.user.name') FROM ev -> accepted
//
// So the function form already yields the column as the whole source through the
// generic expression walk, and the dot form stays the unresolved gap the engine
// would also fail on. The Doris subcolumn path reading is deliberately not mirrored
// to this dialect: a mirror would record edges for a statement this engine rejects.
func TestVARIANTIsReadByFunctionOnStarRocks(t *testing.T) {
	t.Parallel()

	cat := testutil.NewMemoryCatalogProvide()
	cat.AddTable(&catalog.TableMeta{
		ID:      model.ObjectIdentifier{Name: "ev"},
		Columns: []catalog.ColumnMeta{{Name: "id", Type: "int"}, {Name: "v", Type: "variant"}},
	})

	relations, err := analyzeSQL("SELECT variant_query(v, '$.user.name') AS n FROM ev", cat)
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "v", relations[0].Source.Name)
	require.Equal(t, "n", relations[0].Target.Name)

	relations, err = analyzeSQL("SELECT get_variant(v, '$.user.name') AS n FROM ev", cat)
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "v", relations[0].Source.Name)

	relations, err = analyzeSQL("SELECT v.user.name AS n FROM ev", cat)
	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Empty(t, relations, "a dotted path is not a StarRocks VARIANT read")
}
