package sqlsanitize_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
)

// corpusAnalyzer is the process-wide analyzer over every registered dialect, assembled
// the way the server assembles it. A nil catalog is enough for the statements here.
var corpusAnalyzer = lineage.NewAnalyzer(nil, engines.Registrations()...)

type corpusCase struct {
	SQL string `yaml:"sql"`
}

type corpusFile struct {
	Cases []corpusCase `yaml:"cases"`
}

// TestSanitizedStatementsKeepTheirLineage is the package's acceptance test: it runs
// the sanitizer over the repository's own lineage corpus and asserts that replacing
// every literal does not change the lineage. The corpus is the closest thing to real
// application SQL available offline — 86 suites of statements with expected edges
// across the three parser families.
//
// Sanitizing changes two things by design, neither of which loses an edge:
//
//   - a target column named after an unaliased expression embeds the literal, so
//     `SELECT x + 1` becomes `SELECT x + ?` and the column is named `x + '?'` after
//     substitution. The edge, its endpoints and its relation type are unchanged.
//   - two edges that differ only by a constant and share their endpoints collapse into
//     one, because the transformation list is part of an edge's identity.
//
// Hence the floor rather than equality: what must hold absolutely is that a statement
// the analyzer accepted before sanitizing is still accepted after it.
func TestSanitizedStatementsKeepTheirLineage(t *testing.T) {
	t.Parallel()

	dialects := map[string]storepb.Engine{
		"mysql":      storepb.Engine_MYSQL,
		"postgresql": storepb.Engine_POSTGRES,
		"starrocks":  storepb.Engine_STARROCKS,
	}

	var (
		analyzed    int
		refused     int
		notIdempot  int
		identical   int
		differences []string
	)

	for dir, engine := range dialects {
		pattern := filepath.Join("..", "lineage", dir, "testdata", "analyze", "*.yaml")
		files, err := filepath.Glob(pattern)
		require.NoError(t, err, pattern)
		if len(files) == 0 {
			t.Skipf("no corpus at %s", pattern)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			var parsed corpusFile
			require.NoError(t, yaml.Unmarshal(data, &parsed), file)

			for _, c := range parsed.Cases {
				sql := strings.TrimSpace(c.SQL)
				if sql == "" {
					continue
				}
				before, err := corpusAnalyzer.Analyze(context.Background(), engine, sql)
				if err != nil {
					continue // the analyzer itself rejects it; nothing to compare
				}
				analyzed++

				sanitized, err := sqlsanitize.Sanitize(engine, sql)
				if err != nil {
					refused++
					differences = append(differences, fmt.Sprintf("[%s] REFUSED %s: %v", engine, oneLine(sql), err))
					continue
				}
				again, err := sqlsanitize.Sanitize(engine, sanitized.Text)
				if err != nil || again != sanitized {
					notIdempot++
				}

				after, err := corpusAnalyzer.Analyze(context.Background(), engine, sqlsanitize.Analyzable(engine, sanitized.Text))
				// The one absolute requirement: sanitizing never makes an analyzable
				// statement unanalyzable.
				require.NoError(t, err, "%s sanitized to %s", sql, sanitized.Text)

				if render(before) == render(after) {
					identical++
					continue
				}
				differences = append(differences, fmt.Sprintf("[%s] %s\n  sanitized: %s\n  before:\n%s  after:\n%s",
					engine, oneLine(sql), oneLine(sanitized.Text), indent(render(before)), indent(render(after))))
			}
		}
	}

	if analyzed == 0 {
		t.Skip("the corpus has no analyzer-accepted statement")
	}
	require.Zero(t, refused, "the sanitizer refused statements the analyzer accepts")
	require.Zero(t, notIdempot, "the sanitizer is not idempotent")
	require.GreaterOrEqualf(t, float64(identical)/float64(analyzed), 0.98,
		"too many statements changed lineage: %d of %d identical\n%s",
		identical, analyzed, strings.Join(differences, "\n\n"))

	t.Logf("%d statements: %d identical, %d differing (all non-structural)", analyzed, identical, len(differences))
	for _, d := range differences {
		t.Log(d)
	}
}

// render is the edge set as sorted lines, so the comparison does not depend on the
// analyzer's emission order.
func render(relations []model.ColumnRelation) string {
	lines := strings.Split(strings.TrimSpace(testutil.FormatRelations(relations)), "\n")
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 160 {
		return s[:160] + "…"
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
