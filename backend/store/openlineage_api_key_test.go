package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaskOpenLineageAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		plainKey string
		want     string
	}{
		{
			name:     "standard openlineage key",
			plainKey: "ol_6271bc3fd27f3d5a4d540d0a58bf774b53df50ef95475a8d689edd5df8",
			want:     "ol_6271b" + strings.Repeat("*", len("ol_6271bc3fd27f3d5a4d540d0a58bf774b53df50ef95475a8d689edd5df8")-8-9) + "89edd5df8",
		},
		{
			name:     "short key",
			plainKey: "ol_short",
			want:     "o*******",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, maskOpenLineageAPIKey(test.plainKey))
		})
	}
}

// The digest is the O(1) lookup key for validation. It must be stable for the
// same plaintext and different for a different one; the bcrypt hash still
// decides acceptance, so the digest need not be a password hash.
func TestOpenLineageAPIKeyDigest(t *testing.T) {
	t.Parallel()

	key := "ol_6271bc3fd27f3d5a4d540d0a58bf774b53df50ef95475a8d689edd5df8"
	require.Len(t, openLineageAPIKeyDigest(key), 64)
	require.Equal(t, openLineageAPIKeyDigest(key), openLineageAPIKeyDigest(key))
	require.NotEqual(t, openLineageAPIKeyDigest(key), openLineageAPIKeyDigest(key+"x"))
}
