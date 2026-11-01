package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The saved snapshot is current when it equals the target's fingerprint, and also
// when it equals the legacy DDL hash: a rule saved before fingerprints existed
// stored that hash, and an upgrade must not report every StarRocks/Doris check as
// a structure change.
func TestQualitySchemaChangedSince(t *testing.T) {
	t.Parallel()

	fingerprint := []byte("fingerprint")
	legacyDDL := []byte("ddl-hash")

	tests := []struct {
		name     string
		snapshot []byte
		signal   store.MetaRegistrySchemaSignal
		want     bool
	}{
		{
			name:     "no snapshot",
			snapshot: nil,
			signal:   store.MetaRegistrySchemaSignal{Fingerprint: fingerprint},
			want:     false,
		},
		{
			name:     "target has no fingerprint yet",
			snapshot: []byte("anything"),
			signal:   store.MetaRegistrySchemaSignal{},
			want:     false,
		},
		{
			name:     "matches the fingerprint",
			snapshot: fingerprint,
			signal:   store.MetaRegistrySchemaSignal{Fingerprint: fingerprint},
			want:     false,
		},
		{
			name:     "matches the legacy DDL hash",
			snapshot: legacyDDL,
			signal:   store.MetaRegistrySchemaSignal{Fingerprint: fingerprint, LegacyDDLHash: legacyDDL},
			want:     false,
		},
		{
			name:     "differs from a target that has only a fingerprint",
			snapshot: []byte("older"),
			signal:   store.MetaRegistrySchemaSignal{Fingerprint: fingerprint},
			want:     true,
		},
		{
			name:     "differs from both signals",
			snapshot: []byte("older"),
			signal:   store.MetaRegistrySchemaSignal{Fingerprint: fingerprint, LegacyDDLHash: legacyDDL},
			want:     true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, qualitySchemaChangedSince(test.snapshot, test.signal))
		})
	}
}
