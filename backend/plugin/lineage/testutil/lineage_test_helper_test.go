package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

func TestLoadLineageTestSuiteFromYAML(t *testing.T) {
	tempDir := t.TempDir()
	suitePath := filepath.Join(tempDir, "suite.yaml")

	err := os.WriteFile(suitePath, []byte(`name: YAML loader smoke test
cases:
  - name: loads catalog and edge metadata
    sql: |
      SELECT id FROM users
    catalog:
      tables:
        users:
          - id
          - name
      schemas:
        public:
          orders:
            - order_id
    expected_edges:
      - from_table: users
        from_field: id
        to_table: __result__
        to_field: id
        relation_type: direct
        has_transform: false
        is_temp: true
    min_edges: 1
    skip_edge_validation: true
    debug: true
  - name: keeps optional edge assertions absent
    sql: SELECT 1
    expect_error: true
`), 0o600)
	require.NoError(t, err)

	suite, err := LoadLineageTestSuiteFromYAML(suitePath)
	require.NoError(t, err)
	require.Equal(t, "YAML loader smoke test", suite.Name)
	require.Len(t, suite.Cases, 2)

	first := suite.Cases[0]
	require.Equal(t, "loads catalog and edge metadata", first.Name)
	require.Equal(t, "SELECT id FROM users\n", first.SQL)
	require.NotNil(t, first.Catalog)
	require.NotNil(t, first.MinEdges)
	require.Equal(t, 1, *first.MinEdges)
	require.True(t, first.SkipEdgeValidation)
	require.True(t, first.Debug)
	require.Len(t, first.ExpectedEdges, 1)

	edge := first.ExpectedEdges[0]
	require.Equal(t, "users", edge.FromTable)
	require.Equal(t, "id", edge.FromField)
	require.Equal(t, "__result__", edge.ToTable)
	require.Equal(t, "id", edge.ToField)
	require.NotNil(t, edge.RelationType)
	require.Equal(t, model.RelationTypeDirect, *edge.RelationType)
	require.NotNil(t, edge.HasTransform)
	require.False(t, *edge.HasTransform)
	require.NotNil(t, edge.IsTemp)
	require.True(t, *edge.IsTemp)

	usersTable, err := first.Catalog.GetTable(context.Background(), model.ObjectIdentifier{Name: "users"})
	require.NoError(t, err)
	require.NotNil(t, usersTable)
	require.Len(t, usersTable.Columns, 2)
	require.Equal(t, "id", usersTable.Columns[0].Name)

	ordersTable, err := first.Catalog.GetTable(context.Background(), model.ObjectIdentifier{Schema: "public", Name: "orders"})
	require.NoError(t, err)
	require.NotNil(t, ordersTable)
	require.Len(t, ordersTable.Columns, 1)
	require.Equal(t, "order_id", ordersTable.Columns[0].Name)

	second := suite.Cases[1]
	require.True(t, second.ExpectError)
	require.Nil(t, second.ExpectedEdges)
}
