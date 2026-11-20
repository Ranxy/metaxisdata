package v1

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestMapCommonErrorKeepsInternalErrorUnwrapped(t *testing.T) {
	t.Parallel()

	handlerErr := connect.NewError(connect.CodeInternal, errors.New("failed to fetch models"))
	mapped := mapCommonError(handlerErr)

	require.Same(t, handlerErr, mapped)
	connectErr, ok := errors.AsType[*connect.Error](mapped)
	require.True(t, ok)
	require.Equal(t, "failed to fetch models", connectErr.Message())
}

func TestMapCommonErrorUpgradesToCommonCode(t *testing.T) {
	t.Parallel()

	cause := common.Errorf(common.NotFound, "LLM profile %q not found", "profiles/x")
	mapped := mapCommonError(connect.NewError(connect.CodeInternal, cause))

	connectErr, ok := errors.AsType[*connect.Error](mapped)
	require.True(t, ok)
	require.Equal(t, connect.CodeNotFound, connectErr.Code())
	require.Equal(t, `LLM profile "profiles/x" not found`, connectErr.Message())
	require.ErrorIs(t, mapped, cause)
}

func TestMapCommonErrorKeepsDetailsWhenUpgrading(t *testing.T) {
	t.Parallel()

	handlerErr := connect.NewError(connect.CodeInternal, common.Errorf(common.NotFound, "missing"))
	detail, detailErr := connect.NewErrorDetail(&v1pb.GetInstanceRequest{Name: "instances/i1"})
	require.NoError(t, detailErr)
	handlerErr.AddDetail(detail)

	connectErr, ok := errors.AsType[*connect.Error](mapCommonError(handlerErr))
	require.True(t, ok)
	require.Equal(t, connect.CodeNotFound, connectErr.Code())
	require.Len(t, connectErr.Details(), 1)
}

func TestMapCommonErrorUpgradesUnknownCode(t *testing.T) {
	t.Parallel()

	mapped := mapCommonError(connect.NewError(connect.CodeUnknown, common.Errorf(common.Invalid, "bad %q", "x")))

	connectErr, ok := errors.AsType[*connect.Error](mapped)
	require.True(t, ok)
	require.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	require.Equal(t, `bad "x"`, connectErr.Message())
}

func TestMapCommonErrorKeepsHandlerChosenStatus(t *testing.T) {
	t.Parallel()

	handlerErr := connect.NewError(connect.CodePermissionDenied, common.Errorf(common.NotFound, "hidden"))
	require.Same(t, handlerErr, mapCommonError(handlerErr))
}

func TestMapCommonErrorWrapsBareCommonError(t *testing.T) {
	t.Parallel()

	mapped := mapCommonError(common.Errorf(common.Conflict, "duplicate name"))

	connectErr, ok := errors.AsType[*connect.Error](mapped)
	require.True(t, ok)
	require.Equal(t, connect.CodeAlreadyExists, connectErr.Code())
	require.Equal(t, "duplicate name", connectErr.Message())
}

func TestMapCommonErrorPassesNilThrough(t *testing.T) {
	t.Parallel()

	require.NoError(t, mapCommonError(nil))
}
