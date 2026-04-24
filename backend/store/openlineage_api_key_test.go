package store

import (
	"strings"
	"testing"
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
			if got := maskOpenLineageAPIKey(test.plainKey); got != test.want {
				t.Fatalf("maskOpenLineageAPIKey() = %q, want %q", got, test.want)
			}
		})
	}
}
