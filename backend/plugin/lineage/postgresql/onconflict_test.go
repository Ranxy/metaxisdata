package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestOnConflictExcludedEdgesDropped pins that ON CONFLICT DO UPDATE SET does not
// emit edges sourced from the EXCLUDED pseudo-relation. EXCLUDED is not a
// metadata-registry object, so such an edge can never resolve; the real lineage
// already comes from the INSERT source. Genuine target self-references (a
// read-modify-write on the target table) are preserved.
func TestOnConflictExcludedEdgesDropped(t *testing.T) {
	relations, err := analyzeSQL(`INSERT INTO inventory (product_id, quantity)
		SELECT product_id, quantity FROM shipment
		ON CONFLICT (product_id) DO UPDATE SET quantity = inventory.quantity + EXCLUDED.quantity`, nil)
	require.NoError(t, err)

	for _, rel := range relations {
		require.NotEqual(t, excludedRelationName, rel.Source.Table.Name,
			"EXCLUDED must not be a lineage source:\n%s", testutil.FormatRelations(relations))
	}

	require.True(t, hasTableEdge(relations, "shipment", "quantity", "inventory", "quantity"),
		"the INSERT-source edge must remain:\n%s", testutil.FormatRelations(relations))
	require.True(t, hasTableEdge(relations, "inventory", "quantity", "inventory", "quantity"),
		"the target self-reference must remain:\n%s", testutil.FormatRelations(relations))
}

func hasTableEdge(relations []model.ColumnRelation, sourceTable, sourceColumn, targetTable, targetColumn string) bool {
	for _, rel := range relations {
		if rel.Source.Table.Name == sourceTable && rel.Source.Name == sourceColumn &&
			rel.Target.Table.Name == targetTable && rel.Target.Name == targetColumn {
			return true
		}
	}
	return false
}
