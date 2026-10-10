package spike

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// expectedEdge mirrors the corpus' annotated edges, so the target extractor can be
// checked against the analyzer's own answer rather than against my reading of the
// DML grammar.
type expectedEdge struct {
	FromTable string `yaml:"from_table"`
	ToTable   string `yaml:"to_table"`
	ToField   string `yaml:"to_field"`
	IsTemp    *bool  `yaml:"is_temp"`
}

type annotatedCase struct {
	Name string `yaml:"name"`
	SQL  string `yaml:"sql"`
	// Flat edges, plus the per-statement grouping the corpus uses.
	ExpectedEdges []expectedEdge `yaml:"expected_edges"`

	Statements []struct {
		SQL           string         `yaml:"sql"`
		ExpectedEdges []expectedEdge `yaml:"expected_edges"`
	} `yaml:"statements"`
}

type annotatedFile struct {
	Name  string          `yaml:"name"`
	Cases []annotatedCase `yaml:"cases"`
}

// TestDMLTargetCoverage checks the D9 extractor against every corpus case that
// writes a real table: the target it reads must be the table the analyzer said the
// statement writes. This is the only independent evidence available offline that
// the helper does not silently invent or miss a target.
func TestDMLTargetCoverage(t *testing.T) {
	dirs := map[string]storepb.Engine{
		"mysql":      storepb.Engine_MYSQL,
		"postgresql": storepb.Engine_POSTGRES,
		"starrocks":  storepb.Engine_STARROCKS,
	}
	var (
		checked, matched, missed, wrong int
		samples                         []string
	)
	for dir, engine := range dirs {
		files, _ := filepath.Glob(filepath.Join("..", "plugin", "lineage", dir, "testdata", "analyze", "*.yaml"))
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			var parsed annotatedFile
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("yaml %s: %v", file, err)
			}
			for _, c := range parsed.Cases {
				for _, group := range append([]struct {
					SQL           string         `yaml:"sql"`
					ExpectedEdges []expectedEdge `yaml:"expected_edges"`
				}{{SQL: c.SQL, ExpectedEdges: c.ExpectedEdges}}, c.Statements...) {
					sql := strings.TrimSpace(group.SQL)
					if sql == "" {
						continue
					}
					want := map[string]bool{}
					for _, e := range group.ExpectedEdges {
						if e.ToTable == "" || e.ToTable == "__result__" || e.ToTable == "__deletion__" {
							continue
						}
						if e.IsTemp != nil && *e.IsTemp {
							continue
						}
						want[e.ToTable] = true
					}
					if len(want) == 0 {
						continue
					}
					checked++
					got, err := DMLTargets(engine, sql)
					if err != nil {
						missed++
						if len(samples) < 15 {
							samples = append(samples, fmt.Sprintf("[%s] no target (%v): %s", engine, err, oneLine(sql)))
						}
						continue
					}
					ok := false
					for _, g := range got {
						if want[g] || want[lastSegment(g)] || wantAny(want, g) {
							ok = true
						}
					}
					if ok {
						matched++
						continue
					}
					wrong++
					if len(samples) < 15 {
						samples = append(samples, fmt.Sprintf("[%s] got %v want %v: %s", engine, got, keys(want), oneLine(sql)))
					}
				}
			}
		}
	}
	fmt.Printf("\n### D9 target extraction over corpus write statements\n")
	fmt.Printf("### cases with a real write target: %d\n", checked)
	fmt.Printf("### target found and matching:     %d\n", matched)
	fmt.Printf("### no target extracted:           %d\n", missed)
	fmt.Printf("### target extracted but different: %d\n", wrong)
	if len(samples) > 0 {
		fmt.Printf("\n--- samples ---\n%s\n", strings.Join(samples, "\n"))
	}
}

func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func wantAny(want map[string]bool, got string) bool {
	for w := range want {
		if lastSegment(w) == lastSegment(got) {
			return true
		}
	}
	return false
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
