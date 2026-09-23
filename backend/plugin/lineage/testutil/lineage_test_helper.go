// Package testutil provides common test utilities for lineage analysis tests.
package testutil

import (
	"bytes"
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

	// Optional: expected transformations. Compared exactly — same count, each
	// entry matching a distinct produced transformation — unless the case sets
	// Subset, in which case each entry only has to match some transformation.
	Transformations []ExpectedTransformation

	// SubsetTransformations relaxes the transformation comparison to "each
	// expected entry matches some produced transformation". It is derived from
	// the case-level Subset flag.
	SubsetTransformations bool
}

// ExpectedTransformation matches a transformation on the fields it sets.
type ExpectedTransformation struct {
	Operation    string
	FunctionName string
	Expression   string
	OpType       string
	Condition    string
	Arguments    []string
	GroupKeys    []string
	PartitionBy  []string
	OrderBy      []string
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
	// When an empty (non-nil) slice, expects no edges at all. They are compared
	// exactly by default: every produced edge must match a distinct expected edge,
	// so an unexpected extra edge fails.
	ExpectedEdges []ExpectedEdge

	// Subset declares the case deliberately partial: expectations are matched as
	// subsets instead, and transformation expectations only have to be contained.
	// It is the escape hatch for a case that asserts part of a large result.
	Subset bool

	// ExpectError indicates the test expects an analysis error
	ExpectError bool
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
	Name          string              `yaml:"name"`
	SQL           string              `yaml:"sql"`
	Catalog       *yamlCatalog        `yaml:"catalog,omitempty"`
	ExpectedEdges *[]yamlExpectedEdge `yaml:"expected_edges,omitempty"`
	Subset        bool                `yaml:"subset,omitempty"`
	ExpectError   bool                `yaml:"expect_error,omitempty"`
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

	RelationType    *string              `yaml:"relation_type,omitempty"`
	HasTransform    *bool                `yaml:"has_transform,omitempty"`
	IsTemp          *bool                `yaml:"is_temp,omitempty"`
	Transformations []yamlTransformation `yaml:"transformations,omitempty"`
}

