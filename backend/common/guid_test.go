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

func TestGetInstanceFromGUID(t *testing.T) {
	if got, ok := GetInstanceFromGUID("inst;db;public;users"); !ok || got != "inst" {
		t.Errorf("GetInstanceFromGUID = (%q, %v), want (\"inst\", true)", got, ok)
	}
	if _, ok := GetInstanceFromGUID("inst"); ok {
		t.Error("GetInstanceFromGUID(\"inst\") should report no separator")
	}
}

func TestGetSchemaFromGUID(t *testing.T) {
	if got, ok := GetSchemaFromGUID("inst;db;public;users"); !ok || got != "public" {
		t.Errorf("GetSchemaFromGUID = (%q, %v), want (\"public\", true)", got, ok)
	}
	if _, ok := GetSchemaFromGUID("inst;db"); ok {
		t.Error("GetSchemaFromGUID(\"inst;db\") should report too few segments")
	}
}

// A name containing the separator must not shift the parsed fields. Names
// without it must keep the exact same GUID so stored rows are unaffected.
func TestMetaGUIDEscapesTheSeparator(t *testing.T) {
	plain := BuildMetaGUID("inst", "db", "public", "users")
	if plain != "inst;db;public;users" {
		t.Fatalf("BuildMetaGUID without a separator = %q, want the plain join", plain)
	}

	escaped := BuildMetaGUID("inst", "db;prod", "public", "users")
	if escaped == "inst;db;prod;public;users" {
		t.Fatalf("BuildMetaGUID did not escape the separator: %q", escaped)
	}
	parts := SplitMetaGUID(escaped)
	if len(parts) != 4 {
		t.Fatalf("SplitMetaGUID(%q) = %v, want 4 segments", escaped, parts)
	}
	if parts[1] != "db;prod" {
		t.Fatalf("database segment = %q, want %q", parts[1], "db;prod")
	}

	if got, ok := GetInstanceFromGUID(escaped); !ok || got != "inst" {
		t.Fatalf("GetInstanceFromGUID(%q) = (%q, %v)", escaped, got, ok)
	}
	if got, ok := GetSchemaFromGUID(escaped); !ok || got != "public" {
		t.Fatalf("GetSchemaFromGUID(%q) = (%q, %v)", escaped, got, ok)
	}
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

// BuildMetaGUID/SplitMetaGUID are the pair that must stay symmetric: a name
// containing the separator or the escape character has to survive a round trip
// through a joined GUID.
func TestMetaGUIDRoundTripsNamesWithTheEscapeCharacter(t *testing.T) {
	t.Parallel()

	parts := []string{"inst", "db%3B", "schema;x", "100%"}
	guid := BuildMetaGUID(parts...)
	require.Equal(t, parts, SplitMetaGUID(guid))
	require.Equal(t, []string{"inst", "db%3B", "schema;x", "100%"}, SplitMetaGUID(guid))
}
