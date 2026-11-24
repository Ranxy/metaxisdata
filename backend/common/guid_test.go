//nolint:revive
package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGUIDPrefix(t *testing.T) {
	tests := []struct {
		guid string
		want string
	}{
		{"inst;db;public;users", "inst;db;public"},
		{"inst;db;users", "inst;db"},
		{"inst;db", "inst"},
		{"inst", ""},
		{"", ""},
		// GUIDs are split on ";" and never on ".", so a dotted name must stay
		// inside its segment.
		{"inst;db;public;my.table", "inst;db;public"},
	}
	for _, tc := range tests {
		if got := GUIDPrefix(tc.guid); got != tc.want {
			t.Errorf("GUIDPrefix(%q) = %q, want %q", tc.guid, got, tc.want)
		}
	}
}

// The instance and the schema are fields of the joined GUID; a GUID too short to
// carry the field reports that instead of returning an empty name.
func TestGUIDFieldExtraction(t *testing.T) {
	t.Parallel()

	instance, ok := GetInstanceFromGUID("inst;db;public;users")
	require.True(t, ok)
	require.Equal(t, "inst", instance)

	schema, ok := GetSchemaFromGUID("inst;db;public;users")
	require.True(t, ok)
	require.Equal(t, "public", schema)

	_, ok = GetInstanceFromGUID("inst")
	require.False(t, ok, "a GUID with no separator has no instance field")
	_, ok = GetSchemaFromGUID("inst;db")
	require.False(t, ok, "a GUID with fewer than three segments has no schema field")
}

// A name containing the separator must not shift the parsed fields. Names
// without it must keep the exact same GUID so stored rows are unaffected.
func TestMetaGUIDEscapesTheSeparator(t *testing.T) {
	t.Parallel()

	plain := BuildMetaGUID("inst", "db", "public", "users")
	require.Equal(t, "inst;db;public;users", plain)

	escaped := BuildMetaGUID("inst", "db;prod", "public", "users")
	require.NotEqual(t, "inst;db;prod;public;users", escaped, "the separator inside a name must be escaped")
	require.Equal(t, []string{"inst", "db;prod", "public", "users"}, SplitMetaGUID(escaped))

	instance, ok := GetInstanceFromGUID(escaped)
	require.True(t, ok)
	require.Equal(t, "inst", instance)
	schema, ok := GetSchemaFromGUID(escaped)
	require.True(t, ok)
	require.Equal(t, "public", schema)

	// The escape character itself must survive a round trip through the joined
	// GUID, together with the separator.
	parts := []string{"inst", "db%3B", "schema;x", "100%"}
	require.Equal(t, parts, SplitMetaGUID(BuildMetaGUID(parts...)))
}

// The escaping must be injective: two different names may never produce the same
// segment. The old encoding escaped ";" but left "%" alone, so a literal "%3B"
// and an escaped ";" collided and two metadata objects shared one GUID.
func TestEscapeGUIDPartIsInjective(t *testing.T) {
	t.Parallel()

	names := []string{
		"",
		"plain",
		"dotted.name",
		";",
		"%",
		"%3B",
		"%3b",
		"%25",
		"db;prod",
		"db%3Bprod",
		"db%253B",
		"a;b%3B;c",
		"%3B;",
		";%3B",
		"100%",
		"%25;%3B",
		"x%3By",
		"%3B%3B",
		"%%",
		";;",
	}
	owners := make(map[string]string, len(names))
	for _, name := range names {
		escaped := EscapeGUIDPart(name)
		previous, duplicate := owners[escaped]
		require.Falsef(t, duplicate, "names %q and %q both escape to %q", previous, name, escaped)
		owners[escaped] = name
		require.Equalf(t, name, UnescapeGUIDPart(escaped), "UnescapeGUIDPart(EscapeGUIDPart(%q))", name)
	}

	// The two names the old encoding aliased must now differ.
	require.NotEqual(t, EscapeGUIDPart("db;prod"), EscapeGUIDPart("db%3Bprod"))
	require.Equal(t, "db%3Bprod", EscapeGUIDPart("db;prod"))
	require.Equal(t, "db%253Bprod", EscapeGUIDPart("db%3Bprod"))
}
