package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
)

// A database the target no longer has was hidden by the sync; the caller's
// reference is stale, so the RPC answers NotFound instead of an internal error.
// Anything else keeps the internal-error answer, with the sync's own explanation.
func TestSyncDatabaseErrorMapsTheSyncOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantCode    connect.Code
		wantMessage string
	}{
		{
			name:        "a database the target no longer has is NotFound",
			err:         errors.Wrapf(schemasync.ErrDatabaseHidden, "database %q", "gone"),
			wantCode:    connect.CodeNotFound,
			wantMessage: "no longer exists on the target",
		},
		{
			name:        "any other failure stays Internal with the sync's explanation",
			err:         errors.New("connection refused"),
			wantCode:    connect.CodeInternal,
			wantMessage: "connection refused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := syncDatabaseError("instances/inst-1/databases/app", tt.err)
			require.Equal(t, tt.wantCode, connect.CodeOf(err))
			require.Contains(t, err.Error(), tt.wantMessage)
		})
	}
}
