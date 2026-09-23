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
    exact_edges: true
    expected_edges:
      - from_table: users
        from_field: id
        to_table: __result__
        to_field: id
        relation_type: direct
        has_transform: false
        is_temp: true
        transformations:
          - operation: PROJECT
            expression: id
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
	require.True(t, first.ExactEdges)
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
	require.Len(t, edge.Transformations, 1)
	require.Equal(t, "PROJECT", edge.Transformations[0].Operation)
	require.Equal(t, "id", edge.Transformations[0].Expression)

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

// fakeT is a testing.TB stub that records assertion failures instead of failing.
type fakeT struct {
	failed bool
	msg    string
}

func (f *fakeT) Errorf(format string, _ ...any) {
	f.failed = true
	f.msg = format
}

func (*fakeT) FailNow() {}

func (*fakeT) Helper() {}

func TestValidateExpectedEdgesEmptyMeansNoEdges(t *testing.T) {
	relations := []model.ColumnRelation{testRelation("id", "id")}

	ft := &fakeT{}
	ValidateExpectedEdges(ft, relations, []ExpectedEdge{})
	require.True(t, ft.failed, "an empty expectation must fail when edges were produced")

	ft = &fakeT{}
	ValidateExpectedEdges(ft, nil, []ExpectedEdge{})
	require.False(t, ft.failed, "an empty expectation must pass when no edges were produced")
}

func TestValidateExactEdgesRejectsExtraEdge(t *testing.T) {
	relations := []model.ColumnRelation{
		testRelation("id", "id"),
		testRelation("name", "name"),
	}

	ft := &fakeT{}
	ValidateExactEdges(ft, relations, []ExpectedEdge{{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"}})
	require.True(t, ft.failed, "an unexpected extra edge must fail the exact check")

	ft = &fakeT{}
	ValidateExactEdges(ft, relations, []ExpectedEdge{
		{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
		{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
	})
	require.False(t, ft.failed, "matching edges must pass the exact check")
}

func TestValidateExpectedEdgesChecksTransformations(t *testing.T) {
	relation := testRelation("name", "upper_name")
	relation.Transformation = []model.Transformation{model.NewProjectTransformation("name")}

	ft := &fakeT{}
	ValidateExpectedEdges(ft, []model.ColumnRelation{relation}, []ExpectedEdge{{
		FromTable:       "users",
		FromField:       "name",
		ToTable:         "__result__",
		ToField:         "upper_name",
		Transformations: []ExpectedTransformation{{Operation: "OPERATOR"}},
	}})
	require.True(t, ft.failed, "a mismatched transformation kind must fail")

	ft = &fakeT{}
	ValidateExpectedEdges(ft, []model.ColumnRelation{relation}, []ExpectedEdge{{
		FromTable:       "users",
		FromField:       "name",
		ToTable:         "__result__",
		ToField:         "upper_name",
		Transformations: []ExpectedTransformation{{Operation: "PROJECT", Expression: "name"}},
	}})
	require.False(t, ft.failed, "a matching transformation must pass")
}

func TestTransformationMatches(t *testing.T) {
	transform := model.NewWindowTransformation("SUM", "SUM(x)OVER()", []string{"a"}, []string{"b"})
	require.True(t, TransformationMatches(transform, ExpectedTransformation{Operation: "WINDOW", FunctionName: "SUM"}))
	require.True(t, TransformationMatches(transform, ExpectedTransformation{PartitionBy: []string{"a"}, OrderBy: []string{"b"}}))
	require.False(t, TransformationMatches(transform, ExpectedTransformation{PartitionBy: []string{"z"}}))
	require.False(t, TransformationMatches(transform, ExpectedTransformation{FunctionName: "AVG"}))
}

func testRelation(fromField, toField string) model.ColumnRelation {
	return model.ColumnRelation{
		Source: model.Column{Table: model.ObjectIdentifier{Name: "users"}, Name: fromField},
		Target: model.Column{Table: model.ObjectIdentifier{Name: "__result__"}, Name: toField},
	}
}
