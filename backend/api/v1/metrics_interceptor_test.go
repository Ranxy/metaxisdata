package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

// TestProcedureParts pins the route-label split used by the RPC metrics
// interceptor: the generated procedure is "/<package>.<Service>/<Method>", and
// the label set must be the short service name and the method.
func TestProcedureParts(t *testing.T) {
	t.Parallel()

	service, method := procedureParts("/metaxisdata.v1.InstanceService/SyncInstance")
	require.Equal(t, "InstanceService", service)
	require.Equal(t, "SyncInstance", method)

	service, method = procedureParts("no-slash")
	require.Equal(t, "unknown", service)
	require.Equal(t, "unknown", method)
}

// TestRPCCode pins the code label: a nil error is "ok", not Connect's unknown.
func TestRPCCode(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ok", rpcCode(nil))
	require.Equal(t, "invalid_argument", rpcCode(connect.NewError(connect.CodeInvalidArgument, errors.New("bad"))))
	require.Equal(t, "unavailable", rpcCode(connect.NewError(connect.CodeUnavailable, context.Canceled)))
}
