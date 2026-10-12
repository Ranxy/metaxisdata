package sqlsanitize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The YAML corpus in testdata is this package's contract. Each case pins, for one
// statement, the stored text and the canonical form exactly; a case that names no engine
// runs under all three dialects, because nothing in N1-N6 is allowed to depend on which
// lexer produced the tokens.
//
// Pinning the canonical form pins the fingerprint too, and that is how the corpus states
// the two halves of the deduplication contract without a second mechanism: statements
// whose canonical forms are written identically must merge into one row, and statements
// whose canonical forms differ must not. A suite that folds is therefore read as a list of
// equivalence classes.
//
// On top of the pinned values the runner asserts what has to hold for every statement:
// nothing that is a literal or a comment survives into the stored text, the fingerprint is
// the canonical form's hash, sanitizing the stored text again changes nothing, and the
// analyzable form has no placeholder left in it.
var yamlEngines = map[string]storepb.Engine{
	"mysql":     storepb.Engine_MYSQL,
	"postgres":  storepb.Engine_POSTGRES,
	"starrocks": storepb.Engine_STARROCKS,
}

// everyEngine is the order an unpinned case runs in, so a failure lists the dialects in
// the same order every time.
var everyEngine = []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS}

func engineName(engine storepb.Engine) string {
	switch engine {
	case storepb.Engine_MYSQL:
		return "mysql"
	case storepb.Engine_POSTGRES:
		return "postgres"
	case storepb.Engine_STARROCKS:
		return "starrocks"
	default:
		return engine.String()
	}
}

type yamlSanitizeSuite struct {
	Name   string             `yaml:"name"`
	Engine yamlEngineField    `yaml:"engine"`
	Cases  []yamlSanitizeCase `yaml:"cases"`
}

// yamlEngineField accepts one dialect name or a list of them. An empty field means every
// dialect; a list is how a case that is valid in two of the three says so.
type yamlEngineField []string

func (f *yamlEngineField) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Value != "" {
			*f = yamlEngineField{value.Value}
		}
	case yaml.SequenceNode:
		var names []string
		if err := value.Decode(&names); err != nil {
			return err
		}
		*f = yamlEngineField(names)
	default:
		return errors.New("engine must be one dialect name or a list of them")
	}
	return nil
}

// yamlSanitizeCase keeps text, canonical and analyzable as pointers so that the loader can
// tell a written key from an omitted one: `expect_error` refuses them, and every other case
// is required to write text and canonical. Omitting one must be an error rather than a
// silently skipped comparison.
type yamlSanitizeCase struct {
	Name        string          `yaml:"name"`
	SQL         string          `yaml:"sql"`
	Engine      yamlEngineField `yaml:"engine"`
	Text        *string         `yaml:"text"`
	Canonical   *string         `yaml:"canonical"`
	Analyzable  *string         `yaml:"analyzable"`
	ExpectError bool            `yaml:"expect_error"`
}

type sanitizeSuite struct {
	name  string
	cases []sanitizeCase
}

type sanitizeCase struct {
	name        string
	sql         string
	engines     []storepb.Engine
	text        *string
	canonical   *string
	analyzable  *string
	expectError bool
}

// TestSanitizeYAMLCorpus runs every suite in testdata.
func TestSanitizeYAMLCorpus(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "the sanitizer has no YAML corpus")

	slices.Sort(paths)
	// One statement, one case. A copy-pasted case pins nothing new, and two cases that
	// disagree about the same statement under the same dialect is a contradiction the
	// per-case comparison cannot report as one.
	seen := make(map[string]string)
	for _, path := range paths {
		suite, err := loadSanitizeSuite(path)
		require.NoError(t, err)

		for _, c := range suite.cases {
			where := suite.name + " / " + c.name
			for _, engine := range c.engines {
				key := engineName(engine) + "\x00" + c.sql
				if other, ok := seen[key]; ok {
					require.Failf(t, "the same statement is pinned twice",
						"%s and %s both cover %q under %s", other, where, oneLine(c.sql), engineName(engine))
				}
				seen[key] = where
			}
		}

		t.Run(suite.name, func(t *testing.T) {
			t.Parallel()
			for _, c := range suite.cases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					for _, engine := range c.engines {
						checkSanitizeCase(t, c, engine)
					}
				})
			}
		})
	}
}

