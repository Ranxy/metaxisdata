package sqlsanitize_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
)

// benchmarkCorpusStatements reads the repository's own lineage corpus — the widest sample of real
// statement shapes available offline — and keeps the statements the analyzer accepts, which is what
// the ingest path sees.
func benchmarkCorpusStatements(b *testing.B, dir string, engine storepb.Engine) []string {
	b.Helper()

	files, err := filepath.Glob(filepath.Join("..", "lineage", dir, "testdata", "analyze", "*.yaml"))
	if err != nil {
		b.Fatal(err)
	}
	statements := make([]string, 0, 256)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			b.Fatal(err)
		}
		var parsed corpusFile
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			b.Fatal(err)
		}
		for _, c := range parsed.Cases {
			sql := strings.TrimSpace(c.SQL)
			if sql == "" {
				continue
			}
			if _, err := corpusAnalyzer.Analyze(context.Background(), engine, sql); err != nil {
				continue
			}
			statements = append(statements, sql)
		}
	}
	if len(statements) == 0 {
		b.Fatalf("no analyzer-accepted statement under %s", dir)
	}
	return statements
}

// BenchmarkIngestCost is the sanitizer's cost beside the cost of what follows it, because the
// answer to "is the sanitizer fast enough" is a share, not a number of nanoseconds. Every reported
// statement pays all three layers: Sanitize, then Analyzable, then the analyzer's own parse.
//
// Read it as: `sanitize` is what this package costs, `sanitize_analyzable` adds the form the
// analyzer is handed, and the difference to `full_gate` is the analyzer's parse — which the runner
// then does a second time.
func BenchmarkIngestCost(b *testing.B) {
	dialects := []struct {
		dir    string
		engine storepb.Engine
	}{
		{"mysql", storepb.Engine_MYSQL},
		{"postgresql", storepb.Engine_POSTGRES},
		{"starrocks", storepb.Engine_STARROCKS},
	}

	for _, dialect := range dialects {
		statements := benchmarkCorpusStatements(b, dialect.dir, dialect.engine)
		total := 0
		for _, sql := range statements {
			total += len(sql)
		}
		b.Run(dialect.dir+"/sanitize", func(b *testing.B) {
			b.SetBytes(int64(total))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, sql := range statements {
					if _, err := sqlsanitize.Sanitize(dialect.engine, sql); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
		b.Run(dialect.dir+"/sanitize_analyzable", func(b *testing.B) {
			b.SetBytes(int64(total))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, sql := range statements {
					stored, err := sqlsanitize.Sanitize(dialect.engine, sql)
					if err != nil {
						b.Fatal(err)
					}
					if sqlsanitize.Analyzable(dialect.engine, stored.Text) == "" {
						b.Fatal("no analyzable form")
					}
				}
			}
		})
		b.Run(dialect.dir+"/full_gate", func(b *testing.B) {
			b.SetBytes(int64(total))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, sql := range statements {
					stored, err := sqlsanitize.Sanitize(dialect.engine, sql)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := corpusAnalyzer.Analyze(context.Background(), dialect.engine, sqlsanitize.Analyzable(dialect.engine, stored.Text)); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
