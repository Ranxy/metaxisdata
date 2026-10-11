package idp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The claim path is what every field mapping depends on, so a nested claim has
// to resolve and a missing one has to be reported as absent rather than as a
// zero value that would look like a real claim.
func TestGetValueWithKey(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub":   "abc",
		"email": "person@example.com",
		"realm_access": map[string]any{
			"roles": []any{"admin", "auditor"},
		},
		"nested": map[string]any{
			"deeper": map[string]any{"value": "found"},
		},
		"empty": "",
	}

	testCases := []struct {
		name string
		key  string
		want any
	}{
		{name: "top level", key: "sub", want: "abc"},
		{name: "nested list", key: "realm_access.roles", want: []any{"admin", "auditor"}},
		{name: "deeply nested", key: "nested.deeper.value", want: "found"},
		{name: "absent", key: "missing", want: nil},
		{name: "absent nested", key: "realm_access.missing", want: nil},
		{name: "path through a scalar", key: "sub.deeper", want: nil},
		// An empty string is a value, not an absence: a mapping onto it must not
		// be confused with the claim not being there.
		{name: "empty string", key: "empty", want: ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, GetValueWithKey(claims, tc.key))
		})
	}
}

func TestGroupValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		claim    any
		want     []string
		wantRead bool
	}{
		{name: "array of strings", claim: []any{"admin", "auditor"}, want: []string{"admin", "auditor"}, wantRead: true},
		{name: "empty array", claim: []any{}, want: []string{}, wantRead: true},
		{name: "array skips non-strings", claim: []any{"admin", 7, nil, "auditor"}, want: []string{"admin", "auditor"}, wantRead: true},
		// An array this cannot name a single group from is an unreadable shape,
		// not a provider saying the account has no groups: reading it as the
		// latter would clear every membership.
		{name: "array of objects", claim: []any{map[string]any{"id": "dev"}}, want: nil, wantRead: false},
		{name: "array of numbers", claim: []any{1, 2}, want: nil, wantRead: false},
		{name: "space delimited", claim: "openid email profile", want: []string{"openid", "email", "profile"}, wantRead: true},
		{name: "comma delimited", claim: "admin,auditor", want: []string{"admin", "auditor"}, wantRead: true},
		{name: "empty string", claim: "", want: []string{}, wantRead: true},
		// An unreadable shape is reported as unread, never as "no groups": the
		// caller clears memberships on an empty list.
		{name: "absent", claim: nil, want: nil, wantRead: false},
		{name: "object", claim: map[string]any{"a": "b"}, want: nil, wantRead: false},
		{name: "number", claim: 3, want: nil, wantRead: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := GroupValues(tc.claim)
			require.Equal(t, tc.wantRead, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
