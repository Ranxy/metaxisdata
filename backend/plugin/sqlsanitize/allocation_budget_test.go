package sqlsanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSanitizeAllocationBudget keeps the sanitizer's allocation count from creeping up. The budget
// is a count, not a wall-clock number, so it does not depend on the machine running it: a time
// budget fails on a loaded CI runner and passes on a quiet laptop, which makes it a regression
// detector nobody trusts. The limits carry headroom over the measured values, and
// `BenchmarkSanitize` remains the place to look when a shape actually slows down.
//
// The dialect with the highest count sets the limit for each shape: on a token-dense statement the
// three lexers differ by a factor of three — StarRocks allocates the fewest, because its tokenizer
// hands back one slice of tokens — but the budget is not meant to pin which of them is cheapest
// today, only to notice when a rule change or an upstream bump adds work to all of them.
func TestSanitizeAllocationBudget(t *testing.T) {
	// Deliberately not parallel: AllocsPerRun counts allocations process-wide, so a test running
	// beside it would be measured as part of it.
	shapes := []struct {
		name  string
		sql   string
		limit float64
	}{
		{"select_small", "SELECT id, name FROM users WHERE email = 'a@b.c' AND active = ?", 48},
		{"insert_batch_100", benchBatchInsert(100), 3100},
		{"insert_batch_1000", benchBatchInsert(1000), 30000},
		{"in_list_100", benchInList(100), 490},
		{"predicate_20", benchWidePredicate(20), 260},
		{
			"view_ddl",
			"CREATE VIEW v AS SELECT a.id, b.name, COUNT(*) AS n FROM a " +
				"JOIN b ON a.id = b.aid LEFT JOIN c ON b.id = c.bid GROUP BY a.id, b.name HAVING COUNT(*) > 1",
			130,
		},
		{"long_literal", "SELECT '" + strings.Repeat("x", 10<<10) + "' AS pad FROM t", 48},
		{"commented", "SELECT a, -- one\n b, /* two */ c\nFROM t # three\nWHERE d = 'x' /* four */", 44},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			for _, engine := range everyEngine {
				allocs := testing.AllocsPerRun(20, func() {
					if _, err := Sanitize(engine, shape.sql); err != nil {
						t.Fatalf("sanitize %q: %v", shape.sql, err)
					}
				})
				t.Logf("%s: %.0f allocations, budget %.0f", engineName(engine), allocs, shape.limit)
				require.LessOrEqualf(t, allocs, shape.limit,
					"sanitizing %s on %s allocated %.0f times, over its budget of %.0f; if the increase is deliberate, raise the limit and say why",
					shape.name, engineName(engine), allocs, shape.limit)
			}
		})
	}
}
