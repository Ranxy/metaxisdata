package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScopes(t *testing.T) {
	t.Parallel()

	scopes, err := ParseScopes("dev=1;shop, prod=9;shop;public ,1;other")
	require.NoError(t, err)
	require.Equal(t, []Scope{
		{Name: "dev", GUID: "1;shop"},
		{Name: "prod", GUID: "9;shop;public"},
		{Name: "", GUID: "1;other"},
	}, scopes)
}

func TestParseScopesRejectsMalformedEntries(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"=1;shop", "dev=", "="} {
		_, err := ParseScopes(raw)
		require.Error(t, err, "input %q", raw)
	}

	// "dev" alone is a bare GUID, not an error: a scope may go unnamed.
	scopes, err := ParseScopes("dev")
	require.NoError(t, err)
	require.Equal(t, []Scope{{GUID: "dev"}}, scopes)

	empty, err := ParseScopes("")
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestSelectReturnsEverythingByDefault(t *testing.T) {
	t.Parallel()

	configured := []Scope{{Name: "dev", GUID: "1;shop"}, {Name: "prod", GUID: "9;shop"}}
	selected, err := Select(configured, nil)
	require.NoError(t, err)
	require.Equal(t, configured, selected)
}

func TestSelectByNameGUIDAndAll(t *testing.T) {
	t.Parallel()

	configured := []Scope{{Name: "dev", GUID: "1;shop"}, {Name: "prod", GUID: "9;shop"}, {Name: "stage", GUID: "5;shop"}}

	byName, err := Select(configured, []string{"prod"})
	require.NoError(t, err)
	require.Equal(t, []Scope{{Name: "prod", GUID: "9;shop"}}, byName)

	// A raw GUID is accepted without being declared, which is how a one-off
	// query against an unlisted database works.
	byGUID, err := Select(configured, []string{"7;shop;public"})
	require.NoError(t, err)
	require.Equal(t, []Scope{{GUID: "7;shop;public"}}, byGUID)

	// Several values, and a comma separated value, are equivalent.
	both, err := Select(configured, []string{"dev", "prod"})
	require.NoError(t, err)
	require.Len(t, both, 2)
	commaSeparated, err := Select(configured, []string{"dev,prod"})
	require.NoError(t, err)
	require.Equal(t, both, commaSeparated)

	all, err := Select(configured, []string{"all"})
	require.NoError(t, err)
	require.Len(t, all, 3)
}

// Selecting the same scope twice must not analyze the statement twice.
func TestSelectDeduplicates(t *testing.T) {
	t.Parallel()

	configured := []Scope{{Name: "dev", GUID: "1;shop"}}
	selected, err := Select(configured, []string{"dev", "1;shop", "dev"})
	require.NoError(t, err)
	require.Len(t, selected, 1)
}

func TestSelectRejectsAnUnknownName(t *testing.T) {
	t.Parallel()

	_, err := Select([]Scope{{Name: "dev", GUID: "1;shop"}}, []string{"prod"})
	require.ErrorContains(t, err, "unknown scope")
}

func TestLookupIgnoresGUIDs(t *testing.T) {
	t.Parallel()

	configured := []Scope{{Name: "dev", GUID: "1;shop"}}
	_, ok := Lookup(configured, "1;shop")
	require.False(t, ok, "a GUID is not a name; it must not resolve by accident")
}
