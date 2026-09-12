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

func TestGetInstaceFromGUID(t *testing.T) {
	if got, ok := GetInstaceFromGUID("inst;db;public;users"); !ok || got != "inst" {
		t.Errorf("GetInstaceFromGUID = (%q, %v), want (\"inst\", true)", got, ok)
	}
	if _, ok := GetInstaceFromGUID("inst"); ok {
		t.Error("GetInstaceFromGUID(\"inst\") should report no separator")
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
