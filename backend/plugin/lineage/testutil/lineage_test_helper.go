// Package testutil provides common test utilities for lineage analysis tests.
package testutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

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
		cols := make([]catalog.ColumnMeta, len(columns))
		for i, col := range columns {
			cols[i] = catalog.ColumnMeta{Name: col}
		}

		table := &catalog.TableMeta{
			ID: model.ObjectIdentifier{
				Name: tableName,
			},
			Columns: cols,
		}
		cat.AddTable(table)
	}

	return cat
}

// CreateCatalogWithSchema creates a catalog with schema-qualified tables.
func CreateCatalogWithSchema(tables map[string]map[string][]string) catalog.Provide {
	cat := catalog.NewMemoryCatalogProvide()

	for schema, schemaTables := range tables {
		for tableName, columns := range schemaTables {
			cols := make([]catalog.ColumnMeta, len(columns))
			for i, col := range columns {
				cols[i] = catalog.ColumnMeta{Name: col}
			}

			table := &catalog.TableMeta{
				ID: model.ObjectIdentifier{
					Schema: schema,
					Name:   tableName,
				},
				Columns: cols,
			}
			cat.AddTable(table)
		}
	}

	return cat
}