// validateSuiteSource rejects the two YAML spellings that quietly change what a case
// asserts, both of which a corpus author will otherwise hit eventually:
//
//   - ` #` in a plain scalar starts a comment, so `sql: SELECT a FROM t # hidden` parses as
//     `SELECT a FROM t` and the case asserts far less than it was written to.
//   - trailing whitespace is stripped from a plain scalar, so a stored text that ends in a
//     space — which a removed line comment leaves — cannot be pinned without quoting.
//
// It compares each plain scalar against the source line it started on rather than looking
// for ` #` in the file: a SQL string may contain one (`SELECT 'a # b'`), and a check on the
// text alone cannot tell that from the comment it actually is. A quoted scalar and a block
// scalar are skipped, since there `#` and trailing spaces are the author's.
func validateSuiteSource(path, content string, doc *yaml.Node) error {
	lines := strings.Split(content, "\n")

	check := func(node *yaml.Node) error {
		if node.Kind != yaml.ScalarNode || node.Style != 0 ||
			node.Line < 1 || node.Line > len(lines) || node.Column < 1 || node.Column-1 >= len(lines[node.Line-1]) {
			return nil
		}
		raw := lines[node.Line-1][node.Column-1:]
		switch {
		case raw != strings.TrimRight(raw, " \t"):
			return fmt.Errorf("%s line %d: YAML strips trailing whitespace from a plain scalar; quote the value to keep it", filepath.Base(path), node.Line)
		case raw != node.Value && strings.Contains(raw, " #"):
			return fmt.Errorf("%s line %d: ` #` starts a YAML comment in a plain scalar, so the value is cut short; quote it or use a block scalar", filepath.Base(path), node.Line)
		}
		// A plain scalar equal to its source line was fully consumed; one that is not,
		// without a comment or trailing space to explain it, continues on a later line.
		return nil
	}

	// Mapping keys are skipped: a key shares its line with the value, so its own source
	// slice would hold the value's text too.
	var walk func(node *yaml.Node) error
	walk = func(node *yaml.Node) error {
		for i, child := range node.Content {
			if node.Kind == yaml.MappingNode && i%2 == 0 {
				continue
			}
			if err := check(child); err != nil {
				return err
			}
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(doc)
}

func loadSanitizeSuite(path string) (sanitizeSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sanitizeSuite{}, fmt.Errorf("failed to read sanitize suite %q: %w", path, err)
	}

	var raw yamlSanitizeSuite
	// KnownFields rejects a misspelled key: a typo in `canonical` would otherwise drop the
	// exact comparison the case exists for.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return sanitizeSuite{}, fmt.Errorf("failed to unmarshal sanitize suite %q: %w", path, err)
	}

	// The same document again as a node tree, so the source guard can compare each plain
	// scalar with the line it was written on.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return sanitizeSuite{}, fmt.Errorf("failed to re-read sanitize suite %q: %w", path, err)
	}
	if err := validateSuiteSource(path, string(data), &doc); err != nil {
		return sanitizeSuite{}, err
	}

	defaultEngines, err := resolveEngines(raw.Engine)
	if err != nil {
		return sanitizeSuite{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	name := raw.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if len(raw.Cases) == 0 {
		return sanitizeSuite{}, fmt.Errorf("%s: suite %q has no cases", filepath.Base(path), name)
	}

	cases := make([]sanitizeCase, 0, len(raw.Cases))
	seen := make(map[string]string, len(raw.Cases))
	for i, rawCase := range raw.Cases {
		tc, err := rawCase.toCase(defaultEngines)
		if err != nil {
			return sanitizeSuite{}, fmt.Errorf("%s case %d (%s): %w", filepath.Base(path), i, rawCase.Name, err)
		}
		if other, ok := seen[tc.name]; ok {
			return sanitizeSuite{}, fmt.Errorf("%s: %q and %q have the same name", filepath.Base(path), other, tc.name)
		}
		seen[tc.name] = tc.name
		cases = append(cases, tc)
	}

	return sanitizeSuite{name: name, cases: cases}, nil
}

// resolveEngines maps the dialects a suite or case names to the engines to run it under.
// An empty list means every dialect.
func resolveEngines(names []string) ([]storepb.Engine, error) {
	if len(names) == 0 {
		return everyEngine, nil
	}
	engines := make([]storepb.Engine, 0, len(names))
	for _, name := range names {
		engine, ok := yamlEngines[name]
		if !ok {
			known := make([]string, 0, len(yamlEngines))
			for candidate := range yamlEngines {
				known = append(known, candidate)
			}
			slices.Sort(known)
			return nil, fmt.Errorf("unknown engine %q: use %s, or omit it to run every dialect", name, strings.Join(known, ", "))
		}
		engines = append(engines, engine)
	}
	return engines, nil
}

func (raw yamlSanitizeCase) toCase(defaultEngines []storepb.Engine) (sanitizeCase, error) {
	if strings.TrimSpace(raw.Name) == "" {
		return sanitizeCase{}, errors.New("a case needs a name")
	}

	engines := defaultEngines
	if len(raw.Engine) > 0 {
		var err error
		if engines, err = resolveEngines(raw.Engine); err != nil {
			return sanitizeCase{}, err
		}
	}

	// A pinned value that ends with a newline means the author wrote `|` where they meant
	// `|-`, and the trailing newline the block scalar adds would never match. A pinned
	// empty value is the same mistake in the other direction: no accepted statement
	// sanitizes to nothing.
	newlines := []struct {
		key   string
		value *string
	}{
		{"text", raw.Text},
		{"canonical", raw.Canonical},
		{"analyzable", raw.Analyzable},
	}
	for _, v := range newlines {
		if v.value == nil {
			continue
		}
		if strings.HasSuffix(*v.value, "\n") {
			return sanitizeCase{}, fmt.Errorf("%s ends with a newline: write `|-` instead of `|`", v.key)
		}
		if *v.value == "" {
			return sanitizeCase{}, fmt.Errorf("%s is empty", v.key)
		}
	}

	if raw.ExpectError && (raw.Text != nil || raw.Canonical != nil || raw.Analyzable != nil) {
		return sanitizeCase{}, errors.New("expect_error cannot also pin text, canonical or analyzable")
	}
	if !raw.ExpectError && (raw.Text == nil || raw.Canonical == nil) {
		return sanitizeCase{}, errors.New("a case must pin both text and canonical, or set expect_error")
	}

	return sanitizeCase{
		name:        raw.Name,
		sql:         raw.SQL,
		engines:     engines,
		text:        raw.Text,
		canonical:   raw.Canonical,
		analyzable:  raw.Analyzable,
		expectError: raw.ExpectError,
	}, nil
}

func checkSanitizeCase(t *testing.T, c sanitizeCase, engine storepb.Engine) {
	t.Helper()

	where := fmt.Sprintf("[%s] %s", engineName(engine), oneLine(c.sql))

	got, err := Sanitize(engine, c.sql)
	if c.expectError {
		require.ErrorIsf(t, err, ErrUnlexable, "%s must be refused", where)
		return
	}
	require.NoErrorf(t, err, "%s must be accepted", where)

	require.Equalf(t, *c.text, got.Text, "%s: stored text", where)
	require.Equalf(t, *c.canonical, got.Canonical, "%s: canonical form", where)

	// The fingerprint is the canonical form's hash and nothing else.
	sum := sha256.Sum256([]byte(got.Canonical))
	require.Equalf(t, hex.EncodeToString(sum[:]), got.Fingerprint, "%s: fingerprint", where)

	requireNoLiteral(t, where, engine, got.Text)
	requireTokenizedOutput(t, where, engine, got.Text)

	// A statement reported twice has to land on one row: re-sanitizing the stored text is
	// a no-op, including the fingerprint.
	again, err := Sanitize(engine, got.Text)
	require.NoErrorf(t, err, "%s: the stored text must be sanitizable again", where)
	require.Equalf(t, got, again, "%s: sanitizing the stored text changed it", where)

	// ...and so is folding the canonical form again, which is what makes the fingerprint a
	// function of the statement rather than of how many times it was reported.
	require.Equalf(t, got.Canonical, canonicalForm(engine, got.Canonical),
		"%s: folding the canonical form again changed it", where)

	analyzable := Analyzable(engine, got.Text)
	for _, s := range tokenSpans(engine, analyzable) {
		require.NotEqualf(t, Placeholder, analyzable[s.loc:s.end],
			"%s: a placeholder survived into the analyzable form %q", where, analyzable)
	}
	if c.analyzable != nil {
		require.Equalf(t, *c.analyzable, analyzable, "%s: analyzable form", where)
	}
}

// requireNoLiteral is the reason the package exists: an application reports its real SQL,
// and a literal that reaches the store is a value that has leaked out of it.
//
// It asks isLiteral, so it cannot see a literal form isLiteral itself does not know —
// which literal forms count is what the corpus pins. What this catches is the other
// direction: a form the package knows about being copied through by a later change.
func requireNoLiteral(t *testing.T, where string, engine storepb.Engine, text string) {
	t.Helper()

	for _, s := range tokenSpans(engine, text) {
		raw := text[s.loc:s.end]
		require.Falsef(t, isLiteral(engine, raw), "%s: %q survived into the stored text", where, raw)
	}
}

// requireTokenizedOutput checks that the stored text is a sequence of real tokens: every
// byte between two tokens is whitespace once comments are removed. A stored statement the
// lexer cannot walk to its end is a statement no later rule can inspect — and Sanitize
// refuses that on the way in, so a gap that is not whitespace here means some byte was
// copied through without ever being classified.
func requireTokenizedOutput(t *testing.T, where string, engine storepb.Engine, text string) {
	t.Helper()

	spans := tokenSpans(engine, text)
	cursor := 0
	for _, s := range spans {
		gap := text[cursor:s.loc]
		require.Equalf(t, "", strings.TrimSpace(stripComments(engine, gap)),
			"%s: %q is not a token, a comment or whitespace", where, gap)
		cursor = s.end
	}
	tail := text[cursor:]
	require.Equalf(t, "", strings.TrimSpace(stripComments(engine, tail)),
		"%s: %q follows the last token", where, tail)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// TestSuiteLoaderGuards pins the checks that keep a suite from quietly asserting less than
// it looks like it does, and the two spellings a corpus author will otherwise write by
// mistake: a ` #` that YAML reads as a comment, and a `|` block scalar that adds a newline
// no stored text can end with.
func TestSuiteLoaderGuards(t *testing.T) {
	t.Parallel()

	valid := `name: A suite
cases:
  - name: a one element list
    sql: SELECT a FROM t WHERE x IN (1)
    text: SELECT a FROM t WHERE x IN (?)
    canonical: SELECT a FROM t WHERE x IN ( ? )
`

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "a complete suite loads",
			yaml: valid,
		},
		{
			name: "a hash in a plain scalar would be cut short",
			yaml: `cases:
  - name: a hash comment
    sql: SELECT a FROM t # hidden
    text: SELECT a FROM t
    canonical: SELECT a FROM t
`,
			wantErr: "starts a YAML comment",
		},
		{
			// The guard compares the scalar with its source line, so a SQL string holding
			// ` #` is still caught: this line does truncate, and a check that only looked
			// for a ` #` in the file could not tell it from the quoted case below.
			name: "a hash after a sql quote still truncates",
			yaml: `cases:
  - name: a string holding a hash
    sql: SELECT 'a # b' FROM t
    text: SELECT ? FROM t
    canonical: SELECT ? FROM t
`,
			wantErr: "starts a YAML comment",
		},
		{
			name: "a quoted hash is fine",
			yaml: `cases:
  - name: a quoted hash comment
    sql: "SELECT a FROM t # hidden"
    text: SELECT a FROM t
    canonical: SELECT a FROM t
`,
		},
		{
			name: "a hash inside a block scalar is fine",
			yaml: `cases:
  - name: a block scalar hash comment
    sql: |-
      SELECT a FROM t # hidden
    text: SELECT a FROM t
    canonical: SELECT a FROM t
`,
		},
		{
			name:    "trailing whitespace in a plain scalar would be stripped",
			yaml:    "cases:\n  - name: a trailing space\n    sql: SELECT a\n    text: SELECT a \n    canonical: SELECT a\n",
			wantErr: "strips trailing whitespace",
		},
		{
			name: "an omitted canonical is an error",
			yaml: `cases:
  - name: half a case
    sql: SELECT a
    text: SELECT a
`,
			wantErr: "must pin both text and canonical",
		},
		{
			name: "expect_error with a pinned form is an error",
			yaml: `cases:
  - name: contradictory
    sql: "   "
    canonical: SELECT a
    expect_error: true
`,
			wantErr: "cannot also pin",
		},
		{
			name: "a block scalar newline in a pinned form is an error",
			yaml: `cases:
  - name: a block scalar
    sql: SELECT a
    text: |
      SELECT a
    canonical: SELECT a
`,
			wantErr: "write `|-` instead of `|`",
		},
		{
			name: "an unknown engine is an error",
			yaml: `cases:
  - name: a typo
    engine: mysq1
    sql: SELECT a
    text: SELECT a
    canonical: SELECT a
`,
			wantErr: "unknown engine",
		},
		{
			name: "a misspelled key is an error",
			yaml: `cases:
  - name: a typo
    sql: SELECT a
    text: SELECT a
    txet: SELECT a
`,
			wantErr: "field txet not found",
		},
		{
			name:    "a suite with no cases is an error",
			yaml:    "name: Empty\n",
			wantErr: "has no cases",
		},
		{
			name: "two cases with one name are an error",
			yaml: `cases:
  - name: twice
    sql: SELECT a
    text: SELECT a
    canonical: SELECT a
  - name: twice
    sql: SELECT b
    text: SELECT b
    canonical: SELECT b
`,
			wantErr: "have the same name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "suite.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))

			suite, err := loadSanitizeSuite(path)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, suite.cases)
		})
	}
}

// TestSuiteEngineLists pins that a case can name the dialects it is valid in, and that an
// empty engine field means all of them.
func TestSuiteEngineLists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "suite.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`cases:
  - name: everywhere
    sql: SELECT 1
    text: SELECT ?
    canonical: SELECT ?
  - name: two of three
    engine: [mysql, starrocks]
    sql: SELECT "x"
    text: SELECT ?
    canonical: SELECT ?
  - name: one of three
    engine: postgres
    sql: SELECT "x" FROM t
    text: SELECT "x" FROM t
    canonical: SELECT "x" FROM t
`), 0o600))

	suite, err := loadSanitizeSuite(path)
	require.NoError(t, err)
	require.Len(t, suite.cases, 3)
	require.Equal(t, []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS}, suite.cases[0].engines)
	require.Equal(t, []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_STARROCKS}, suite.cases[1].engines)
	require.Equal(t, []storepb.Engine{storepb.Engine_POSTGRES}, suite.cases[2].engines)
}
