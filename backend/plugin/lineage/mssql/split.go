package mssql

import (
	omnimssql "github.com/bytebase/omni/mssql"
	nodes "github.com/bytebase/omni/mssql/ast"
)

// SplitStatements splits a T-SQL script into the statements it contains, dropping
// the ones that carry no SQL.
//
// The parse is the primary splitter rather than a lexical scan, because T-SQL
// settles two things only a parser can: a `;` inside a CREATE PROCEDURE body ends a
// statement inside the batch, not the batch, and `GO` is a client-side batch
// separator that is neither a terminator nor SQL. The analyzer is handed one
// statement at a time, so a script the parser accepts is cut exactly the way the
// parser saw it.
//
// A script the parser rejects falls back to a lexical scan. That fallback is the
// whole point of the split contract: a splitter that returned nothing for
// unparseable text would turn "could not analyze" into the silent answer "this
// script has no lineage" (see backend/plugin/lineage/lineage.go).
func SplitStatements(sql string) []string {
	if statements, err := omnimssql.Parse(sql); err == nil {
		out := make([]string, 0, len(statements))
		usable := true
		for _, statement := range statements {
			if nodeOrNil(statement.AST) == nil || isBatchSeparator(statement.AST) {
				continue
			}
			// A statement's text span is derived from the previous one's end offset, and
			// a statement whose Loc has no end makes the next one's span empty. Falling
			// back to the lexical scan then keeps every statement, where returning this
			// list would hand the analyzer an empty string to parse.
			if !carriesSQL(statement.Text) {
				usable = false
				break
			}
			out = append(out, statement.Text)
		}
		if usable && len(out) > 0 {
			return out
		}
	}
	return splitLexically(sql)
}

// isDataModification reports whether a statement writes stored rows.
func isDataModification(node nodes.Node) bool {
	switch nodeOrNil(node).(type) {
	case *nodes.InsertStmt, *nodes.UpdateStmt, *nodes.DeleteStmt, *nodes.MergeStmt:
		return true
	default:
		return false
	}
}

// isBatchSeparator reports whether a parsed statement is the `GO` batch separator.
// It carries no SQL and is not a statement an analyzer can refuse.
func isBatchSeparator(node nodes.Node) bool {
	_, ok := node.(*nodes.GoStmt)
	return ok
}

// splitLexically cuts a script on statement terminators. It understands the one
// thing that matters at this level: a terminator inside a string literal, a quoted
// identifier, a comment or a variable name is not a terminator.
func splitLexically(sql string) []string {
	var out []string
	start := 0
	for i := 0; i < len(sql); {
		switch {
		case sql[i] == '\'':
			i = skipQuoted(sql, i, '\'')
		case sql[i] == '"':
			i = skipQuoted(sql, i, '"')
		case sql[i] == '[':
			i = skipBracketed(sql, i)
		case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
			i = skipLineComment(sql, i)
		case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i = skipBlockComment(sql, i)
		case sql[i] == ';':
			out = appendStatement(out, sql[start:i])
			i++
			start = i
		default:
			i++
		}
	}
	return appendStatement(out, sql[start:])
}

// appendStatement keeps a segment only when it carries SQL: a blank line and a lone
// comment are not statements, and analyzing them would report a parse failure for
// text that was never SQL.
func appendStatement(out []string, segment string) []string {
	if carriesSQL(segment) {
		out = append(out, segment)
	}
	return out
}

// carriesSQL reports whether a segment holds anything but whitespace and comments.
func carriesSQL(segment string) bool {
	for i := 0; i < len(segment); {
		switch {
		case segment[i] == ' ' || segment[i] == '\t' || segment[i] == '\r' || segment[i] == '\n':
			i++
		case segment[i] == '-' && i+1 < len(segment) && segment[i+1] == '-':
			i = skipLineComment(segment, i)
		case segment[i] == '/' && i+1 < len(segment) && segment[i+1] == '*':
			i = skipBlockComment(segment, i)
		default:
			return true
		}
	}
	return false
}

// skipQuoted returns the offset past a quoted run that starts at i. A doubled quote
// character inside is one quote and does not end it; an unterminated run ends at the
// end of the text.
func skipQuoted(text string, i int, quote byte) int {
	i++
	for i < len(text) {
		if text[i] != quote {
			i++
			continue
		}
		if i+1 < len(text) && text[i+1] == quote {
			i += 2
			continue
		}
		return i + 1
	}
	return i
}

// skipBracketed returns the offset past a bracketed identifier. A doubled `]]`
// inside is one `]`.
func skipBracketed(text string, i int) int {
	i++
	for i < len(text) {
		if text[i] != ']' {
			i++
			continue
		}
		if i+1 < len(text) && text[i+1] == ']' {
			i += 2
			continue
		}
		return i + 1
	}
	return i
}

func skipLineComment(text string, i int) int {
	for i < len(text) && text[i] != '\n' {
		i++
	}
	return i
}

// skipBlockComment returns the offset past a block comment. T-SQL block comments
// nest, so the scan counts depth: a terminator inside a commented-out statement must
// not split the script.
func skipBlockComment(text string, i int) int {
	depth := 0
	for i < len(text) {
		switch {
		case text[i] == '/' && i+1 < len(text) && text[i+1] == '*':
			depth++
			i += 2
		case text[i] == '*' && i+1 < len(text) && text[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return i
}
