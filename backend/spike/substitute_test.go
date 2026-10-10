package spike

import (
	"context"
	"fmt"
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestSubstituteHypothesis checks the shape of every refusal the corpus run
// found: each one is a position where the grammar demands a string literal, so
// NULL and $n are rejected while a string is not. If a string works everywhere,
// the neutral substitute is one rule for every dialect instead of three.
func TestSubstituteHypothesis(_ *testing.T) {
	cases := []struct {
		engine storepb.Engine
		shape  string // with %s where the substitute goes
	}{
		{storepb.Engine_MYSQL, "LOAD DATA INFILE %s INTO TABLE employees FIELDS TERMINATED BY %s (id, name)"},
		{storepb.Engine_MYSQL, "SELECT * FROM t, JSON_TABLE(t.doc, %s COLUMNS (x INT PATH %s)) AS jt"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a, b) VALUES (%s, %s)"},
		{storepb.Engine_MYSQL, "SELECT a FROM t LIMIT %s"},
		{storepb.Engine_MYSQL, "UPDATE t SET a = %s WHERE b = %s"},
		{storepb.Engine_MYSQL, "SELECT DATE_ADD(d, INTERVAL %s DAY) FROM t"},
		{storepb.Engine_MYSQL, "DELETE FROM t WHERE a = %s LIMIT %s"},
		{storepb.Engine_POSTGRES, "COPY employees (id, name) FROM %s"},
		{storepb.Engine_POSTGRES, "DELETE FROM users WHERE status = %s AND last_login < NOW() - INTERVAL %s"},
		{storepb.Engine_POSTGRES, "SELECT a FROM t WHERE c = %s"},
		{storepb.Engine_POSTGRES, "SELECT a FROM t LIMIT %s"},
		{storepb.Engine_POSTGRES, "UPDATE t SET a = %s WHERE b = %s"},
		{storepb.Engine_STARROCKS, "COPY INTO t1 FROM %s"},
		{storepb.Engine_STARROCKS, "INSERT INTO t (a, b) VALUES (%s, %s)"},
		{storepb.Engine_STARROCKS, "SELECT a FROM t WHERE c = %s"},
		{storepb.Engine_STARROCKS, "SELECT a FROM t LIMIT %s"},
	}
	// For PostgreSQL the numbered form is what we currently derive; test it too.
	subs := []string{"NULL", "''", "0", "'?'", "'x'"}
	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_STARROCKS} {
		for _, sub := range subs {
			fails := 0
			for _, c := range cases {
				if c.engine != engine {
					continue
				}
				sql := replaceN(c.shape, sub)
				if engine == storepb.Engine_POSTGRES {
					// Also try the $n form the design currently derives.
					for _, variant := range []string{sql, replaceN(c.shape, "$PLACEHOLDER")} {
						v := variant
						if variant != sql {
							v = numberPlaceholders(c.shape)
						}
						if _, err := analyzer.Analyze(context.Background(), engine, v); err != nil {
							fails++
							fmt.Printf("  FAIL engine=%-9s sub=%-4s %s\n      -> %v\n", engine, sub, v, firstLine(err))
						}
					}
					continue
				}
				if _, err := analyzer.Analyze(context.Background(), engine, sql); err != nil {
					fails++
					fmt.Printf("  FAIL engine=%-9s sub=%-4s %s\n      -> %v\n", engine, sub, sql, firstLine(err))
				}
			}
			fmt.Printf("=== %-9s substitute %-4s : %d failures\n", engine, sub, fails)
		}
	}

	// Does the analyzer accept the substituted form, which is the real gate?
	for _, c := range cases {
		for _, sub := range subs {
			sql := replaceN(c.shape, sub)
			if _, err := analyzer.Analyze(context.Background(), c.engine, sql); err != nil {
				fmt.Printf("  ANALYZER rejects engine=%-9s sub=%-4s %s -> %v\n", c.engine, sub, sql, firstLine(err))
			}
		}
	}
}

func replaceN(shape, sub string) string {
	out := ""
	for i := 0; i < len(shape); i++ {
		if i+1 < len(shape) && shape[i] == '%' && shape[i+1] == 's' {
			out += sub
			i++
			continue
		}
		out += string(shape[i])
	}
	return out
}

func numberPlaceholders(shape string) string {
	out, n := "", 0
	for i := 0; i < len(shape); i++ {
		if i+1 < len(shape) && shape[i] == '%' && shape[i+1] == 's' {
			n++
			out += fmt.Sprintf("$%d", n)
			i++
			continue
		}
		out += string(shape[i])
	}
	return out
}

func firstLine(err error) string {
	for i := 0; i < len(err.Error()); i++ {
		if err.Error()[i] == '\n' {
			return err.Error()[:i]
		}
	}
	return err.Error()
}
