package v1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestDebugInterceptorTruncatesLongConnectErrors(t *testing.T) {
	t.Parallel()

	interceptor := NewDebugInterceptor()
	long := strings.Repeat("x", 11000)

	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(connect.CodeInternal, errors.New(long))
	})

	_, err := wrapped(context.Background(), connect.NewRequest(&v1pb.GetInstanceRequest{Name: "instances/i1"}))

	require.Error(t, err)
	connectErr, ok := errors.AsType[*connect.Error](err)
	require.True(t, ok)
	require.Equal(t, connect.CodeInternal, connectErr.Code())
	require.Equal(t, len("[TRUNCATED] ")+10240, len(connectErr.Message()))
	require.True(t, strings.HasPrefix(connectErr.Message(), "[TRUNCATED] "))
}

func TestDebugInterceptorLeavesShortErrorsAlone(t *testing.T) {
	t.Parallel()

	interceptor := NewDebugInterceptor()

	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("instance not found"))
	})

	_, err := wrapped(context.Background(), connect.NewRequest(&v1pb.GetInstanceRequest{Name: "instances/i1"}))

	require.ErrorContains(t, err, "instance not found")
	require.NotContains(t, err.Error(), "[TRUNCATED]")
}

func TestDebugInterceptorPassesSuccessThrough(t *testing.T) {
	t.Parallel()

	interceptor := NewDebugInterceptor()
	response := connect.NewResponse(&v1pb.Instance{Name: "instances/i1"})

	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return response, nil
	})

	got, err := wrapped(context.Background(), connect.NewRequest(&v1pb.GetInstanceRequest{Name: "instances/i1"}))
	require.NoError(t, err)
	require.Same(t, response, got)
}

func TestDebugInterceptorDoesNotTruncatePlainErrors(t *testing.T) {
	t.Parallel()

	interceptor := NewDebugInterceptor()
	long := errors.New(strings.Repeat("y", 11000))

	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, long
	})

	_, err := wrapped(context.Background(), connect.NewRequest(&v1pb.GetInstanceRequest{Name: "instances/i1"}))
	require.ErrorIs(t, err, long)
	require.NotContains(t, err.Error(), "[TRUNCATED]")
}

// Rebuilding a truncated error must keep its structured details: dropping them
// silently changed the client-visible response.
func TestDebugInterceptorKeepsDetailsWhenTruncating(t *testing.T) {
	t.Parallel()

	interceptor := NewDebugInterceptor()
	long := strings.Repeat("x", 11000)

	original := connect.NewError(connect.CodeInvalidArgument, errors.New(long))
	detail, detailErr := connect.NewErrorDetail(&v1pb.GetInstanceRequest{Name: "instances/i1"})
	require.NoError(t, detailErr)
	original.AddDetail(detail)

	wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, original
	})

	_, err := wrapped(context.Background(), connect.NewRequest(&v1pb.GetInstanceRequest{Name: "instances/i1"}))
	require.Error(t, err)
	connectErr, ok := errors.AsType[*connect.Error](err)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(connectErr.Message(), "[TRUNCATED] "))
	require.Len(t, connectErr.Details(), 1)
}
