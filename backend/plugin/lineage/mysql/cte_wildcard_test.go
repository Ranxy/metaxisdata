package mysql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// catalogProvideForCTE serves the one relation the statement below reads, so the
// first CTE's own lineage resolves to real columns instead of a wildcard.
type catalogProvideForCTE struct{}

func (catalogProvideForCTE) GetTable(_ context.Context, id model.ObjectIdentifier) (*catalog.TableMeta, error) {
	if id.Name != "orders" {
		return nil, nil
	}
	return &catalog.TableMeta{ID: id, Columns: []catalog.ColumnMeta{{Name: "user_id"}, {Name: "amount"}}}, nil
}

// A `COUNT(*)` inside a CTE that reads another CTE has no column of its own to
// attribute itself to, so the analyzer attributes it to the relation it reads.
// When that relation is a CTE, the edge must be traced through to the relation
// the CTE read: an edge naming the CTE points at an object that exists only in
// the statement, which ingestion then stores as a phantom table (see
// test/e2e_etl/FINDINGS.md F8, where PostgreSQL had the same defect).
func TestWildcardAggregateOverACTEDoesNotNameTheCTE(t *testing.T) {
	relations, err := analyzeSQL(`INSERT INTO summary (user_id, total)
WITH base AS (
  SELECT user_id, amount FROM orders
),
agg AS (
  SELECT b.user_id, count(*) AS total FROM base b GROUP BY b.user_id
)
SELECT a.user_id, a.total FROM agg a`, catalogProvideForCTE{})
	require.NoError(t, err)

	var counted []model.ColumnRelation
	for _, relation := range relations {
		require.NotEqual(t, "base", relation.Source.Table.Name,
			"a wildcard aggregate must not name the CTE it read as a relation")
		require.NotEqual(t, "agg", relation.Source.Table.Name)
		if relation.Target.Name == "total" {
			counted = append(counted, relation)
		}
	}

	require.Len(t, counted, 2, "count(*) reads both columns the CTE exposes")
	for _, relation := range counted {
		require.Equal(t, "orders", relation.Source.Table.Name)
		require.Equal(t, model.RelationTypeGroup, relation.RelationType)
	}
}
