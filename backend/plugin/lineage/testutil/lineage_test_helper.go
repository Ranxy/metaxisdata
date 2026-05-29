// Package testutil provides common test utilities for lineage analysis tests.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// ExpectedEdge defines the expected lineage edge for testing.
// This structure allows for flexible matching - nil fields are not validated.
type ExpectedEdge struct {
	// Source column information
	FromDatabase string
	FromSchema   string
	FromTable    string
	FromField    string

	// Target column information
	ToDatabase string
	ToSchema   string
	ToTable    string
	ToField    string

	// Optional: expected relation type (nil = not checked)
	RelationType *model.RelationType

	// Optional: whether transformation is expected (nil = not checked)
	HasTransform *bool

	// Optional: whether target is temporary (nil = not checked)
	IsTemp *bool
}

// LineageTestCase defines a test case for lineage analysis.
type LineageTestCase struct {
	// Name of the test case
	Name string

	// SQL query to analyze
	SQL string

	// Optional catalog provider
	Catalog catalog.Provide

	// Expected edges. When nil, only checks that analysis succeeds without error.
	// When empty slice, expects no edges.
	ExpectedEdges []ExpectedEdge

	// ExpectError indicates the test expects an analysis error
	ExpectError bool

	// MinEdges specifies minimum expected edges count (used when ExpectedEdges is nil)
	MinEdges *int

	// SkipEdgeValidation skips detailed edge matching but ensures MinEdges is satisfied
	SkipEdgeValidation bool

	// Debug enables verbose output for debugging
	Debug bool
}

// LineageTestSuite defines a named group of lineage test cases loaded from YAML.
type LineageTestSuite struct {
	Name  string
	Cases []LineageTestCase
}

type yamlLineageTestSuite struct {
	Name  string                `yaml:"name"`
	Cases []yamlLineageTestCase `yaml:"cases"`
}

type yamlLineageTestCase struct {
	Name               string              `yaml:"name"`
	SQL                string              `yaml:"sql"`
	Catalog            *yamlCatalog        `yaml:"catalog,omitempty"`
	ExpectedEdges      *[]yamlExpectedEdge `yaml:"expected_edges,omitempty"`
	ExpectError        bool                `yaml:"expect_error,omitempty"`
	MinEdges           *int                `yaml:"min_edges,omitempty"`
	SkipEdgeValidation bool                `yaml:"skip_edge_validation,omitempty"`
	Debug              bool                `yaml:"debug,omitempty"`
}

type yamlCatalog struct {
	Tables  map[string][]string            `yaml:"tables,omitempty"`
	Schemas map[string]map[string][]string `yaml:"schemas,omitempty"`
}

type yamlExpectedEdge struct {
	FromDatabase string `yaml:"from_database,omitempty"`
	FromSchema   string `yaml:"from_schema,omitempty"`
	FromTable    string `yaml:"from_table,omitempty"`
	FromField    string `yaml:"from_field,omitempty"`

	ToDatabase string `yaml:"to_database,omitempty"`
	ToSchema   string `yaml:"to_schema,omitempty"`
	ToTable    string `yaml:"to_table,omitempty"`
	ToField    string `yaml:"to_field,omitempty"`

	RelationType *string `yaml:"relation_type,omitempty"`
	HasTransform *bool   `yaml:"has_transform,omitempty"`
	IsTemp       *bool   `yaml:"is_temp,omitempty"`
}

// Bool returns a pointer to a bool value for use in ExpectedEdge.
func Bool(v bool) *bool {
	return &v
}

// Int returns a pointer to an int value for use in LineageTestCase.MinEdges.
func Int(v int) *int {
	return &v
}

// RelType returns a pointer to a RelationType value for use in ExpectedEdge.
func RelType(v model.RelationType) *model.RelationType {
	return &v
}

// AnalyzeFunc is the function signature for analyzing SQL and returning relations.
type AnalyzeFunc func(sql string, cat catalog.Provide) ([]model.ColumnRelation, error)

