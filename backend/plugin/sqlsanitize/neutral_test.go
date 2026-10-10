package sqlsanitize_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
)

// TestNeutralLiteralIsTheOnlySubstituteThatParsesEverywhere pins why the package
// substitutes a *string* literal rather than something type-agnostic.
//
// Each shape below is a position the corpus does not reach, and the table records
// whether NULL and 0 can stand in there. Both are rejected in most of them; the MySQL
// family's parser has no placeholder production at all, so a placeholder cannot simply be
// left in place either. A string literal is the only substitute that clears every
// position, which is what collapsed the dialect-specific derivation the design first
// proposed into one rule.
func TestNeutralLiteralIsTheOnlySubstituteThatParsesEverywhere(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		engine     storepb.Engine
		sql        string // `?` marks where one placeholder sits
		nullParses bool
		zeroParses bool
		// dollarParses records whether PostgreSQL's own $n fits. It is only meaningful
		// for PostgreSQL cases, and it is what decided the design against a per-dialect
		// derivation: $n works in ordinary DML but not in COPY or INTERVAL.
		dollarParses bool
	}{
		{
			name: "mysql load data", engine: storepb.Engine_MYSQL,
			sql:        "LOAD DATA INFILE ? INTO TABLE employees FIELDS TERMINATED BY ? (id, name)",
			nullParses: false, zeroParses: false,
		},
		{
			name: "mysql json_table path", engine: storepb.Engine_MYSQL,
			sql:        "SELECT * FROM t, JSON_TABLE(t.doc, ? COLUMNS (x INT PATH ?)) AS jt",
			nullParses: false, zeroParses: false,
		},
		{
			name: "mysql insert", engine: storepb.Engine_MYSQL,
			sql:        "INSERT INTO t (a, b) VALUES (?, ?)",
			nullParses: true, zeroParses: true,
		},
		{
			name: "mysql limit", engine: storepb.Engine_MYSQL,
			sql:        "SELECT a FROM t LIMIT ?",
			nullParses: true, zeroParses: true,
		},
		{
			// PostgreSQL's COPY takes NULL but not 0, the one place the two differ.
			name: "postgres copy", engine: storepb.Engine_POSTGRES,
			sql:        "COPY employees (id, name) FROM ?",
			nullParses: true, zeroParses: false, dollarParses: false,
		},
		{
			name: "postgres interval", engine: storepb.Engine_POSTGRES,
			sql:        "DELETE FROM users WHERE status = ? AND last_login < NOW() - INTERVAL ?",
			nullParses: false, zeroParses: false, dollarParses: false,
		},
		{
			name: "postgres limit", engine: storepb.Engine_POSTGRES,
			sql:        "SELECT a FROM t LIMIT ?",
			nullParses: true, zeroParses: true, dollarParses: true,
		},
		{
			name: "starrocks copy into", engine: storepb.Engine_STARROCKS,
			sql:        "COPY INTO t1 FROM ?",
			nullParses: false, zeroParses: false,
		},
		{
			name: "starrocks insert", engine: storepb.Engine_STARROCKS,
			sql:        "INSERT INTO t (a, b) VALUES (?, ?)",
			nullParses: true, zeroParses: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			// What the package produces must parse, everywhere, with no exceptions.
			_, err := corpusAnalyzer.Analyze(context.Background(), c.engine,
				sqlsanitize.Analyzable(c.engine, c.sql))
			require.NoError(t, err, "the package's own substitution must parse")

			require.Equal(t, c.nullParses, parses(c.engine, strings.ReplaceAll(c.sql, "?", "NULL")),
				"NULL in %s", c.sql)
			require.Equal(t, c.zeroParses, parses(c.engine, strings.ReplaceAll(c.sql, "?", "0")),
				"0 in %s", c.sql)

			// PostgreSQL's numbered placeholder is no better a substitute: it fails in
			// COPY and INTERVAL, which is what retired the per-dialect derivation.
			if c.engine == storepb.Engine_POSTGRES {
				require.Equal(t, c.dollarParses, parses(c.engine, numberPlaceholders(c.sql)),
					"$n in %s", c.sql)
			}
		})
	}
}

func parses(engine storepb.Engine, sql string) bool {
	_, err := corpusAnalyzer.Analyze(context.Background(), engine, sql)
	return err == nil
}

// numberPlaceholders rewrites `?` into $1, $2, ... in order, which is how PostgreSQL's
// own drivers write a prepared statement.
func numberPlaceholders(sql string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' {
			n++
			_, _ = fmt.Fprintf(&b, "$%d", n)
			continue
		}
		_ = b.WriteByte(sql[i])
	}
	return b.String()
}
