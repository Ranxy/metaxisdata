package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

// The four list methods share one CEL->SQL translator. These cases pin the
// grammar it accepts and the placeholder style it emits, so a field handler
// cannot start splicing a literal into the WHERE clause unnoticed.
func TestTranslateFilterGrammar(t *testing.T) {
	t.Parallel()

	fields := map[string]filterField{
		"title": likeField("t.title", true),
		"state": equalField("t.state"),
		"kind":  enumField("t.kind", func(name string) (any, error) { return "store/" + name, nil }, true),
	}

	tests := []struct {
		name      string
		filter    string
		where     string
		args      []any
		expectErr bool
	}{
		{name: "empty filter is ignored", filter: "", where: "", args: nil},
		{name: "blank filter is ignored", filter: "   ", where: "", args: nil},
		{name: "equality", filter: `title == "abc"`, where: `(t.title = $1)`, args: []any{"abc"}},
		{name: "matches folds case", filter: `title.matches("ABC")`, where: `(LOWER(t.title) LIKE $1)`, args: []any{"%abc%"}},
		{name: "conjunction", filter: `title == "a" && state == "b"`, where: `((t.title = $1) AND (t.state = $2))`, args: []any{"a", "b"}},
		{name: "disjunction", filter: `title == "a" || state == "b"`, where: `((t.title = $1) OR (t.state = $2))`, args: []any{"a", "b"}},
		{name: "in list", filter: `kind in ["A", "B"]`, where: `(t.kind IN ($1, $2))`, args: []any{"store/A", "store/B"}},
		{name: "negated in list", filter: `!(kind in ["A"])`, where: `(t.kind NOT IN ($1))`, args: []any{"store/A"}},
		{name: "unknown variable", filter: `other == "a"`, expectErr: true},
		{name: "matches on a field without like support", filter: `state.matches("a")`, expectErr: true},
		{name: "in on a field without list support", filter: `state in ["a"]`, expectErr: true},
		{name: "negation of a non-list expression", filter: `!(state == "a")`, expectErr: true},
		{name: "unsupported operator", filter: `state > "a"`, expectErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filter, err := translateFilter(tc.filter, fields)
			if tc.expectErr {
				require.Error(t, err)
				require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}
			require.NoError(t, err)
			if tc.where == "" {
				require.Nil(t, filter)
				return
			}
			require.Equal(t, tc.where, filter.Where)
			require.Equal(t, tc.args, filter.Args)
		})
	}
}

// The engine list filter used to paste `'MYSQL','POSTGRES'` into the statement.
// It must bind values like every other predicate, and the bound value must be
// the enum name because the metadata column is protojson.
func TestInstanceEngineInFilterIsParameterized(t *testing.T) {
	t.Parallel()

	filter, err := parseListInstanceFilter(`engine in ["MYSQL", "POSTGRES"]`)
	require.NoError(t, err)
	require.Equal(t, `(instance.metadata->>'engine' IN ($1, $2))`, filter.Where)
	require.Equal(t, []any{"MYSQL", "POSTGRES"}, filter.Args)
}

// The engine filter compared the text column against the enum's number, so it
// silently matched nothing. It must bind the enum name that protojson wrote.
func TestEngineFilterBindsTheEnumName(t *testing.T) {
	t.Parallel()

	filter, err := parseListInstanceFilter(`engine == "MYSQL"`)
	require.NoError(t, err)
	require.Equal(t, `(instance.metadata->>'engine' = $1)`, filter.Where)
	require.Equal(t, []any{"MYSQL"}, filter.Args)

	databaseFilter, err := getListDatabaseFilter(`engine == "TIDB"`)
	require.NoError(t, err)
	require.Equal(t, []any{"TIDB"}, databaseFilter.Args)

	_, err = parseListInstanceFilter(`engine == "MONGODB"`)
	require.Error(t, err, "an engine the product no longer supports is rejected")
}