// LoadLineageTestSuiteFromYAML loads a lineage test suite definition from a YAML file.
func LoadLineageTestSuiteFromYAML(path string) (LineageTestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LineageTestSuite{}, fmt.Errorf("failed to read lineage test suite %q: %w", path, err)
	}

	var raw yamlLineageTestSuite
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return LineageTestSuite{}, fmt.Errorf("failed to unmarshal lineage test suite %q: %w", path, err)
	}

	suiteName := raw.Name
	if suiteName == "" {
		suiteName = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	cases := make([]LineageTestCase, 0, len(raw.Cases))
	for idx, rawCase := range raw.Cases {
		tc, err := rawCase.toLineageTestCase()
		if err != nil {
			return LineageTestSuite{}, fmt.Errorf("failed to convert case %d from %q: %w", idx, path, err)
		}
		cases = append(cases, tc)
	}

	return LineageTestSuite{
		Name:  suiteName,
		Cases: cases,
	}, nil
}

// RunLineageTestsFromYAML executes all cases defined in a YAML suite file.
func RunLineageTestsFromYAML(t *testing.T, path string, analyzeFn AnalyzeFunc) {
	t.Helper()

	suite, err := LoadLineageTestSuiteFromYAML(path)
	require.NoError(t, err)

	RunLineageTests(t, suite.Cases, analyzeFn)
}

// RunLineageTestSuitesFromYAMLDir executes every YAML suite in a directory.
func RunLineageTestSuitesFromYAMLDir(t *testing.T, dir string, analyzeFn AnalyzeFunc) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	suitePaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		suitePaths = append(suitePaths, filepath.Join(dir, entry.Name()))
	}
	slices.Sort(suitePaths)
	require.NotEmpty(t, suitePaths, "no YAML lineage test suites found in %s", dir)

	for _, suitePath := range suitePaths {
		suite, err := LoadLineageTestSuiteFromYAML(suitePath)
		require.NoError(t, err)

		t.Run(suite.Name, func(t *testing.T) {
			RunLineageTests(t, suite.Cases, analyzeFn)
		})
	}
}

// RunLineageTests executes a slice of lineage test cases using the provided analyze function.
func RunLineageTests(t *testing.T, testCases []LineageTestCase, analyzeFn AnalyzeFunc) {
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			RunLineageTest(t, tc, analyzeFn)
		})
	}
}

// RunLineageTest executes a single lineage test case using the provided analyze function.
func RunLineageTest(t *testing.T, tc LineageTestCase, analyzeFn AnalyzeFunc) {
	t.Helper()

	relations, err := analyzeFn(tc.SQL, tc.Catalog)

	// Handle expected errors
	if tc.ExpectError {
		require.Error(t, err, "Expected analysis to fail for SQL: %s", tc.SQL)
		return
	}
	require.NoError(t, err, "Failed to analyze SQL: %s", tc.SQL)

	// Debug output
	if tc.Debug {
		fmt.Printf("\n%s edges (%d):\n", tc.Name, len(relations))
		for _, r := range relations {
			fmt.Printf("  %s -> %s (type: %v, isTemp: %v, transform: %v)\n",
				r.Source.Table.FullName()+"."+r.Source.Name,
				r.Target.Table.FullName()+"."+r.Target.Name,
				r.RelationType, r.IsTemp, r.Transformation != nil)
		}
	}

	// Check minimum edges count
	if tc.MinEdges != nil {
		require.GreaterOrEqual(t, len(relations), *tc.MinEdges,
			"Expected at least %d edges, got %d", *tc.MinEdges, len(relations))
	}

	// Skip detailed validation if requested
	if tc.SkipEdgeValidation {
		return
	}

	// Validate expected edges
	if tc.ExpectedEdges != nil {
		ValidateExpectedEdges(t, relations, tc.ExpectedEdges)
	}
}

func (c *yamlLineageTestCase) toLineageTestCase() (LineageTestCase, error) {
	tc := LineageTestCase{
		Name:               c.Name,
		SQL:                c.SQL,
		ExpectError:        c.ExpectError,
		MinEdges:           c.MinEdges,
		SkipEdgeValidation: c.SkipEdgeValidation,
		Debug:              c.Debug,
	}

	if c.Catalog != nil {
		tc.Catalog = c.Catalog.toCatalog()
	}

	if c.ExpectedEdges != nil {
		tc.ExpectedEdges = make([]ExpectedEdge, 0, len(*c.ExpectedEdges))
		for _, rawEdge := range *c.ExpectedEdges {
			edge, err := rawEdge.toExpectedEdge()
			if err != nil {
				return LineageTestCase{}, errors.Wrap(err, "failed to convert expected edge")
			}
			tc.ExpectedEdges = append(tc.ExpectedEdges, edge)
		}
	}

	return tc, nil
}

