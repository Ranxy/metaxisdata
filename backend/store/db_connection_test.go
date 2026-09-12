package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClampMaxOpenConns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		maxConns      int
		reservedConns int
		want          int
	}{
		{name: "capped", maxConns: 300, reservedConns: 3, want: maxOpenConnsCap},
		{name: "budget", maxConns: 20, reservedConns: 5, want: 15},
		{name: "fully reserved", maxConns: 5, reservedConns: 5, want: 1},
		{name: "over reserved", maxConns: 3, reservedConns: 10, want: 1},
		{name: "bogus zero", maxConns: 0, reservedConns: 0, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// 0 would be interpreted by database/sql as "unlimited".
			require.Equal(t, tt.want, clampMaxOpenConns(tt.maxConns, tt.reservedConns))
		})
	}
}
