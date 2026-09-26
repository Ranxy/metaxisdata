package postgresql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// catalogProvideForCTE serves the one relation the statements below read, so the
// first CTE's own lineage resolves to real columns instead of a wildcard.
type catalogProvideForCTE struct{}

func (catalogProvideForCTE) GetTable(_ context.Context, id model.ObjectIdentifier) (*catalog.TableMeta, error) {
	if id.FullName() != "e2e_dwd.v_customer_360" {
		return nil, nil
	}
	columns := []catalog.ColumnMeta{}
	for _, name := range []string{"customer_id", "customer_segment", "country", "region", "lifetime_net", "order_count", "is_vip"} {
		columns = append(columns, catalog.ColumnMeta{Name: name})
	}
	return &catalog.TableMeta{ID: id, Columns: columns}, nil
}

// A `COUNT(*)` inside a CTE that reads another CTE has no column of its own to
// attribute itself to, so the analyzer attributes it to the relation it reads.
// When that relation is a CTE, the edge must be traced through to the relation
// the CTE read: an edge naming the CTE points at an object that exists only in
// the statement, which the ingestion then stores as a phantom table.
func TestWildcardAggregateOverACTEDoesNotNameTheCTE(t *testing.T) {
	relations, err := analyzeSQL(`INSERT INTO e2e_ads.ads_customer_segment (customer_segment, country, region, customer_count)
WITH base AS (
  SELECT c.customer_id, c.customer_segment, c.country, c.region, c.lifetime_net, c.order_count, c.is_vip
  FROM e2e_dwd.v_customer_360 c
),
agg AS (
  SELECT b.customer_segment, b.country, b.region, count(*) AS customer_count
  FROM base b
  GROUP BY b.customer_segment, b.country, b.region
)
SELECT a.customer_segment, a.country, a.region, a.customer_count
FROM agg a;`, catalogProvideForCTE{})
	require.NoError(t, err)

	var counted []model.ColumnRelation
	for _, relation := range relations {
		require.NotEqual(t, "base", relation.Source.Table.Name,
			"a wildcard aggregate must not name the CTE it read as a relation")
		require.NotEqual(t, "agg", relation.Source.Table.Name)
		if relation.Target.Name == "customer_count" {
			counted = append(counted, relation)
		}
	}

	require.NotEmpty(t, counted, "count(*) must still be recorded")
	for _, relation := range counted {
		require.Equal(t, "e2e_dwd", relation.Source.Table.Schema)
		require.Equal(t, "v_customer_360", relation.Source.Table.Name)
		require.Equal(t, model.RelationTypeGroup, relation.RelationType)
	}
}
