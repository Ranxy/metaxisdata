package mysql

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"

	mysqlparser "github.com/bytebase/omni/mysql/parser"
)

// BenchmarkAnalyzeCorpus measures end-to-end analysis (parse + lineage) over the
// whole golden corpus in one operation, giving a single throughput number for
// the analyzer.
func BenchmarkAnalyzeCorpus(b *testing.B) {
	cases := loadCorpusBenchCases(b)
	b.ReportAllocs()
	for b.Loop() {
		for _, c := range cases {
			if _, err := NewAnalyzer(context.Background(), c.SQL, nil).AnalyzeRelations(); err != nil {
				b.Fatalf("case %q: %v", c.Name, err)
			}
		}
	}
}

// BenchmarkAnalyzeCase measures each corpus statement separately so the slow
// shapes are visible.
func BenchmarkAnalyzeCase(b *testing.B) {
	for _, c := range loadCorpusBenchCases(b) {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := NewAnalyzer(context.Background(), c.SQL, nil).AnalyzeRelations(); err != nil {
					b.Fatalf("case %q: %v", c.Name, err)
				}
			}
		})
	}
}

// BenchmarkParseCase isolates the omni parse cost from the lineage walk, so the
// analyzer's own overhead is (AnalyzeCase - ParseCase).
func BenchmarkParseCase(b *testing.B) {
	for _, c := range loadCorpusBenchCases(b) {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := mysqlparser.Parse(c.SQL); err != nil {
					b.Fatalf("case %q: %v", c.Name, err)
				}
			}
		})
	}
}

// BenchmarkAnalyzeShapes measures synthetic statements that scale a single
// dimension (column count, nesting depth, join count) to show how analysis cost
// grows.
func BenchmarkAnalyzeShapes(b *testing.B) {
	shapes := []benchCase{
		{"simple_select", "SELECT a, b, c FROM t WHERE a > 1"},
		{"join_3", "SELECT a.x, b.y, c.z FROM a JOIN b ON a.id = b.aid JOIN c ON b.id = c.bid"},
		{"cte_union_subquery", "WITH x AS (SELECT id, name FROM a), y AS (SELECT id FROM b) " +
			"SELECT name FROM x WHERE id IN (SELECT id FROM y) UNION SELECT name FROM c"},
		{"window_aggregate", "SELECT dept, SUM(salary) OVER (PARTITION BY dept ORDER BY hired) AS running FROM emp"},
		{"insert_select", "INSERT INTO dst (a, b) SELECT id, name FROM src"},
		{"ctas_view_expr", "CREATE VIEW v AS SELECT a.id, a.name, b.val * 2 AS doubled FROM a JOIN b ON a.id = b.id"},
		{"wide_100_cols", buildWideSelect(100)},
		{"deep_subquery_10", buildDeepSubquery(10)},
		{"many_joins_10", buildManyJoins(10)},
	}
	for _, c := range shapes {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := NewAnalyzer(context.Background(), c.SQL, nil).AnalyzeRelations(); err != nil {
					b.Fatalf("case %q: %v", c.Name, err)
				}
			}
		})
	}
}

// BenchmarkAnalyzeWildcardWithCatalog measures SELECT * expansion against a
// 100-column catalog, the production path where the metadata store is consulted.
func BenchmarkAnalyzeWildcardWithCatalog(b *testing.B) {
	columns := make([]catalog.ColumnMeta, 100)
	for i := range columns {
		columns[i] = catalog.ColumnMeta{Name: fmt.Sprintf("c%d", i)}
	}
	provide := catalog.NewMemoryCatalogProvide()
	provide.AddTable(&catalog.TableMeta{
		ID:      model.ObjectIdentifier{Database: "db", Name: "t"},
		Columns: columns,
	})
	sql := "SELECT * FROM db.t"

	b.Run("with_catalog", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := NewAnalyzer(context.Background(), sql, provide).AnalyzeRelations(); err != nil {
				b.Fatalf("with catalog: %v", err)
			}
		}
	})
	b.Run("without_catalog", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := NewAnalyzer(context.Background(), sql, nil).AnalyzeRelations(); err != nil {
				b.Fatalf("without catalog: %v", err)
			}
		}
	})
}

type benchCase struct {
	Name string
	SQL  string
}

func loadCorpusBenchCases(b *testing.B) []benchCase {
	b.Helper()
	dir := testdataPath("analyze")
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.Fatalf("read testdata: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	slices.Sort(files)

	var cases []benchCase
	for _, file := range files {
		suite, err := testutil.LoadLineageTestSuiteFromYAML(file)
		if err != nil {
			b.Fatalf("load %s: %v", file, err)
		}
		for _, tc := range suite.Cases {
			cases = append(cases, benchCase{Name: benchCaseName(tc.Name), SQL: tc.SQL})
		}
	}
	if len(cases) == 0 {
		b.Fatal("no corpus cases found")
	}
	return cases
}

func benchCaseName(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}

func buildWideSelect(n int) string {
	columns := make([]string, n)
	for i := range columns {
		columns[i] = fmt.Sprintf("c%d", i)
	}
	return "SELECT " + strings.Join(columns, ", ") + " FROM t"
}

func buildDeepSubquery(n int) string {
	query := "SELECT c FROM t0"
	for i := 1; i <= n; i++ {
		query = fmt.Sprintf("SELECT s%d.c FROM (%s) s%d", i, query, i)
	}
	return query
}

func buildManyJoins(n int) string {
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
