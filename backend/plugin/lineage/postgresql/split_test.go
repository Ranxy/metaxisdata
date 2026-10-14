package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSplitStatements pins the two segment shapes the splitter's decisions turn on, both of
// them owned by omni's PostgreSQL lexer rather than by this package.
//
// A `--` comment ends at a carriage return, so the statement after one is a statement of its
// own: reading the `\r` as comment text would fold it into the comment and drop it from the
// analysis silently. A form feed is whitespace, so a segment holding nothing else carries no
// SQL and is not a statement. Both were reached by re-running the corpus after the omni bump.
func TestSplitStatements(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"SELECT 1;", " -- c\r SELECT 2;"},
		SplitStatements("SELECT 1; -- c\r SELECT 2;"))
	require.Equal(t, []string{"SELECT 1;"}, SplitStatements("SELECT 1;\f;"))
	require.Empty(t, SplitStatements("\f"))
}
