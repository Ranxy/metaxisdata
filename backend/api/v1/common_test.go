package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func TestConnectErrorForWriteMapsConflictToAlreadyExists(t *testing.T) {
	t.Parallel()

	err := connectErrorForWrite(common.Errorf(common.Conflict, "user with email %q already exists", "a@example.com"), "failed to create user")
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeAlreadyExists, connectErr.Code())

	err = connectErrorForWrite(errors.New("boom"), "failed to create user")
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeInternal, connectErr.Code())
}
