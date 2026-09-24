package testutil

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// IsPartialAnalysis reports whether an error is the one a partial analysis
// returns: the analyzer kept the edges it found and reported what it could not
// represent. A benchmark runs corpus cases that expect an error beside their
// edges, so it treats that error as the case's documented outcome while any other
// error still fails it.
func IsPartialAnalysis(err error) bool {
	if err == nil {
		return true
	}
	var partial *lineage.UnsupportedStatementError
	return errors.As(err, &partial)
}

// BenchCase is one statement a benchmark measures.
type BenchCase struct {
	Name string
	SQL  string
}

// LoadCorpusBenchCases reads every statement in the YAML suites under dir that is
// expected to analyze successfully, so the same corpus that guards correctness
// also measures cost. An error case never reaches the walk a benchmark measures:
// it stops at the statement the analyzer rejects.
func LoadCorpusBenchCases(tb testing.TB, dir string) []BenchCase {
	tb.Helper()
	files, err := FindLineageTestSuites(dir)
	if err != nil {
		tb.Fatalf("read testdata %s: %v", dir, err)
	}
	slices.Sort(files)

	var cases []BenchCase
	for _, file := range files {
		suite, err := LoadLineageTestSuiteFromYAML(file)
		if err != nil {
			tb.Fatalf("load %s: %v", file, err)
		}
		for _, tc := range suite.Cases {
			if tc.ExpectError && tc.ExpectedEdges == nil {
				continue
			}
			cases = append(cases, BenchCase{Name: BenchCaseName(tc.Name), SQL: tc.SQL})
		}
	}
	if len(cases) == 0 {
		tb.Fatalf("no corpus cases found under %s", dir)
	}
	return cases
}

// BenchCaseName turns a corpus case name into a benchmark sub-name.
func BenchCaseName(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	return strings.ReplaceAll(s, "/", "_")
}

// BenchmarkCorpusAnalysis measures end-to-end analysis over a whole corpus in one
// operation, giving a single throughput number for the analyzer.
func BenchmarkCorpusAnalysis(b *testing.B, dir string, analyze AnalyzeFunc) {
	b.Helper()
	cases := LoadCorpusBenchCases(b, dir)
	b.ReportAllocs()
	for b.Loop() {
		for _, c := range cases {
			if _, err := analyze(c.SQL, nil); !IsPartialAnalysis(err) {
				b.Fatalf("case %q: %v", c.Name, err)
			}
		}
	}
}

// BenchmarkCaseAnalysis measures each corpus statement separately so the slow
// shapes are visible.
func BenchmarkCaseAnalysis(b *testing.B, dir string, analyze AnalyzeFunc) {
	b.Helper()
	for _, c := range LoadCorpusBenchCases(b, dir) {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := analyze(c.SQL, nil); !IsPartialAnalysis(err) {
					b.Fatalf("case %q: %v", c.Name, err)
				}
			}
		})
	}
}

// BenchmarkCaseParse isolates the parse cost from the lineage walk, so the
// analyzer's own overhead is the difference between the two.
func BenchmarkCaseParse(b *testing.B, dir string, parse func(string) error) {
	b.Helper()
	for _, c := range LoadCorpusBenchCases(b, dir) {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := parse(c.SQL); err != nil {
					b.Fatalf("case %q: %v", c.Name, err)
				}
			}
		})
	}
}

// BenchmarkShapes measures synthetic statements that scale a single dimension
// (column count, nesting depth, join count) to show how analysis cost grows.
func BenchmarkShapes(b *testing.B, analyze AnalyzeFunc) {
	b.Helper()
	shapes := []BenchCase{
		{"simple_select", "SELECT a, b, c FROM t WHERE a > 1"},
		{"join_3", "SELECT a.x, b.y, c.z FROM a JOIN b ON a.id = b.aid JOIN c ON b.id = c.bid"},
		{"cte_union_subquery", "WITH x AS (SELECT id, name FROM a), y AS (SELECT id FROM b) " +
			"SELECT name FROM x WHERE id IN (SELECT id FROM y) UNION SELECT name FROM c"},
		{"window_aggregate", "SELECT dept, SUM(salary) OVER (PARTITION BY dept ORDER BY hired) AS running FROM emp"},
		{"insert_select", "INSERT INTO dst (a, b) SELECT id, name FROM src"},
		{"ctas_view_expr", "CREATE VIEW v AS SELECT a.id, a.name, b.val * 2 AS doubled FROM a JOIN b ON a.id = b.id"},
		{"wide_100_cols", BuildWideSelect(100)},
		{"deep_subquery_10", BuildDeepSubquery(10)},
		{"many_joins_10", BuildManyJoins(10)},
	}
	for _, c := range shapes {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := analyze(c.SQL, nil); err != nil {
					b.Fatalf("case %q: %v", c.Name, err)
				}
			}
		})
	}
}

// BenchmarkWildcardCatalog measures SELECT * expansion against a 100-column
// catalog, the production path where the metadata store is consulted.
func BenchmarkWildcardCatalog(b *testing.B, analyze AnalyzeFunc) {
	b.Helper()
	columns := make([]catalog.ColumnMeta, 100)
	for i := range columns {
		columns[i] = catalog.ColumnMeta{Name: fmt.Sprintf("c%d", i)}
	}
	provide := catalog.NewMemoryCatalogProvide()
	provide.AddTable(&catalog.TableMeta{
		ID:      model.ObjectIdentifier{Name: "t"},
		Columns: columns,
	})
	sql := "SELECT * FROM t"

	b.Run("with_catalog", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := analyze(sql, provide); err != nil {
				b.Fatalf("with catalog: %v", err)
			}
		}
	})
	b.Run("without_catalog", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := analyze(sql, nil); err != nil {
				b.Fatalf("without catalog: %v", err)
			}
		}
	})
}

// BuildWideSelect builds a select of n projected columns.
func BuildWideSelect(n int) string {
	columns := make([]string, n)
	for i := range columns {
		columns[i] = fmt.Sprintf("c%d", i)
	}
	return "SELECT " + strings.Join(columns, ", ") + " FROM t"
}

// BuildDeepSubquery builds n nested derived tables.
func BuildDeepSubquery(n int) string {
	query := "SELECT c FROM t0"
	for i := 1; i <= n; i++ {
		query = fmt.Sprintf("SELECT s%d.c FROM (%s) s%d", i, query, i)
	}
	return query
}

// BuildManyJoins builds a join chain over n tables.
func BuildManyJoins(n int) string {
	var b strings.Builder
	_, _ = b.WriteString("SELECT t0.c")
	for i := 1; i <= n; i++ {
		_, _ = fmt.Fprintf(&b, ", t%d.c AS c%d", i, i)
	}
	_, _ = b.WriteString(" FROM t0")
	for i := 1; i <= n; i++ {
		_, _ = fmt.Fprintf(&b, " JOIN t%d ON t%d.id = t%d.id", i, i-1, i)
	}
	return b.String()
}
