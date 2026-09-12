package v1

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The CEL filter parsers turn user input into SQL predicates. They must bind
// every user-controlled value as a query argument instead of splicing it into
// the statement, otherwise a filter like name.matches("x') OR TRUE --") injects
// arbitrary SQL.
func TestFilterParsersDoNotSpliceLiterals(t *testing.T) {
	const payload = `x') OR TRUE --`
	// The raw payload must never appear in the generated SQL.
	const marker = "OR TRUE"

	t.Run("user", func(t *testing.T) {
		find := &store.FindUserMessage{}
		require.NoError(t, parseListUserFilter(find, `name.matches("`+payload+`")`))
		require.NotContains(t, find.Filter.Where, marker)
		require.Equal(t, []any{`%` + strings.ToLower(payload) + `%`}, find.Filter.Args)
	})

	t.Run("instance", func(t *testing.T) {
		filter, err := parseListInstanceFilter(`name.matches("` + payload + `")`)
		require.NoError(t, err)
		require.NotContains(t, filter.Where, marker)
		require.Equal(t, []any{`%` + strings.ToLower(payload) + `%`}, filter.Args)
	})

	t.Run("database-name", func(t *testing.T) {
		filter, err := getListDatabaseFilter(`name.matches("` + payload + `")`)
		require.NoError(t, err)
		require.NotContains(t, filter.Where, marker)
		require.Equal(t, []any{`%` + strings.ToLower(payload) + `%`}, filter.Args)
	})

	t.Run("database-table", func(t *testing.T) {
		filter, err := getListDatabaseFilter(`table.matches("` + payload + `")`)
		require.NoError(t, err)
		require.NotContains(t, filter.Where, marker)
		require.Equal(t, []any{`%` + strings.ToLower(payload) + `%`}, filter.Args)
	})

	t.Run("database-label", func(t *testing.T) {
		// The label key used to be interpolated into db.metadata->'labels'->>'...'.
		filter, err := getListDatabaseFilter(`label == "k':v"`)
		require.NoError(t, err)
		require.NotContains(t, filter.Where, `'k'`)
		require.Equal(t, []any{"k'", []string{"v"}}, filter.Args)
	})
}

// LIKE patterns must escape wildcards in user input so that a literal "%" does
// not turn into a match-anything pattern.
func TestLikePatternEscapesWildcards(t *testing.T) {
	require.Equal(t, `%100\%%`, likePattern("100%"))
	require.Equal(t, `%a\_b%`, likePattern("a_b"))
	require.Equal(t, `%c\\d%`, likePattern(`c\d`))
}
