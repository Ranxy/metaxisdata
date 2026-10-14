package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A registry lookup answers with the identity it registered, which a caller records instead
// of the name it asked with; reading that identity back out of the GUID is what makes the
// two agree. A GUID that does not name an object of its own is not an identity.
func TestGUIDToObjectIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		guid string
		want ObjectIdentifier
		ok   bool
	}{
		{
			name: "object",
			guid: "inst;demo;dbo;Users",
			want: ObjectIdentifier{InstanceID: "inst", Database: "demo", Schema: "dbo", Name: "Users"},
			ok:   true,
		},
		{
			name: "name with the separator escaped",
			guid: "inst;demo;dbo;a%3Bb",
			want: ObjectIdentifier{InstanceID: "inst", Database: "demo", Schema: "dbo", Name: "a;b"},
			ok:   true,
		},
		{
			name: "unqualified object",
			guid: "inst;demo;;t",
			want: ObjectIdentifier{InstanceID: "inst", Database: "demo", Name: "t"},
			ok:   true,
		},
		{
			name: "scope only",
			guid: "inst;demo;dbo",
			ok:   false,
		},
		{
			name: "no name",
			guid: "inst;demo;dbo;",
			ok:   false,
		},
		{
			name: "instance only",
			guid: "inst",
			ok:   false,
		},
		{
			name: "empty",
			guid: "",
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := GUIDToObjectIdentifier(test.guid)
			require.Equal(t, test.ok, ok)
			if test.ok {
				require.Equal(t, test.want, got)
			}
		})
	}
}
