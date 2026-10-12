package sqlsanitize

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The sanitizer runs once per reported statement, on the request path, before the analyzer that
// follows it parses the same text a second time. These benchmarks measure the statement shapes a
// report can drive, from the typical one to the extremes the endpoint's limits allow, and then
// split one statement's cost into the layers Sanitize is made of.
//
// Layer names match the functions, so `BenchmarkSanitizeLayers` reads as the cost of one lexer
// pass, of the rewrite, of the canonical form's second pass, and of the analyzable form's third.
// Three passes are by design rather than waste: replacing a literal with `?` changes token
// boundaries in one dialect (PostgreSQL reads `-1` as two tokens and `-?` as one), so the canonical
// form cannot reuse the first pass's tokens.

// benchBatchInsert is an INSERT of rows value groups, the shape an ORM batches with.
func benchBatchInsert(rows int) string {
	var b strings.Builder
	_, _ = b.WriteString("INSERT INTO orders (id, user_id, total, status, created_at) VALUES ")
	for i := 0; i < rows; i++ {
		if i > 0 {
			_, _ = b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%d, %d, %d.%02d, 'paid', '2026-01-14T09:00:00Z')", i, i+1, i*7%1000, i%100)
	}
	return b.String()
}

// benchInList is the shape whose length an application decides at runtime.
func benchInList(n int) string {
	var b strings.Builder
	_, _ = b.WriteString("SELECT id, total FROM orders WHERE user_id IN (")
	for i := 0; i < n; i++ {
		if i > 0 {
			_, _ = b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", i)
	}
	return b.String() + ") AND status = 'paid'"
}

// benchWidePredicate is a filter an ORM emits for a search form: many ANDed comparisons.
func benchWidePredicate(n int) string {
	var b strings.Builder
	_, _ = b.WriteString("SELECT id FROM orders WHERE 1 = 1")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, " AND col_%d = 'value-%d'", i, i)
	}
	return b.String()
}

// benchShapes returns the statements the shape benchmark measures. They are built once, so a
// benchmark's setup never lands inside its timing loop.
func benchShapes() []struct {
	name string
	sql  string
} {
	return []struct {
		name string
		sql  string
	}{
		{"select_small", "SELECT id, name FROM users WHERE email = 'a@b.c' AND active = ?"},
		{"insert_batch_100", benchBatchInsert(100)},
		{"insert_batch_1000", benchBatchInsert(1000)},
		{"in_list_100", benchInList(100)},
		{"predicate_20", benchWidePredicate(20)},
		{
			"view_ddl",
			"CREATE VIEW v AS SELECT a.id, b.name, COUNT(*) AS n FROM a " +
				"JOIN b ON a.id = b.aid LEFT JOIN c ON b.id = c.bid GROUP BY a.id, b.name HAVING COUNT(*) > 1",
		},
		{"long_literal", "SELECT '" + strings.Repeat("x", 10<<10) + "' AS pad FROM t"},
		{
			"commented",
			"SELECT a, -- one\n b, /* two */ c\nFROM t # three\nWHERE d = 'x' /* four */",
		},
	}
}

