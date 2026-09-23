package postgresql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// corpusAllocationBudget is the allocation count one pass over the whole corpus
// is allowed. The corpus is the widest sample of real shapes the analyzer has, so
// a per-edge regression shows up here even when no single shape exceeds its own
// budget.
const corpusAllocationBudget = 34000

// TestAnalyzeAllocationBudget keeps the analyzer's allocation count from creeping
// up. The budget is a count, not a wall-clock number, so it does not depend on
// the machine running it: a time budget fails on a loaded CI runner and passes on
// a quiet laptop, which makes it a regression detector nobody trusts. The limits
// carry headroom over the measured values, and `BenchmarkAnalyze*` remains the
// place to look when a shape actually slows down.
func TestAnalyzeAllocationBudget(t *testing.T) {
	// Deliberately not parallel: AllocsPerRun counts allocations process-wide, so
	// a test running beside it would be measured as part of it.
	shapes := []struct {
		name  string
		sql   string
		limit float64
	}{
		{"simple_select", "SELECT a, b, c FROM t WHERE a > 1", 160},
		{"join_3", "SELECT a.x, b.y, c.z FROM a JOIN b ON a.id = b.aid JOIN c ON b.id = c.bid", 230},
		{
			"cte_union_subquery",
			"WITH x AS (SELECT id, name FROM a), y AS (SELECT id FROM b) " +
				"SELECT name FROM x WHERE id IN (SELECT id FROM y) UNION SELECT name FROM c",
			370,
		},
		{"window_aggregate", "SELECT dept, SUM(salary) OVER (PARTITION BY dept ORDER BY hired) AS running FROM emp", 180},
		{"insert_select", "INSERT INTO dst (a, b) SELECT id, name FROM src WHERE id > 1", 160},
		{"create_view_expr", "CREATE VIEW v AS SELECT a.id, a.name, b.val * 2 AS doubled FROM a JOIN b ON a.id = b.id", 210},
		{"nested_set_operation", "SELECT a FROM t1 UNION ALL SELECT b FROM t2 INTERSECT SELECT c FROM t3", 200},
		{"wildcard", "SELECT * FROM t", 70},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(50, func() {
				if _, err := NewAnalyzer(context.Background(), shape.sql, nil).AnalyzeRelations(); err != nil {
					t.Fatalf("analyze %q: %v", shape.sql, err)
				}
			})
			t.Logf("%.0f allocations, budget %.0f", allocs, shape.limit)
			require.LessOrEqualf(t, allocs, shape.limit,
				"analyzing %q allocated %.0f times, over its budget of %.0f; if the increase is deliberate, raise the limit and say why",
				shape.sql, allocs, shape.limit)
		})
	}

	t.Run("corpus", func(t *testing.T) {
		cases := testutil.LoadCorpusBenchCases(t, testdataPath("analyze"))
		allocs := testing.AllocsPerRun(5, func() {
			for _, c := range cases {
				if _, err := NewAnalyzer(context.Background(), c.SQL, nil).AnalyzeRelations(); err != nil {
					t.Fatalf("analyze %q: %v", c.Name, err)
				}
			}
		})
		t.Logf("%.0f allocations over %d corpus cases, budget %d", allocs, len(cases), corpusAllocationBudget)
		require.LessOrEqualf(t, allocs, float64(corpusAllocationBudget),
			"one pass over %d corpus cases allocated %.0f times, over the budget of %d; if the increase is deliberate, raise the limit and say why",
			len(cases), allocs, corpusAllocationBudget)
	})
}
