package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
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
      databases:
        db1:
          events:
            - event_id
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
            group_keys: []
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
	require.True(t, edge.Transformations[0].GroupKeysSet, "a written group_keys, even empty, is an assertion")
	require.Empty(t, edge.Transformations[0].GroupKeys)

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

	// A database-qualified entry is reachable the way the MySQL family addresses
	// a SQL qualifier, and is not visible under the schemas: form.
	eventsTable, err := first.Catalog.GetTable(context.Background(), model.ObjectIdentifier{Database: "db1", Name: "events"})
	require.NoError(t, err)
	require.NotNil(t, eventsTable)
	require.Len(t, eventsTable.Columns, 1)
	require.Equal(t, "event_id", eventsTable.Columns[0].Name)

	missingTable, err := first.Catalog.GetTable(context.Background(), model.ObjectIdentifier{Schema: "db1", Name: "events"})
	require.NoError(t, err)
	require.Nil(t, missingTable)

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

// An omitted column name matches anything, while a written one is exact — the
// distinction a row-level influence edge (empty target column) depends on.
func TestEdgeMatchesFieldSet(t *testing.T) {
	rowLevel := model.ColumnRelation{
		Source: model.Column{Table: model.ObjectIdentifier{Name: "orders"}, Name: "status"},
		Target: model.Column{Table: model.ObjectIdentifier{Name: "summary"}},
	}

	require.True(t, EdgeMatches(rowLevel, ExpectedEdge{FromTable: "orders"}),
		"an omitted to_field must not be checked")
	require.False(t, EdgeMatches(rowLevel, ExpectedEdge{FromTable: "orders", ToFieldSet: true, ToField: "status"}))
	require.True(t, EdgeMatches(rowLevel, ExpectedEdge{FromTable: "orders", ToFieldSet: true}))
	require.False(t, EdgeMatches(rowLevel, ExpectedEdge{FromTable: "orders", FromFieldSet: true, FromField: "other"}))
	require.True(t, EdgeMatches(rowLevel, ExpectedEdge{FromTable: "orders", FromFieldSet: true, FromField: "status"}))
}

func TestTransformationMatches(t *testing.T) {
	transform := model.NewWindowTransformation("SUM", "SUM(x)OVER()", []string{"a"}, []string{"b"})
	require.True(t, TransformationMatches(transform, ExpectedTransformation{Operation: "WINDOW", FunctionName: "SUM"}))
	require.True(t, TransformationMatches(transform, ExpectedTransformation{PartitionBy: []string{"a"}, OrderBy: []string{"b"}}))
	require.False(t, TransformationMatches(transform, ExpectedTransformation{PartitionBy: []string{"z"}}))
	require.False(t, TransformationMatches(transform, ExpectedTransformation{FunctionName: "AVG"}))
}

// An omitted group_keys asserts nothing, while a written one is exact — that
// distinction is the whole reason the field is a pointer in the YAML.
func TestTransformationMatchesGroupKeys(t *testing.T) {
	grouped := model.NewAggregateTransformation("COUNT", "COUNT(*)", []string{"a", "b"})
	ungrouped := model.NewAggregateTransformation("COUNT", "COUNT(*)", nil)

	require.True(t, TransformationMatches(grouped, ExpectedTransformation{Operation: "AGGREGATE"}),
		"an absent group_keys must not be checked")
	require.False(t, TransformationMatches(grouped, ExpectedTransformation{Operation: "AGGREGATE", GroupKeysSet: true}),
		"group_keys: [] must reject a transformation that carries keys")
	require.True(t, TransformationMatches(ungrouped, ExpectedTransformation{Operation: "AGGREGATE", GroupKeysSet: true}))
	require.True(t, TransformationMatches(grouped, ExpectedTransformation{GroupKeys: []string{"a", "b"}, GroupKeysSet: true}))
	require.False(t, TransformationMatches(grouped, ExpectedTransformation{GroupKeys: []string{"a"}, GroupKeysSet: true}),
		"the key order is part of the assertion")
}

func testRelation(fromField, toField string) model.ColumnRelation {
	return model.ColumnRelation{
		Source: model.Column{Table: model.ObjectIdentifier{Name: "users"}, Name: fromField},
		Target: model.Column{Table: model.ObjectIdentifier{Name: "__result__"}, Name: toField},
	}
}

