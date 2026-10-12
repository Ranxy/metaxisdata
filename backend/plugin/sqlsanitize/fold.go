package sqlsanitize

import "strings"

// The rules in this file fold the canonical form — the fingerprint's input — and never the stored
// text, which is what a person reads. They exist because one statement in an application's code
// can otherwise become one row per shape of its literals: an IN list of varying length, a batch
// insert of varying size, a constant written differently. Without them the table grows with the
// application's execution pattern rather than with its source code.
//
// Each folding is idempotent, which the corpus test asserts: folding an already folded form has to
// leave it alone, or a statement reported twice would be two rows.

// foldInLists collapses an IN / NOT IN list whose elements are all placeholders into one element:
// `IN (?, ?, ?)` becomes `IN (?)`, so an ORM building a list of varying length is one statement.
//
// Only an all-Placeholder list folds. `IN (a, b)` names columns and keeps its shape, and a mixed
// list is left alone, so two statements that select different columns stay distinct.
func foldInLists(raws []string) []string {
	out := make([]string, 0, len(raws))
	for i := 0; i < len(raws); i++ {
		if !isKeyword(raws[i], "IN") || i+1 >= len(raws) || raws[i+1] != "(" {
			out = append(out, raws[i])
			continue
		}
		end, ok := placeholderListEnd(raws, i+1)
		if !ok {
			out = append(out, raws[i])
			continue
		}
		out = append(out, raws[i], "(", Placeholder, ")")
		i = end
	}
	return out
}

// foldValueGroups collapses the repeated row groups of a VALUES list into the first one:
// `VALUES (?, ?), (?, ?)` becomes `VALUES (?, ?)`, so a batch insert of varying size is one
// statement.
//
// The first group keeps its arity, so two inserts with a different column count stay distinct.
func foldValueGroups(raws []string) []string {
	out := make([]string, 0, len(raws))
	for i := 0; i < len(raws); i++ {
		if !isKeyword(raws[i], "VALUES") {
			out = append(out, raws[i])
			continue
		}
		firstEnd, ok := placeholderGroupEnd(raws, i+1)
		if !ok {
			out = append(out, raws[i])
			continue
		}
		last := firstEnd
		for last+2 < len(raws) && raws[last+1] == "," {
			nextEnd, ok := placeholderGroupEnd(raws, last+2)
			if !ok {
				break
			}
			last = nextEnd
		}
		out = append(out, raws[i])
		out = append(out, raws[i+1:firstEnd+1]...)
		i = last
	}
	return out
}

// foldConstantRuns collapses a run that begins with a Placeholder and holds only placeholders and
// operators into one Placeholder: `1 + 1`, `2` and `5 - 3` are one statement.
//
// The run has to *begin* with a Placeholder. Without that rule the run following an identifier
// would fold too: `WHERE a - 1 > 0` is `a - ? > ?`, whose run would be `- ? > ?` and would collapse
// to `a ?` — colliding with statements that share nothing but the identifier. Beginning at the
// Placeholder leaves `a - ?` instead.
//
// A keyword ends a run, which is what keeps `a = ? AND b = ?` two comparisons; `NULL` is a keyword
// too, so `a = NULL` stays distinct from `a = ?`. A comma and a parenthesis end a run, which is
// what keeps `f(?, ?)` a call of two arguments.
func foldConstantRuns(raws []string) []string {
	out := make([]string, 0, len(raws))
	for i := 0; i < len(raws); i++ {
		if raws[i] != Placeholder {
			out = append(out, raws[i])
			continue
		}
		end, placeholders := i, 0
		for j := i; j < len(raws) && isOperator(raws[j]); j++ {
			if raws[j] == Placeholder {
				placeholders++
			}
			end = j
		}
		if placeholders < 2 {
			out = append(out, raws[i])
			continue
		}
		out = append(out, Placeholder)
		i = end
	}
	return out
}

// placeholderListEnd matches `( ? (, ?)* )` at open and returns the index of the closing
// parenthesis.
func placeholderListEnd(raws []string, open int) (int, bool) {
	i := open + 1
	if i >= len(raws) || raws[i] != Placeholder {
		return 0, false
	}
	i++
	for i+1 < len(raws) && raws[i] == "," && raws[i+1] == Placeholder {
		i += 2
	}
	if i >= len(raws) || raws[i] != ")" {
		return 0, false
	}
	return i, true
}

// placeholderGroupEnd matches a `( ? (, ?)* )` group as a VALUES row group. It reads only the
// parenthesis, because a group of a VALUES list is the same shape as an IN list; the two rules
// differ in which list they were called for, not in what they match.
func placeholderGroupEnd(raws []string, open int) (int, bool) {
	if open >= len(raws) || raws[open] != "(" {
		return 0, false
	}
	return placeholderListEnd(raws, open)
}

// isKeyword reports whether a token spells a keyword, whatever its case.
func isKeyword(raw, keyword string) bool {
	return strings.EqualFold(raw, keyword)
}

// isOperator reports whether a token may sit inside a constant run: a Placeholder, or a token that
// is not a word, a parenthesis or a comma.
//
// The test is on the token's shape, not on a per-dialect operator table, which omni does not
// export: a token starting with a letter or an underscore is an identifier or a keyword and ends
// the run, and so does a quoted name. Everything else — `+`, `-`, `->>`, `||`, `%`, `::` — is an
// operator here, and it only ever extends a run that already began at a Placeholder.
func isOperator(raw string) bool {
	if raw == Placeholder {
		return true
	}
	switch raw {
	case ",", "(", ")", "":
		return false
	}
	c := raw[0]
	switch {
	case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return false
	case c == '\'', c == '"', c == '`':
		return false
	}
	return true
}