// BenchmarkSanitize measures one reported statement per iteration, per dialect. It is the number
// the ingest path pays per statement, before the analyzer's own parse.
func BenchmarkSanitize(b *testing.B) {
	for _, engine := range everyEngine {
		for _, shape := range benchShapes() {
			b.Run(engineName(engine)+"/"+shape.name, func(b *testing.B) {
				if _, err := Sanitize(engine, shape.sql); err != nil {
					b.Fatalf("sanitize %s: %v", shape.name, err)
				}
				b.SetBytes(int64(len(shape.sql)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := Sanitize(engine, shape.sql); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkSanitizeLayers measures the same statement one layer at a time, so the cost of each
// lexer pass and each fold is visible on its own.
func BenchmarkSanitizeLayers(b *testing.B) {
	for _, shape := range []struct {
		name string
		sql  string
	}{
		{"select_small", "SELECT id, name FROM users WHERE email = 'a@b.c' AND active = ?"},
		{"insert_batch_100", benchBatchInsert(100)},
		{"insert_batch_1000", benchBatchInsert(1000)},
	} {
		engine := storepb.Engine_MYSQL
		b.Run(shape.name+"/token_spans", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if spans := tokenSpans(engine, shape.sql); len(spans) == 0 {
					b.Fatal("no tokens")
				}
			}
		})
		b.Run(shape.name+"/stored_text", func(b *testing.B) {
			spans := tokenSpans(engine, shape.sql)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if text := strings.TrimSpace(rewriteLiterals(engine, shape.sql, spans)); text == "" {
					b.Fatal("no text")
				}
			}
		})
		b.Run(shape.name+"/canonical", func(b *testing.B) {
			spans := tokenSpans(engine, shape.sql)
			text := strings.TrimSpace(rewriteLiterals(engine, shape.sql, spans))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if canonical := canonicalForm(engine, text); canonical == "" {
					b.Fatal("no canonical form")
				}
			}
		})
		b.Run(shape.name+"/analyzable", func(b *testing.B) {
			spans := tokenSpans(engine, shape.sql)
			text := strings.TrimSpace(rewriteLiterals(engine, shape.sql, spans))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if analyzable := Analyzable(engine, text); analyzable == "" {
					b.Fatal("no analyzable form")
				}
			}
		})
		b.Run(shape.name+"/sanitize_all", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Sanitize(engine, shape.sql); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSanitizeMalformedText measures text that is not real SQL: the shapes a client that is
// broken, or hostile, can send. D6 gives the route no request budget, so this is what a looping key
// can spend per call, and it is worth knowing beside the cost of a real statement.
//
// Whether each shape is refused is dialect-specific rather than a property of the shape — the MySQL
// family reads an unterminated string as one literal token and stores it as `?`, while PostgreSQL and
// StarRocks refuse the same bytes — so the benchmark records each outcome once and asserts only that
// it does not change underfoot.
func BenchmarkSanitizeMalformedText(b *testing.B) {
	shapes := []struct {
		name string
		sql  string
	}{
		{"empty", "   "},
		{"only_a_comment", "/* " + strings.Repeat("x", 1<<10) + " */"},
		{"unterminated_string", "SELECT a FROM t WHERE b = 'unterminated"},
		{"unterminated_string_after_10kb", "SELECT '" + strings.Repeat("x", 10<<10) + " FROM t WHERE b = 'x"},
		{"ten_kb_of_words", "SELECT " + strings.Repeat("a, ", 2<<10) + "a FROM t"},
	}
	for _, engine := range everyEngine {
		for _, shape := range shapes {
			refused := false
			if _, err := Sanitize(engine, shape.sql); err != nil {
				refused = true
			}
			b.Run(engineName(engine)+"/"+shape.name, func(b *testing.B) {
				b.SetBytes(int64(len(shape.sql)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := Sanitize(engine, shape.sql); (err != nil) != refused {
						b.Fatalf("the outcome of sanitizing %s changed", shape.name)
					}
				}
			})
		}
	}
}

// BenchmarkSanitizeCorpus runs the pinned corpus in testdata once per iteration: every case under
// every dialect it names. The shapes above are the extremes; this is the mixed sample.
func BenchmarkSanitizeCorpus(b *testing.B) {
	type corpusStatement struct {
		engine storepb.Engine
		sql    string
	}

	paths, err := filepath.Glob(filepath.Join("testdata", "*.yaml"))
	if err != nil {
		b.Fatal(err)
	}
	corpus := make([]corpusStatement, 0, 512)
	total := 0
	for _, path := range paths {
		suite, err := loadSanitizeSuite(path)
		if err != nil {
			b.Fatal(err)
		}
		for _, c := range suite.cases {
			if c.expectError {
				continue
			}
			for _, engine := range c.engines {
				corpus = append(corpus, corpusStatement{engine: engine, sql: c.sql})
				total += len(c.sql)
			}
		}
	}
	if len(corpus) == 0 {
		b.Fatal("the corpus is empty")
	}

	b.SetBytes(int64(total))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, statement := range corpus {
			if _, err := Sanitize(statement.engine, statement.sql); err != nil {
				b.Fatalf("%v %q: %v", statement.engine, statement.sql, err)
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(corpus))*float64(b.N)/b.Elapsed().Seconds(), "stmts/s")
}
