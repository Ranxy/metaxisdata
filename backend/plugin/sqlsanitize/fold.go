package sqlsanitize

import "strings"

// The rules in this file fold the canonical form — the fingerprint's input — and never the stored
// text, which is what a person reads. There are two of them, and both fold a *list*: an IN list
// whose length depends on how many values the application bound, and a VALUES list whose length
// depends on how many rows it batched. Those are the two shapes that vary from execution to
// execution, so without these rules the table would grow with the application's execution pattern
// rather than with its source code.
//
// Nothing else folds. A constant is rewritten to `?` and left where it is, so the operators around
// it and the number of placeholders a statement carries both stay part of its identity: a
// fingerprint that swallowed them would give `price * 100 >= 5000` and `price * 100 <= 2000` one
// row, and `id = ?` and `id = ? - ?` — one bound value against two — one row as well. That is the
// rule the reference fingerprinter in this space states as "we only replace numbers, quoted
// strings, and value lists".
//
// Each folding is idempotent, which the corpus test asserts: folding an already folded form has to
// leave it alone, or a statement reported twice would be two rows.

// foldInLists collapses an IN / NOT IN list whose elements are all placeholders into one element:
// `IN (?, ?, ?)` becomes `IN (?)`, so an ORM building a list of varying length is one statement.
//
// Only an all-Placeholder list folds. `IN (a, b)` names columns and keeps its shape, a mixed list is
// left alone, and a subquery is neither, so two statements that ask about different things stay
// distinct.
func foldInLists(raws []string) []string {
	out := make([]string, 0, len(raws))
	for i := 0; i < len(raws); i++ {
		if !isKeyword(raws[i], "IN") {
			out = append(out, raws[i])
			continue
		}
		end, ok := placeholderGroupEnd(raws, i+1)
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
// The first group keeps its arity, so two inserts that write a different number of columns stay
// distinct.
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

// placeholderGroupEnd matches a parenthesized group of placeholders, `( ? (, ?)* )`, at open and
// returns the index of its closing parenthesis. It is what both list rules ask, because a list
// folds only when every element of it is a placeholder: anything else — a column, a call, a
// subquery, a literal the sanitizer did not rewrite — is a shape of its own.
func placeholderGroupEnd(raws []string, open int) (int, bool) {
	if open >= len(raws) || raws[open] != "(" {
		return 0, false
	}
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

// isKeyword reports whether a token spells a keyword, whatever its case.
func isKeyword(raw, keyword string) bool {
	return strings.EqualFold(raw, keyword)
}
