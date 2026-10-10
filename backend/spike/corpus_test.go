package spike

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

var analyzer = lineage.NewAnalyzer(nil, engines.Registrations()...)

type corpusCase struct {
	Name string `yaml:"name"`
	SQL  string `yaml:"sql"`
}

type corpusFile struct {
	Name  string       `yaml:"name"`
	Cases []corpusCase `yaml:"cases"`
}

type corpusEntry struct {
	engine storepb.Engine
	file   string
	name   string
	sql    string
}

// loadCorpus reads the repository's own annotated lineage corpus. It is the
// closest thing to real application SQL available offline: 86 suites of
// statements with expected edges, across the three parser families.
func loadCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	dirs := map[string]storepb.Engine{
		"mysql":      storepb.Engine_MYSQL,
		"postgresql": storepb.Engine_POSTGRES,
		"starrocks":  storepb.Engine_STARROCKS,
	}
	var out []corpusEntry
	for dir, engine := range dirs {
		pattern := filepath.Join("..", "plugin", "lineage", dir, "testdata", "analyze", "*.yaml")
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			var parsed corpusFile
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("yaml %s: %v", file, err)
			}
			for _, c := range parsed.Cases {
				if strings.TrimSpace(c.SQL) == "" {
					continue
				}
				out = append(out, corpusEntry{engine: engine, file: filepath.Base(file), name: c.Name, sql: c.SQL})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].engine != out[j].engine {
			return out[i].engine < out[j].engine
		}
		return out[i].sql < out[j].sql
	})
	return out
}

// sortedRelations renders an edge set as sorted lines, so the comparison does not
// depend on the analyzer's emission order.
func sortedRelations(rendered string) string {
	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestCorpusSanitize measures the three things P0 has to answer: does literal
// replacement change lineage, is the sanitizer idempotent, and how often does it
// refuse a statement the analyzer itself accepts.
func TestCorpusSanitize(t *testing.T) {
	corpus := loadCorpus(t)

	var (
		analyzed         int
		unchanged        int
		differing        int
		refused          int
		idempotent       int
		withLiterals     int
		canonicalDiffers int
	)
	diffSamples := make([]string, 0, 12)
	refusalSamples := make([]string, 0, 12)

	for _, entry := range corpus {
		before, beforeErr := analyzer.Analyze(context.Background(), entry.engine, entry.sql)
		if beforeErr != nil {
			continue // the analyzer itself rejects it; not a fair comparison
		}
		analyzed++

		sanitized, err := Sanitize(entry.engine, entry.sql)
		if err != nil {
			refused++
			if len(refusalSamples) < 12 {
				refusalSamples = append(refusalSamples, fmt.Sprintf("[%s] %s => %v", entry.engine, oneLine(entry.sql), err))
			}
			continue
		}
		stored := sanitized.Text
		if sanitized.Canonical != stored {
			canonicalDiffers++
		}
		if stored != strings.TrimSpace(entry.sql) {
			withLiterals++
		}
		if twice, err2 := Sanitize(entry.engine, stored); err2 == nil && twice.Text == stored {
			idempotent++
		} else if err2 != nil {
			refusalSamples = append(refusalSamples, fmt.Sprintf("[%s] NOT IDEMPOTENT (second pass refused) %s => %v", entry.engine, oneLine(stored), err2))
		}

		after, afterErr := analyzer.Analyze(context.Background(), entry.engine, Analyzable(entry.engine, stored))
		if afterErr != nil {
			differing++
			if len(diffSamples) < 12 {
				diffSamples = append(diffSamples, fmt.Sprintf("[%s] %s\n  sanitized %s\n  analyze failed: %v", entry.engine, oneLine(entry.sql), oneLine(stored), afterErr))
			}
			continue
		}
		if sortedRelations(testutil.FormatRelations(before)) == sortedRelations(testutil.FormatRelations(after)) {
			unchanged++
			continue
		}
		differing++
		if len(diffSamples) < 12 {
			diffSamples = append(diffSamples, fmt.Sprintf("[%s] %s\n  sanitized: %s\n  before:\n%s\n  after:\n%s",
				entry.engine, oneLine(entry.sql), oneLine(stored),
				indent(testutil.FormatRelations(before)), indent(testutil.FormatRelations(after))))
		}
	}

	fmt.Printf("\n### corpus: %d statements analyzed by the analyzer\n", analyzed)
	fmt.Printf("### refused by the sanitizer:      %d\n", refused)
	fmt.Printf("### text changed by sanitizing:    %d\n", withLiterals)
	fmt.Printf("### canonical differs from stored: %d\n", canonicalDiffers)
	fmt.Printf("### idempotent:                    %d / %d sanitized\n", idempotent, analyzed-refused)
	fmt.Printf("### lineage identical:             %d / %d\n", unchanged, analyzed-refused)
	fmt.Printf("### lineage differing:             %d\n", differing)

	if len(refusalSamples) > 0 {
		fmt.Printf("\n--- sanitizer refusals ---\n%s\n", strings.Join(refusalSamples, "\n"))
	}
	if len(diffSamples) > 0 {
		fmt.Printf("\n--- lineage differences (first %d) ---\n%s\n", len(diffSamples), strings.Join(diffSamples, "\n\n"))
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 150 {
		return s[:150] + "…"
	}
	return s
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}
