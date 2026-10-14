package sqlsanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	omnimssql "github.com/bytebase/omni/mssql/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// firstTokenType returns the type and source text of a text's first token.
func firstTokenType(text string) (int, string) {
	lexer := omnimssql.NewLexer(text)
	token := lexer.NextToken()
	if token.Type == 0 || token.End <= token.Loc {
		return 0, ""
	}
	return token.Type, text[token.Loc:token.End]
}

// TestMSSQLTokenRulesAgreeWithTheLexer pins the sanitizer's text rules against the T-SQL
// lexer's own classification, which is the authority the rules approximate. The rules read
// source text because the sanitizer is dialect-neutral and a token's *type* is unexported for
// every dialect but a few string and number constants, so this is where the approximation is
// checked: a token the lexer calls money or a string must be rewritten, and one it calls an
// identifier, a bracketed name or a pseudo-column must not be.
//
// The token types are learned from the lexer rather than named, for the same reason.
func TestMSSQLTokenRulesAgreeWithTheLexer(t *testing.T) {
	t.Parallel()

	moneyType, _ := firstTokenType("$1")
	pseudoType, _ := firstTokenType("$action")
	identifierType, _ := firstTokenType("abc")
	bracketedType, _ := firstTokenType("[a]")
	stringType, _ := firstTokenType("'a'")
	nationalType, _ := firstTokenType("N'a'")
	numberType, _ := firstTokenType("1")

	// mustRewrite maps a token type to what the lexer calls it.
	mustRewrite := map[int]string{
		moneyType: "money", stringType: "string", nationalType: "national string", numberType: "number",
	}
	// mustKeep maps a token type to what the lexer calls it.
	mustKeep := map[int]string{
		identifierType: "identifier", bracketedType: "bracketed name", pseudoType: "pseudo-column",
	}

	// Every currency symbol the matrix can construct, against every money amount shape the
	// lexer accepts — including a sign, which the first version of the rule missed.
	var candidates []string
	for _, symbol := range []string{
		"$", "£", "€", "¥", "¤", "¢", "￥", "＄", "￡", "₭", "₹", "฿", "₫", "₩", "₽", "﷼", "₪", "₴", "₦", "₱", "₡",
	} {
		for _, amount := range []string{"12.50", "-4.78", "+2", ".5", "", "abc", "12abc"} {
			candidates = append(candidates, symbol+amount)
		}
	}
	// The shapes that only look like literals, and the ones that are literals in another form.
	candidates = append(candidates,
		"abc", "名前", "a$b", "_x", "@v", "#t", "[£x]", `"x"`, "'x'", "0x1f", "1e5", ".5", "1.5",
		"NULL", "$action", "$identity", "$partition", "a#b", "£abc", "?", "a", "1", "0x", "@@ROWCOUNT", "N'x'",
	)

	for _, candidate := range candidates {
		tokenType, token := firstTokenType(candidate)
		if tokenType == 0 {
			continue
		}
		if name, ok := mustRewrite[tokenType]; ok {
			// A lone currency symbol with no number is not a literal value: the lexer still calls
			// it money, and leaving it alone is the only sane rewrite — turning `£abc` into `?abc`
			// would fuse a placeholder with an identifier.
			if strings.ContainsAny(token, "0123456789") {
				require.True(t, isLiteral(storepb.Engine_MSSQL, token),
					"the lexer calls %q a %s, but the rules keep the literal: %q", token, name, candidate)
			}
			continue
		}
		if name, ok := mustKeep[tokenType]; ok {
			require.False(t, isLiteral(storepb.Engine_MSSQL, token),
				"the lexer calls %q a %s, but the rules rewrite it: %q", token, name, candidate)
		}
	}
}