type yamlTransformation struct {
	Operation    string   `yaml:"operation,omitempty"`
	FunctionName string   `yaml:"function_name,omitempty"`
	Expression   string   `yaml:"expression,omitempty"`
	OpType       string   `yaml:"op_type,omitempty"`
	Condition    string   `yaml:"condition,omitempty"`
	Arguments    []string `yaml:"arguments,omitempty"`
	GroupKeys    []string `yaml:"group_keys,omitempty"`
	PartitionBy  []string `yaml:"partition_by,omitempty"`
	OrderBy      []string `yaml:"order_by,omitempty"`
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
	// KnownFields rejects a misspelled key instead of silently ignoring it: a typo
	// in exact_edges would otherwise downgrade a case to subset matching, and one
	// in a field of an expectation would turn it into a wildcard.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
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

// RunLineageTestSuitesFromYAMLDir executes every YAML suite in a directory.
func RunLineageTestSuitesFromYAMLDir(t *testing.T, dir string, analyzeFn AnalyzeFunc) {
	t.Helper()
	RunLineageTestSuitesFromYAMLDirSkipping(t, dir, analyzeFn, nil)
}

// RunLineageTestSuitesFromYAMLDirSkipping is RunLineageTestSuitesFromYAMLDir with
// the named cases omitted. A dialect uses it to record, explicitly, statements
// its parser cannot handle yet rather than silently narrowing the shared corpus.
func RunLineageTestSuitesFromYAMLDirSkipping(t *testing.T, dir string, analyzeFn AnalyzeFunc, skip map[string]bool) {
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

		cases := make([]LineageTestCase, 0, len(suite.Cases))
		for _, tc := range suite.Cases {
			if skip[tc.Name] {
				continue
			}
			cases = append(cases, tc)
		}

		t.Run(suite.Name, func(t *testing.T) {
			RunLineageTests(t, cases, analyzeFn)
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

	// An explicitly empty expectation means the statement must produce no edges.
	// Matching is exact unless the case declares itself partial with Subset.
	if tc.ExpectedEdges != nil {
		if tc.Subset {
			ValidateExpectedEdges(t, relations, tc.ExpectedEdges)
		} else {
			ValidateExactEdges(t, relations, tc.ExpectedEdges)
		}
	}
}

func (c *yamlLineageTestCase) toLineageTestCase() (LineageTestCase, error) {
	tc := LineageTestCase{
		Name:        c.Name,
		SQL:         c.SQL,
		ExpectError: c.ExpectError,
		Subset:      c.Subset,
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
			edge.SubsetTransformations = c.Subset
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

	for _, rawTransform := range e.Transformations {
		edge.Transformations = append(edge.Transformations, ExpectedTransformation(rawTransform))
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

// testingT is the subset of testing.TB the validators need, so a stub can be
// used to assert that a validator fails.
type testingT interface {
	require.TestingT
	Helper()
}

// ValidateExpectedEdges checks that all expected edges are found in the results.
func ValidateExpectedEdges(t testingT, relations []model.ColumnRelation, expected []ExpectedEdge) {
	t.Helper()

	if len(expected) == 0 {
		require.Empty(t, relations, "Expected no edges, got:\n%s", FormatRelations(relations))
		return
	}

	for _, exp := range expected {
		found := false
		for _, rel := range relations {
			if EdgeMatches(rel, exp) {
				found = true
				validateEdgeFields(t, rel, exp)
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

// ValidateExactEdges checks that the produced edges are exactly the expected
// ones: every expectation matches a distinct produced edge and no produced edge
// is left unmatched.
func ValidateExactEdges(t testingT, relations []model.ColumnRelation, expected []ExpectedEdge) {
	t.Helper()

	require.Len(t, relations, len(expected),
		"Edge count mismatch\nExpected edges: %s\nAvailable edges: %s",
		formatExpectedEdges(expected), FormatRelations(relations))

	used := make([]bool, len(relations))
	for _, exp := range expected {
		matched := -1
		for i, rel := range relations {
			if used[i] || !EdgeMatches(rel, exp) {
				continue
			}
			matched = i
			break
		}
		if matched < 0 {
			require.Failf(t, "expected edge not found",
				"Expected edge not found: %s.%s.%s.%s -> %s.%s.%s.%s\nAvailable edges: %s",
				exp.FromDatabase, exp.FromSchema, exp.FromTable, exp.FromField,
				exp.ToDatabase, exp.ToSchema, exp.ToTable, exp.ToField,
				FormatRelations(relations))
			return
		}
		used[matched] = true
		validateEdgeFields(t, relations[matched], exp)
	}
}

// validateEdgeFields checks the optional relation fields of a matched edge.
func validateEdgeFields(t testingT, rel model.ColumnRelation, exp ExpectedEdge) {
	t.Helper()

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

	if len(exp.Transformations) == 0 {
		return
	}

	if exp.SubsetTransformations {
		for _, expTransform := range exp.Transformations {
			require.True(t,
				slices.ContainsFunc(rel.Transformation, func(transform model.Transformation) bool {
					return TransformationMatches(transform, expTransform)
				}),
				"Expected transformation %+v not found for edge %s.%s -> %s.%s; got %+v",
				expTransform, exp.FromTable, exp.FromField, exp.ToTable, exp.ToField, rel.Transformation)
		}
		return
	}

	// Exact: the same number of transformations, each expectation matching a
	// distinct produced one, so an unexpected extra transformation fails too.
	require.Len(t, rel.Transformation, len(exp.Transformations),
		"Transformation count mismatch for edge %s.%s -> %s.%s; got %+v",
		exp.FromTable, exp.FromField, exp.ToTable, exp.ToField, rel.Transformation)
	used := make([]bool, len(rel.Transformation))
	for _, expTransform := range exp.Transformations {
		matched := -1
		for i, transform := range rel.Transformation {
			if !used[i] && TransformationMatches(transform, expTransform) {
				matched = i
				break
			}
		}
		if matched < 0 {
			require.Failf(t, "expected transformation not found",
				"Expected transformation %+v not found for edge %s.%s -> %s.%s; got %+v",
				expTransform, exp.FromTable, exp.FromField, exp.ToTable, exp.ToField, rel.Transformation)
			return
		}
		used[matched] = true
	}
}

// TransformationMatches checks a transformation against the fields an expected
// transformation sets.
func TransformationMatches(transform model.Transformation, exp ExpectedTransformation) bool {
	if exp.Operation != "" && transform.Operation != model.OperationType(exp.Operation) {
		return false
	}
	if exp.FunctionName != "" && transform.FunctionName != exp.FunctionName {
		return false
	}
	if exp.Expression != "" && transform.Expression != exp.Expression {
		return false
	}
	if exp.OpType != "" && transform.OpType != exp.OpType {
		return false
	}
	if exp.Condition != "" && transform.Condition != exp.Condition {
		return false
	}
	if len(exp.Arguments) > 0 && !slices.Equal(transform.Arguments, exp.Arguments) {
		return false
	}
	if len(exp.GroupKeys) > 0 && !slices.Equal(transform.GroupKeys, exp.GroupKeys) {
		return false
	}
	if len(exp.PartitionBy) > 0 && !slices.Equal(transform.PartitionBy, exp.PartitionBy) {
		return false
	}
	if len(exp.OrderBy) > 0 && !slices.Equal(transform.OrderBy, exp.OrderBy) {
		return false
	}
	return true
}

// formatExpectedEdges renders expected edges for failure messages.
func formatExpectedEdges(expected []ExpectedEdge) string {
	result := "\n"
	for _, e := range expected {
		result += fmt.Sprintf("  %s.%s.%s.%s -> %s.%s.%s.%s\n",
			e.FromDatabase, e.FromSchema, e.FromTable, e.FromField,
			e.ToDatabase, e.ToSchema, e.ToTable, e.ToField)
	}
	return result
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