func (c *yamlCatalog) toCatalog() catalog.Provide {
	if c == nil {
		return nil
	}

	cat := catalog.NewMemoryCatalogProvide()
	for tableName, columns := range c.Tables {
		addCatalogTable(cat, model.ObjectIdentifier{Name: tableName}, columns)
	}
	for schemaName, tables := range c.Schemas {
		for tableName, columns := range tables {
			addCatalogTable(cat, model.ObjectIdentifier{Schema: schemaName, Name: tableName}, columns)
		}
	}

	return cat
}

func (e *yamlExpectedEdge) toExpectedEdge() (ExpectedEdge, error) {
	edge := ExpectedEdge{
		FromDatabase: e.FromDatabase,
		FromSchema:   e.FromSchema,
		FromTable:    e.FromTable,
		FromField:    e.FromField,
		ToDatabase:   e.ToDatabase,
		ToSchema:     e.ToSchema,
		ToTable:      e.ToTable,
		ToField:      e.ToField,
		HasTransform: e.HasTransform,
		IsTemp:       e.IsTemp,
	}

	if e.RelationType != nil {
		relationType, err := parseRelationType(*e.RelationType)
		if err != nil {
			return ExpectedEdge{}, err
		}
		edge.RelationType = &relationType
	}

	return edge, nil
}

func parseRelationType(name string) (model.RelationType, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "relationtype")
	normalized = strings.TrimPrefix(normalized, "relation_type_")
	normalized = strings.TrimPrefix(normalized, "relation_type")
	normalized = strings.TrimPrefix(normalized, "type_")
	normalized = strings.TrimPrefix(normalized, "_")

	switch normalized {
	case "direct":
		return model.RelationTypeDirect, nil
	case "indirect":
		return model.RelationTypeIndirect, nil
	case "join":
		return model.RelationTypeJoin, nil
	case "group":
		return model.RelationTypeGroup, nil
	case "union":
		return model.RelationTypeUnion, nil
	case "intersect":
		return model.RelationTypeIntersect, nil
	case "except":
		return model.RelationTypeExcept, nil
	case "unknown":
		return model.RelationTypeUnknown, nil
	default:
		return 0, fmt.Errorf("unknown relation type %q", name)
	}
}

func addCatalogTable(cat *catalog.MemoryCatalogProvide, id model.ObjectIdentifier, columns []string) {
	metaColumns := make([]catalog.ColumnMeta, len(columns))
	for idx, columnName := range columns {
		metaColumns[idx] = catalog.ColumnMeta{Name: columnName}
	}

	cat.AddTable(&catalog.TableMeta{
		ID:      id,
		Columns: metaColumns,
	})
}

// ValidateExpectedEdges checks that all expected edges are found in the results.
func ValidateExpectedEdges(t *testing.T, relations []model.ColumnRelation, expected []ExpectedEdge) {
	t.Helper()

	for _, exp := range expected {
		found := false
		for _, rel := range relations {
			if EdgeMatches(rel, exp) {
				found = true

				// Validate optional fields
				if exp.RelationType != nil {
					require.Equal(t, *exp.RelationType, rel.RelationType,
						"Relation type mismatch for edge %s.%s -> %s.%s",
						exp.FromTable, exp.FromField, exp.ToTable, exp.ToField)
				}

				if exp.HasTransform != nil {
					hasTransform := len(rel.Transformation) > 0
					require.Equal(t, *exp.HasTransform, hasTransform,
						"Transform expectation mismatch for edge %s.%s -> %s.%s: expected HasTransform=%v, got %v",
						exp.FromTable, exp.FromField, exp.ToTable, exp.ToField, *exp.HasTransform, hasTransform)
				}

				if exp.IsTemp != nil {
					require.Equal(t, *exp.IsTemp, rel.IsTemp,
						"IsTemp mismatch for edge %s.%s -> %s.%s",
						exp.FromTable, exp.FromField, exp.ToTable, exp.ToField)
				}

				break
			}
		}

		require.True(t, found,
			"Expected edge not found: %s.%s.%s.%s -> %s.%s.%s.%s\nAvailable edges: %s",
			exp.FromDatabase, exp.FromSchema, exp.FromTable, exp.FromField,
			exp.ToDatabase, exp.ToSchema, exp.ToTable, exp.ToField,
			FormatRelations(relations))
	}
}