// A misspelled key would otherwise be ignored, silently downgrading a case to
// subset matching or turning an expectation field into a wildcard.
func TestLoadLineageTestSuiteRejectsUnknownKey(t *testing.T) {
	tempDir := t.TempDir()
	suitePath := filepath.Join(tempDir, "typo.yaml")

	require.NoError(t, os.WriteFile(suitePath, []byte(`name: typo
cases:
  - name: misspelled subset
    sql: SELECT 1
    subsets: true
    expected_edges: []
`), 0o600))

	_, err := LoadLineageTestSuiteFromYAML(suitePath)
	require.Error(t, err, "a misspelled key must be rejected instead of silently ignored")
}

// Matching is exact by default, so `subset: true` is what keeps a deliberately
// partial case passing.
func TestRunLineageTestHonorsSubset(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "subset.yaml"), []byte(`name: subset
cases:
  - name: subset tolerates an extra edge
    sql: SELECT 1
    subset: true
    expected_edges:
      - from_table: users
        from_field: id
        to_table: __result__
        to_field: id
`), 0o600))

	suite, err := LoadLineageTestSuiteFromYAML(filepath.Join(tempDir, "subset.yaml"))
	require.NoError(t, err)
	require.True(t, suite.Cases[0].Subset)
	require.True(t, suite.Cases[0].ExpectedEdges[0].SubsetTransformations,
		"the case-level subset flag must relax transformation matching too")

	analyze := func(_ string, _ catalog.Provide) ([]model.ColumnRelation, error) {
		return []model.ColumnRelation{testRelation("id", "id"), testRelation("name", "name")}, nil
	}
	RunLineageTestSuitesFromYAMLDir(t, tempDir, analyze)
}

// Transformation expectations are exhaustive: an unlisted produced
// transformation fails, and so does an expectation with no produced match.
func TestValidateExactTransformations(t *testing.T) {
	relation := testRelation("name", "upper_name")
	relation.Transformation = []model.Transformation{
		model.NewProjectTransformation("name"),
		model.NewAggregateTransformation("SUM", "SUM(name)", nil),
	}
	base := ExpectedEdge{
		FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "upper_name",
	}

	exp := base
	exp.Transformations = []ExpectedTransformation{{Operation: "PROJECT", Expression: "name"}}
	ft := &fakeT{}
	ValidateExpectedEdges(ft, []model.ColumnRelation{relation}, []ExpectedEdge{exp})
	require.True(t, ft.failed, "an unlisted produced transformation must fail the exact comparison")

	exp = base
	exp.Transformations = []ExpectedTransformation{
		{Operation: "PROJECT", Expression: "name"},
		{Operation: "AGGREGATE", FunctionName: "SUM"},
		{Operation: "WINDOW"},
	}
	ft = &fakeT{}
	ValidateExpectedEdges(ft, []model.ColumnRelation{relation}, []ExpectedEdge{exp})
	require.True(t, ft.failed, "an expectation with no produced match must fail")

	exp = base
	exp.Transformations = []ExpectedTransformation{
		{Operation: "PROJECT", Expression: "name"},
		{Operation: "AGGREGATE", FunctionName: "SUM"},
	}
	ft = &fakeT{}
	ValidateExpectedEdges(ft, []model.ColumnRelation{relation}, []ExpectedEdge{exp})
	require.False(t, ft.failed, "an exact transformation set must pass")

	exp = base
	exp.SubsetTransformations = true
	exp.Transformations = []ExpectedTransformation{{Operation: "PROJECT", Expression: "name"}}
	ft = &fakeT{}
	ValidateExpectedEdges(ft, []model.ColumnRelation{relation}, []ExpectedEdge{exp})
	require.False(t, ft.failed, "a subset expectation must ignore unlisted transformations")
}

// The missing-expectation path must report a failure, not index the match list
// with -1 when the testing stub does not abort.
func TestValidateExactEdgesReportsMissingExpectation(t *testing.T) {
	relations := []model.ColumnRelation{testRelation("id", "id")}

	ft := &fakeT{}
	require.NotPanics(t, func() {
		ValidateExactEdges(ft, relations, []ExpectedEdge{
			{FromTable: "users", FromField: "id"},
			{FromTable: "users", FromField: "missing"},
		})
	})
	require.True(t, ft.failed)
}
