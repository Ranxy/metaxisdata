package doris

import (
	"testing"

	"github.com/stretchr/testify/require"

	nodes "github.com/bytebase/omni/doris/ast"
	dorisparser "github.com/bytebase/omni/doris/parser"
)

// tableFunctionCall parses a FROM table function through a statement, because that
// is how the analyzer sees it.
func tableFunctionCall(t *testing.T, call string) *nodes.FuncCallExpr {
	t.Helper()

	file, err := dorisparser.Parse("SELECT * FROM " + call + " u")
	require.NoError(t, err, call)
	ref, ok := file.Stmts[0].(*nodes.SelectStmt).From[0].(*nodes.TableRef)
	require.True(t, ok, call)
	require.NotNil(t, ref.Func, call)
	return ref.Func
}

// TestTableFunctionColumn pins the name a FROM table function exposes. Doris gives
// it no column list and its arguments are named, and the engine names the column
// after the argument's key: `SELECT u.number FROM numbers("number" = "2") u` works
// on 4.1.4 while `u.numbers` is `Unknown column 'numbers' in 'u'`. A call whose
// arguments name nothing keeps the function's own name as the placeholder, because
// the schema of a table function this analysis cannot read is exactly unknown.
func TestTableFunctionColumn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		call string
		want string
	}{
		{call: `numbers("number" = "2")`, want: "number"},
		{call: `numbers("number" = 2)`, want: "number"},
		{call: `explode(t.arr)`, want: "explode"},
		{call: `f()`, want: "f"},
		{call: `numbers(1)`, want: "numbers"},
		{call: `numbers("number")`, want: "numbers"},
		{call: `f("a" = "1", "b" = "2")`, want: "f"},
	} {
		call := tableFunctionCall(t, tc.call)
		require.Equal(t, tc.want, tableFunctionColumn(call, funcCallName(call)), "call=%s", tc.call)
	}
}

// TestTableFunctionNamedArgumentNamesItsColumn pins the wiring: the lineage a
// generator carries is named after the column the relation exposes, so a named
// argument's key names it rather than the function. Doris restricts these arguments
// to literals, so this column-valued shape is one omni accepts and the engine
// rejects; the LATERAL VIEW form is the generator that reads a column on a
// statement Doris accepts.
func TestTableFunctionNamedArgumentNamesItsColumn(t *testing.T) {
	t.Parallel()

	requireEdges(t, `SELECT * FROM tv2, explode_numbers("number" = tv2.id) u`,
		[]string{"tv2.* -> __result__.*", "tv2.id -> __result__.number"})
	requireEdges(t, `SELECT u.number FROM tv2, explode_numbers("number" = tv2.id) u`,
		[]string{"tv2.id -> __result__.number"})
}