// EdgeMatches checks if a relation matches the expected edge pattern.
// Empty strings in the expected edge are treated as wildcards that match anything.
func EdgeMatches(rel model.ColumnRelation, exp ExpectedEdge) bool {
	// Match source
	if exp.FromDatabase != "" && rel.Source.Table.Database != exp.FromDatabase {
		return false
	}
	if exp.FromSchema != "" && rel.Source.Table.Schema != exp.FromSchema {
		return false
	}
	if exp.FromTable != "" && rel.Source.Table.Name != exp.FromTable {
		return false
	}
	if exp.FromField != "" && rel.Source.Name != exp.FromField {
		return false
	}

	// Match target
	if exp.ToDatabase != "" && rel.Target.Table.Database != exp.ToDatabase {
		return false
	}
	if exp.ToSchema != "" && rel.Target.Table.Schema != exp.ToSchema {
		return false
	}
	if exp.ToTable != "" && rel.Target.Table.Name != exp.ToTable {
		return false
	}
	if exp.ToField != "" && rel.Target.Name != exp.ToField {
		return false
	}

	return true
}

// FormatRelations formats relations for error messages.
func FormatRelations(relations []model.ColumnRelation) string {
	result := "\n"
	for _, r := range relations {
		result += fmt.Sprintf("  %s -> %s\n",
			r.Source.Table.FullName()+"."+r.Source.Name,
			r.Target.Table.FullName()+"."+r.Target.Name)
	}
	return result
}

// AssertEdgeCount verifies the exact number of edges returned.
func AssertEdgeCount(t *testing.T, relations []model.ColumnRelation, expected int) {
	t.Helper()
	require.Len(t, relations, expected, "Expected %d edges, got %d", expected, len(relations))
}

// AssertEdgeExists checks if a specific edge exists in the relations.
func AssertEdgeExists(t *testing.T, relations []model.ColumnRelation, fromTable, fromField, toTable, toField string) {
	t.Helper()

	for _, rel := range relations {
		if rel.Source.Table.Name == fromTable &&
			rel.Source.Name == fromField &&
			rel.Target.Table.Name == toTable &&
			rel.Target.Name == toField {
			return
		}
	}

	require.Fail(t, "Edge not found",
		"Expected edge %s.%s -> %s.%s not found\nAvailable edges: %s",
		fromTable, fromField, toTable, toField, FormatRelations(relations))
}

// AssertNoEdgeFromTable checks that no edges come from the specified table.
func AssertNoEdgeFromTable(t *testing.T, relations []model.ColumnRelation, tableName string) {
	t.Helper()

	for _, rel := range relations {
		if rel.Source.Table.Name == tableName {
			require.Fail(t, "Unexpected edge",
				"Found unexpected edge from table %s: %s.%s -> %s.%s",
				tableName, rel.Source.Table.Name, rel.Source.Name,
				rel.Target.Table.Name, rel.Target.Name)
		}
	}
}

// AssertAllEdgesToTable checks that all edges target the specified table.
func AssertAllEdgesToTable(t *testing.T, relations []model.ColumnRelation, tableName string) {
	t.Helper()

	for _, rel := range relations {
		require.Equal(t, tableName, rel.Target.Table.Name,
			"Expected all edges to target %s, but found edge to %s",
			tableName, rel.Target.Table.Name)
	}
}

// CreateSimpleCatalog creates a simple catalog with the specified tables and columns.
// This is a convenience function for tests that need catalog support.
func CreateSimpleCatalog(tables map[string][]string) catalog.Provide {
	cat := catalog.NewMemoryCatalogProvide()

	for tableName, columns := range tables {
		addCatalogTable(cat, model.ObjectIdentifier{Name: tableName}, columns)
	}

	return cat
}

// CreateCatalogWithSchema creates a catalog with schema-qualified tables.
func CreateCatalogWithSchema(tables map[string]map[string][]string) catalog.Provide {
	cat := catalog.NewMemoryCatalogProvide()

	for schema, schemaTables := range tables {
		for tableName, columns := range schemaTables {
			addCatalogTable(cat, model.ObjectIdentifier{Schema: schema, Name: tableName}, columns)
		}
	}

	return cat
}
