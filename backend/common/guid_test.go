//nolint:revive
package common

import "testing"

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
