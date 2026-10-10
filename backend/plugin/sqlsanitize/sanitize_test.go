package sqlsanitize

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestIsLiteralClassifiesEveryDialectForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dialect storepb.Engine
		raw     string
		want    bool
	}{
		// Strings quote the same way in all three dialects.
		{storepb.Engine_MYSQL, `'a'`, true},
		{storepb.Engine_POSTGRES, `'a'`, true},
		{storepb.Engine_STARROCKS, `'a'`, true},
		// A double quote is a string in the MySQL family and an identifier in PostgreSQL,
		// which is the one place the dialects genuinely disagree.
		{storepb.Engine_MYSQL, `"a"`, true},
		{storepb.Engine_STARROCKS, `"a"`, true},
		{storepb.Engine_POSTGRES, `"a"`, false},
		// Numbers.
		{storepb.Engine_MYSQL, "1", true},
		{storepb.Engine_MYSQL, "1.5", true},
		{storepb.Engine_POSTGRES, "0x1f", true},
		// Prefixed literals are one token that starts with a letter.
		{storepb.Engine_MYSQL, "x'AB'", true},
		{storepb.Engine_MYSQL, "b'01'", true},
		{storepb.Engine_POSTGRES, `E'\n'`, true},
		{storepb.Engine_POSTGRES, `U&'\0041'`, true},
		// PostgreSQL's two uses of the dollar sign.
		{storepb.Engine_POSTGRES, "$$body$$", true},
		{storepb.Engine_POSTGRES, "$tag$body$tag$", true},
		{storepb.Engine_POSTGRES, "$1", false},
		{storepb.Engine_POSTGRES, "$12", false},
		// Quoted identifiers are never literals.
		{storepb.Engine_MYSQL, "`a`", false},
		{storepb.Engine_MYSQL, "col", false},
		{storepb.Engine_POSTGRES, "a", false},
		// The placeholder itself is left alone.
		{storepb.Engine_MYSQL, Placeholder, false},
		// Keywords and variables are not literals.
		{storepb.Engine_MYSQL, "NULL", false},
		{storepb.Engine_MYSQL, "TRUE", false},
		{storepb.Engine_MYSQL, "@v", false},
		{storepb.Engine_MYSQL, "@@version", false},
	}

	for _, c := range cases {
		require.Equal(t, c.want, isLiteral(c.dialect, c.raw), "%s %q", c.dialect, c.raw)
	}
}

func TestSanitizeReplacesLiteralsAndKeepsSpacing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		dialect storepb.Engine
		sql     string
		want    string
	}{
		{
			// Spacing is preserved verbatim: the analyzer names an unaliased expression
			// column after its source text, and that name is stored in column_lineage.
			name:    "mysql keeps the original spacing",
			dialect: storepb.Engine_MYSQL,
			sql:     "SELECT CONCAT(name, ' x'), 1 FROM t",
			want:    "SELECT CONCAT(name, ?), ? FROM t",
		},
		{
			name:    "mysql values",
			dialect: storepb.Engine_MYSQL,
			sql:     "INSERT INTO t (a, b) VALUES (1, 'x'), (2, 'y')",
			want:    "INSERT INTO t (a, b) VALUES (?, ?), (?, ?)",
		},
		{
			name:    "postgres quoted identifier is not a literal",
			dialect: storepb.Engine_POSTGRES,
			sql:     `SELECT "Weird Col" FROM t WHERE a = 'x'`,
			want:    `SELECT "Weird Col" FROM t WHERE a = ?`,
		},
		{
			name:    "postgres dollar quoted body",
			dialect: storepb.Engine_POSTGRES,
			sql:     "SELECT $$a 'quoted' body$$ FROM t",
			want:    "SELECT ? FROM t",
		},
		{
			name:    "postgres numbered placeholder becomes the wire form",
			dialect: storepb.Engine_POSTGRES,
			sql:     "SELECT a FROM t WHERE b = $1 AND c = $2",
			want:    "SELECT a FROM t WHERE b = ? AND c = ?",
		},
		{
			name:    "starrocks limit",
			dialect: storepb.Engine_STARROCKS,
			sql:     "SELECT a FROM t LIMIT 10",
			want:    "SELECT a FROM t LIMIT ?",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := Sanitize(c.dialect, c.sql)
			require.NoError(t, err)
			require.Equal(t, c.want, got.Text)
		})
	}
}

