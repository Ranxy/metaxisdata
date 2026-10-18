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
func TestSyncDatabaseErrorMapsHiddenToNotFound(t *testing.T) {
	t.Parallel()

	err := syncDatabaseError("instances/inst-1/databases/gone", errors.Wrapf(schemasync.ErrDatabaseHidden, "database %q", "gone"))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	require.Contains(t, err.Error(), "no longer exists on the target")
}

// Anything else keeps the internal-error answer, with the sync's own explanation.
func TestSyncDatabaseErrorKeepsOtherFailuresInternal(t *testing.T) {
	t.Parallel()

	err := syncDatabaseError("instances/inst-1/databases/app", errors.New("connection refused"))
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	require.Contains(t, err.Error(), "connection refused")
}
