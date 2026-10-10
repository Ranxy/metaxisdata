// Package spike holds throwaway probes for the application-SQL ingestion design.
// Delete the whole directory once the findings are recorded in .agents/docs.
package spike

import (
	"fmt"
	"testing"

	omnimysql "github.com/bytebase/omni/mysql/parser"
	omnipg "github.com/bytebase/omni/pg/parser"
)

// TestLexProbe asks: can a sanitizer classify a token from input[Loc:End] alone,
// without depending on omni's unexported token-type constants?
func TestLexProbe(t *testing.T) {
	mysqlSQL := "SELECT 'a', 1, 2.5, x'AB', b'01', `id`, \"dq\", NULL, TRUE, ? -- c1\n, @v /* c2 */ FROM t WHERE a >= 1"
	fmt.Println("=== MySQL tokens: raw source slice per token ===")
	l := omnimysql.NewLexer(mysqlSQL)
	for i := 0; i < 60; i++ {
		tok := l.NextToken()
		if tok.Type == 0 && tok.End == 0 {
			break
		}
		raw := ""
		if tok.Loc >= 0 && tok.End <= len(mysqlSQL) && tok.Loc <= tok.End {
			raw = mysqlSQL[tok.Loc:tok.End]
		}
		fmt.Printf("  type=%-4d loc=%-3d end=%-3d str=%-8q raw=%q\n", tok.Type, tok.Loc, tok.End, tok.Str, raw)
	}

	pgSQL := "SELECT 'a', E'b\\n', 1, 2.5, x'AB', b'01', \"id\", $1, $$dollar$$, $tag$d$tag$, NULL, $2 FROM t WHERE a >= 1"
	fmt.Println("=== PostgreSQL tokens: raw source slice per token ===")
	pl := omnipg.NewLexer(pgSQL)
	for i := 0; i < 60; i++ {
		tok := pl.NextToken()
		if tok.Type == 0 && tok.End == 0 {
			break
		}
		raw := ""
		if tok.Loc >= 0 && tok.End <= len(pgSQL) && tok.Loc <= tok.End {
			raw = pgSQL[tok.Loc:tok.End]
		}
		fmt.Printf("  type=%-6d loc=%-3d end=%-3d str=%-10q raw=%q\n", tok.Type, tok.Loc, tok.End, tok.Str, raw)
	}
}