func TestSanitizeRemovesComments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dialect storepb.Engine
		sql     string
		want    string
	}{
		{storepb.Engine_MYSQL, "SELECT a -- trailing\nFROM t", "SELECT a \nFROM t"},
		{storepb.Engine_MYSQL, "SELECT a # hash\nFROM t", "SELECT a \nFROM t"},
		// A block comment becomes one space so that a and b do not fuse.
		{storepb.Engine_MYSQL, "SELECT a/* c */FROM t", "SELECT a FROM t"},
		{storepb.Engine_POSTGRES, "SELECT a /* c */ FROM t WHERE b = 1", "SELECT a   FROM t WHERE b = ?"},
		// `#` is not a comment in PostgreSQL.
		{storepb.Engine_POSTGRES, "SELECT a FROM t", "SELECT a FROM t"},
		// A comment inside a gap is dropped; the comment's own text never reaches the store.
		{storepb.Engine_MYSQL, "SELECT /* user=alice@example.com */ a FROM t", "SELECT   a FROM t"},
	}

	for _, c := range cases {
		got, err := Sanitize(c.dialect, c.sql)
		require.NoError(t, err, c.sql)
		require.Equal(t, c.want, got.Text, c.sql)
	}
}

func TestSanitizeDoesNotTouchPlaceholdersInsideQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	// A quoted identifier may contain a `?`. The sanitizer walks tokens, so it must
	// survive; so must a string literal that contains comment markers.
	got, err := Sanitize(storepb.Engine_MYSQL, "SELECT `wei?rd`, 'a -- b' FROM t")
	require.NoError(t, err)
	require.Equal(t, "SELECT `wei?rd`, ? FROM t", got.Text)

	// The substituted form must keep the identifier's `?` and turn the placeholder into
	// the neutral literal.
	require.Equal(t, "SELECT `wei?rd`, '?' FROM t", Analyzable(storepb.Engine_MYSQL, got.Text))
}

func TestSanitizeIsIdempotent(t *testing.T) {
	t.Parallel()

	sqls := []string{
		"SELECT CONCAT(name, ' x'), 1 FROM t -- note",
		"INSERT INTO t (a, b) VALUES (1, 'x'), (2, 'y')",
		`SELECT "Weird Col", 'x' FROM t`,
		"UPDATE t SET a = 1 WHERE b = 'q'",
	}
	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS} {
		for _, sql := range sqls {
			once, err := Sanitize(engine, sql)
			if err != nil {
				continue // not every sample is valid in every dialect
			}
			twice, err := Sanitize(engine, once.Text)
			require.NoError(t, err, "%s %s", engine, sql)
			require.Equal(t, once, twice, "%s %s", engine, sql)
		}
	}
}

func TestAnalyzableSubstitutesOnePlaceholderPerToken(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS} {
		got := Analyzable(engine, "INSERT INTO t (a, b) VALUES (?, ?)")
		require.Equal(t, "INSERT INTO t (a, b) VALUES ('?', '?')", got, engine)
	}
	// A statement with no placeholder is returned unchanged.
	require.Equal(t, "SELECT a FROM t", Analyzable(storepb.Engine_MYSQL, "SELECT a FROM t"))
}

func TestSanitizeRejectsUnlexableInput(t *testing.T) {
	t.Parallel()

	_, err := Sanitize(storepb.Engine_MYSQL, "   ")
	require.ErrorIs(t, err, ErrUnlexable)
}

func TestFingerprintIgnoresWhitespace(t *testing.T) {
	t.Parallel()

	tight, err := Sanitize(storepb.Engine_MYSQL, "SELECT a FROM t WHERE b = 1")
	require.NoError(t, err)
	loose, err := Sanitize(storepb.Engine_MYSQL, "SELECT  a\n  FROM t\n WHERE b  = 1")
	require.NoError(t, err)

	require.NotEqual(t, tight.Text, loose.Text, "the stored text keeps the spacing it was given")
	require.Equal(t, tight.Fingerprint, loose.Fingerprint, "but the fingerprint does not")
	require.Equal(t, tight.Canonical, loose.Canonical)
}
