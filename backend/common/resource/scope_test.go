package resource

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopeContains(t *testing.T) {
	t.Parallel()

	require.True(t, NewScope(TypeWorkspace, "").Contains(NewRef(TypeQualityRule, "rule-1")))

	database := NewScope(TypeDatabase, "i;db")
	require.True(t, database.Contains(NewRef(TypeDatabase, "i;db")))
	require.True(t, database.Contains(NewRef(TypeTable, "i;db;orders")))
	require.True(t, database.Contains(NewRef(TypeTable, "i;db;public;orders")))
	require.False(t, database.Contains(NewRef(TypeTable, "i;other;orders")))
	require.False(t, database.Contains(NewRef(TypeDatabase, "i;db2")))
	require.False(t, database.Contains(NewRef(TypeQualityRule, "rule-1")))

	instance := NewScope(TypeInstance, "i")
	require.True(t, instance.Contains(NewRef(TypeDatabase, "i;db")))
	require.True(t, instance.Contains(NewRef(TypeTable, "i;db;public;orders")))
	require.False(t, instance.Contains(NewRef(TypeDatabase, "i2;db")))

	table := NewScope(TypeTable, "i;db;orders")
	require.True(t, table.Contains(NewRef(TypeColumn, "i;db;orders;id")))
	require.False(t, table.Contains(NewRef(TypeColumn, "i;db;orders2;id")))
	require.False(t, table.Contains(NewRef(TypeTable, "i;db;orders2")))

	rule := NewScope(TypeQualityRule, "rule-1")
	require.True(t, rule.Contains(NewRef(TypeQualityRule, "rule-1")))
	require.False(t, rule.Contains(NewRef(TypeQualityRule, "rule-2")))
	require.False(t, rule.Contains(NewRef(TypeTable, "i;db;orders")))
}

func TestScopeSetContainsRefs(t *testing.T) {
	t.Parallel()

	set := ScopeSet{Scopes: []Scope{NewScope(TypeDatabase, "i;db")}}
	require.False(t, set.IsEmpty())
	require.True(t, set.Contains(NewRef(TypeTable, "i;db;orders")))
	require.True(t, set.ContainsRefs(MetadataAncestors(NewRef(TypeColumn, "i;db;orders;id"))))
	require.False(t, set.ContainsRefs(MetadataAncestors(NewRef(TypeColumn, "i;other;orders;id"))))

	require.True(t, ScopeSet{All: true}.Contains(NewRef(TypeTable, "other;db;t")))
	require.True(t, ScopeSet{}.IsEmpty())
	require.False(t, ScopeSet{}.Contains(NewRef(TypeTable, "i;db;t")))
}

func TestScopeSetAddAndUnion(t *testing.T) {
	t.Parallel()

	var set ScopeSet
	set.Add(Scope{})
	set.Add(NewScope(TypeDatabase, "i;db"))
	set.Add(NewScope(TypeWorkspace, ""))
	require.True(t, set.All, "a workspace scope widens the set")
	require.False(t, set.IsEmpty())

	var merged ScopeSet
	merged.Add(NewScope(TypeDatabase, "i;db"))
	merged.Union(ScopeSet{Scopes: []Scope{NewScope(TypeInstance, "j")}})
	require.Len(t, merged.Scopes, 2)
}

func TestScopeSetNormalize(t *testing.T) {
	t.Parallel()

	set := ScopeSet{Scopes: []Scope{
		NewScope(TypeTable, "i;db;orders"),
		NewScope(TypeInstance, "i"),
		NewScope(TypeDatabase, "i;db"),
		NewScope(TypeDatabase, "i;db"),
		NewScope(TypeInstance, "j"),
		{},
	}}
	normalized := set.Normalize()
	require.Equal(t, []Scope{
		NewScope(TypeInstance, "i"),
		NewScope(TypeInstance, "j"),
	}, normalized.Scopes)
	require.False(t, normalized.All)

	require.True(t, ScopeSet{Scopes: []Scope{NewScope(TypeWorkspace, "")}}.Normalize().All)
	require.True(t, ScopeSet{All: true, Scopes: []Scope{NewScope(TypeTable, "i;db;t")}}.Normalize().All)
}

func TestScopeSetMetadataScopes(t *testing.T) {
	t.Parallel()

	set := ScopeSet{Scopes: []Scope{
		NewScope(TypeQualityRule, "rule-1"),
		NewScope(TypeDatabase, "i;db"),
		NewScope(TypeTable, "i;db;orders"),
	}}
	require.Equal(t, []Scope{NewScope(TypeDatabase, "i;db")}, set.MetadataScopes())
	require.Nil(t, ScopeSet{All: true}.MetadataScopes())
}

// Self is reserved, not shipped: it makes a set non-empty (so the ACL gate does
// not deny a future self-scoped list) but contains no resource reference, so a
// caller that ignores it fails closed.
func TestScopeSetSelfIsReserved(t *testing.T) {
	t.Parallel()

	self := int32(7)
	set := ScopeSet{Self: &self}
	require.False(t, set.IsEmpty())
	require.False(t, set.Contains(NewRef(TypeTable, "i;db;t")))
	require.Empty(t, set.MetadataScopes())

	// Union keeps the first self id: two principals are not a meaningful union.
	other := int32(9)
	set.Union(ScopeSet{Self: &other})
	require.NotNil(t, set.Self)
	require.Equal(t, int32(7), *set.Self)
}
