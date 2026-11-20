package v1

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

//go:noinline
func fetchModelsFailure() error {
	return pkgerrors.Errorf("unexpected status %d from models endpoint", 404)
}

//go:noinline
func fetchModelsWrap(err error) error {
	return pkgerrors.Wrap(err, "failed to fetch models")
}

// An RPC error logged at the boundary used to show only the interceptor's own
// location. The log line has to name where the failure originated and the
// client must still see a clean message.
func TestDebugInterceptorLogsErrorOrigin(t *testing.T) {
	// slog.SetDefault is process-wide, so this test cannot run in parallel.
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, connect.NewError(connect.CodeInternal, fetchModelsWrap(fetchModelsFailure()))
	}
	chain := NewDebugInterceptor().WrapUnary(NewErrorMappingInterceptor().WrapUnary(handler))

	_, err := chain(context.Background(), connect.NewRequest(&v1pb.GetInstanceRequest{Name: "instances/i1"}))
	require.Error(t, err)

	connectErr, ok := errors.AsType[*connect.Error](err)
	require.True(t, ok)
	require.Equal(t, connect.CodeInternal, connectErr.Code())
	require.Equal(t, "failed to fetch models: unexpected status 404 from models endpoint", connectErr.Message())
	require.Contains(t, buf.String(), "fetchModelsFailure")
	// Only the origin's stack belongs on the line; the wrapper's would point at
	// the annotation site instead.
	require.NotContains(t, buf.String(), "fetchModelsWrap")
}

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
