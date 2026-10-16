package doris

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// corpusAllocationBudget is the allocation count one pass over the whole corpus is
// allowed. The corpus is the widest sample of real shapes the analyzer has, so a
// per-edge regression shows up here even when no single shape exceeds its own
// budget.
// The budget carries over the deliberate increase the VARIANT subcolumn path
// added: the scope-aware collector keeps one source per column reference, and a
// dotted reference is probed against the scope before it is read as a path. It also
// carries the two INTO OUTFILE cases, which measure 31 allocations each on top of
// the pass they are added to, and the two LATERAL VIEW cases, which measure about 930
// each because the statement is tokenized twice and its body analyzed through a
// rewritten source; the two wildcard-target cases add about 130 each, and the
// mixed-spelling VARIANT paths about 230 each, because such a statement is tokenized
// twice, rebuilt and re-parsed. The four grouping-set cases (ROLLUP, CUBE, GROUPING
// SETS, a repeated key) add nothing measurable: they differ from a plain GROUP BY
// only in how the key list is read. The WITH-clause cases add about 550 each, because such a
// statement is tokenized twice and its body re-parsed after the clause moves.
//
// The limit is the 38049 measured under the CI flags on the corpus this branch
// carries, with ~10% headroom: the previous 3% headroom was exhausted by one corpus
// case and the failure it produced landed only on CI, which is the outcome this budget
// is least useful as. The wildcard-target cases add about 360 each, because a `*` over a
// relation whose columns are known now allocates one output column and one source
// reference per column instead of one wildcard for the whole relation, and this branch's
// three CTE-exposure cases about 200 each.
const corpusAllocationBudget = 42000

// TestAnalyzeAllocationBudget keeps the analyzer's allocation count from creeping
// up. The budget is a count, not a wall-clock number, so it does not depend on
// the machine running it: a time budget fails on a loaded CI runner and passes on
// a quiet laptop, which makes it a regression detector nobody trusts. The limits
// carry headroom over the measured values, and `BenchmarkAnalyze*` remains the
// place to look when a shape actually slows down.
//
// The limits are calibrated under `go test -race -cover ./...`, the flags CI runs, and
// not under a plain build: coverage instrumentation allocates a little of its own, so a
// limit set from a plain build passes locally and fails only on CI. Raising one is a
// deliberate act — say in the comment which shape grew and why.
func TestAnalyzeAllocationBudget(t *testing.T) {
	// Deliberately not parallel: AllocsPerRun counts allocations process-wide, so
	// a test running beside it would be measured as part of it.
	shapes := []struct {
		name  string
		sql   string
		limit float64
	}{
		{"simple_select", "SELECT a, b, c FROM t WHERE a > 1", 145},
		{"join_3", "SELECT a.x, b.y, c.z FROM a JOIN b ON a.id = b.aid JOIN c ON b.id = c.bid", 225},
		{
			// Two CTEs, and each one now keeps the columns its body exposes, which costs
			// the two allocations this limit carries over the 405 it had.
			"cte_union_subquery",
			"WITH x AS (SELECT id, name FROM a), y AS (SELECT id FROM b) " +
				"SELECT name FROM x WHERE id IN (SELECT id FROM y) UNION SELECT name FROM c",
			445,
		},
		{"window_aggregate", "SELECT dept, SUM(salary) OVER (PARTITION BY dept ORDER BY hired) AS running FROM emp", 185},
		{"insert_select", "INSERT INTO dst (a, b) SELECT id, name FROM src WHERE id > 1", 145},
		{"create_view_expr", "CREATE VIEW v AS SELECT a.id, a.name, b.val * 2 AS doubled FROM a JOIN b ON a.id = b.id", 205},
		{"nested_set_operation", "SELECT a FROM t1 UNION ALL SELECT b FROM t2 INTERSECT SELECT c FROM t3", 215},
		{"wildcard", "SELECT * FROM t", 60},
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
				// A corpus case that reports a gap beside its edges is measured
				// like any other; only an unexpected error fails.
				if _, err := NewAnalyzer(context.Background(), c.SQL, nil).AnalyzeRelations(); !testutil.IsPartialAnalysis(err) {
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
