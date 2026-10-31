package v1

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// newRequest is a client-built request, whose procedure name is empty: connect's
// AnyRequest cannot be implemented outside the package, so the override lookup is
// exercised through that empty key.
func newRequest() *connect.Request[emptypb.Empty] {
	return connect.NewRequest(&emptypb.Empty{})
}

func TestDeadlineInterceptorTimesOutASlowCall(t *testing.T) {
	t.Parallel()

	in := NewDeadlineInterceptor(10*time.Millisecond, nil)
	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	_, err := in.WrapUnary(next)(context.Background(), newRequest())
	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connectDeadlineMessage, connectErr.Message(),
		"the caller must not learn which dependency stalled")
}

func TestDeadlineInterceptorPassesAFastCallThrough(t *testing.T) {
	t.Parallel()

	in := NewDeadlineInterceptor(time.Second, nil)
	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "the handler must run under the server-side deadline")
		require.Greater(t, time.Until(deadline), 500*time.Millisecond)
		return connect.NewResponse(&emptypb.Empty{}), nil
	}

	resp, err := in.WrapUnary(next)(context.Background(), newRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestDeadlineInterceptorKeepsANonDeadlineError(t *testing.T) {
	t.Parallel()

	in := NewDeadlineInterceptor(time.Second, nil)
	want := connect.NewError(connect.CodeNotFound, errors.New("missing"))
	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, want
	}

	_, err := in.WrapUnary(next)(context.Background(), newRequest())
	require.Equal(t, want, err)
}

func TestDeadlineInterceptorUsesTheOverrideForAListedProcedure(t *testing.T) {
	t.Parallel()

	// A client-built request carries no procedure name, so an override keyed on the
	// empty procedure is the one the lookup can reach from a unit test.
	in := NewDeadlineInterceptor(time.Millisecond, map[string]time.Duration{"": time.Hour})
	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Greater(t, time.Until(deadline), time.Minute, "a listed procedure gets the long bound")
		return connect.NewResponse(&emptypb.Empty{}), nil
	}

	_, err := in.WrapUnary(next)(context.Background(), newRequest())
	require.NoError(t, err)
}

func TestDeadlineInterceptorDisabledLeavesTheCallUnbounded(t *testing.T) {
	t.Parallel()

	in := NewDeadlineInterceptor(0, nil)
	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		_, ok := ctx.Deadline()
		require.False(t, ok)
		return connect.NewResponse(&emptypb.Empty{}), nil
	}

	_, err := in.WrapUnary(next)(context.Background(), newRequest())
	require.NoError(t, err)
}

func TestDeadlineInterceptorTimeoutFor(t *testing.T) {
	t.Parallel()

	const procedure = "/metaxisdata.v1.DataQualityService/ProfileColumns"
	in := NewDeadlineInterceptor(time.Minute, map[string]time.Duration{procedure: time.Hour})
	require.Equal(t, time.Hour, in.timeoutFor(procedure), "a listed procedure gets its own bound")
	require.Equal(t, time.Minute, in.timeoutFor("/metaxisdata.v1.InstanceService/GetInstance"))
}

func TestDeadlineInterceptorLeavesStreamsUntouched(t *testing.T) {
	t.Parallel()

	in := NewDeadlineInterceptor(time.Millisecond, nil)
	handler := func(context.Context, connect.StreamingHandlerConn) error { return nil }
	client := func(context.Context, connect.Spec) connect.StreamingClientConn { return nil }

	require.Equal(t,
		reflect.ValueOf(handler).Pointer(),
		reflect.ValueOf(in.WrapStreamingHandler(handler)).Pointer())
	require.Equal(t,
		reflect.ValueOf(client).Pointer(),
		reflect.ValueOf(in.WrapStreamingClient(client)).Pointer())
}
