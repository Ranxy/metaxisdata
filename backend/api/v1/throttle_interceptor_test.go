package v1

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The budgets are the state package's constants; they are repeated here so a
// silent change to them shows up as a failing test rather than passing either
// way.
const (
	testLoginSourceBudget      = 30
	testCreateUserSourceBudget = 10
)

func newTestThrottleInterceptor(t *testing.T, trustedProxies []string) *ThrottleInterceptor {
	t.Helper()
	stateCfg, err := state.New()
	require.NoError(t, err)
	return NewThrottleInterceptor(stateCfg, trustedProxies)
}

func requireResourceExhausted(t *testing.T, err error) {
	t.Helper()
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeResourceExhausted, connectErr.Code(), "unexpected error: %v", err)
}

// allow runs check exactly count times and returns the first error, so a test can
// show that a budget allows up to its limit and refuses after it.
func allow(t *testing.T, interceptor *ThrottleInterceptor, count int, procedure string, header http.Header, peer string, now time.Time) error {
	t.Helper()
	var err error
	for range count {
		if err = interceptor.check(context.Background(), procedure, header, peer, now); err != nil {
			return err
		}
	}
	return nil
}

// TestThrottleInterceptorLoginBudget pins that anonymous Login is bounded per
// source: the 31st request from one address in the window is refused, while
// another address still gets its own budget.
func TestThrottleInterceptorLoginBudget(t *testing.T) {
	t.Parallel()

	interceptor := newTestThrottleInterceptor(t, nil)
	now := time.Now()

	require.NoError(t, allow(t, interceptor, testLoginSourceBudget, v1connect.AuthServiceLoginProcedure, http.Header{}, "203.0.113.5:4040", now))
	requireResourceExhausted(t, interceptor.check(context.Background(), v1connect.AuthServiceLoginProcedure, http.Header{}, "203.0.113.5:4040", now))
	require.NoError(t, interceptor.check(context.Background(), v1connect.AuthServiceLoginProcedure, http.Header{}, "198.51.100.9:4040", now))
}

// TestThrottleInterceptorCreateUserBudget pins that anonymous signup, which
// hashes a password, has its own smaller budget.
func TestThrottleInterceptorCreateUserBudget(t *testing.T) {
	t.Parallel()

	interceptor := newTestThrottleInterceptor(t, nil)
	now := time.Now()

	require.NoError(t, allow(t, interceptor, testCreateUserSourceBudget, v1connect.UserServiceCreateUserProcedure, http.Header{}, "203.0.113.5:4040", now))
	requireResourceExhausted(t, interceptor.check(context.Background(), v1connect.UserServiceCreateUserProcedure, http.Header{}, "203.0.113.5:4040", now))
}

// TestThrottleInterceptorSkipsAuthenticatedCallers pins that the budget bounds
// anonymous requests only: a signed-in caller's CPU is bounded per principal by
// the general rate limiting, not here.
func TestThrottleInterceptorSkipsAuthenticatedCallers(t *testing.T) {
	t.Parallel()

	interceptor := newTestThrottleInterceptor(t, nil)
	ctx := context.WithValue(context.Background(), common.UserContextKey, &store.UserMessage{ID: 7, Email: "admin@example.com"})
	now := time.Now()

	for range testLoginSourceBudget * 3 {
		require.NoError(t, interceptor.check(ctx, v1connect.AuthServiceLoginProcedure, http.Header{}, "203.0.113.5:4040", now))
	}
}

// TestThrottleInterceptorIgnoresUnrelatedProcedures pins that the interceptor is
// a no-op for every method that is not CPU-bound and anonymous.
func TestThrottleInterceptorIgnoresUnrelatedProcedures(t *testing.T) {
	t.Parallel()

	interceptor := newTestThrottleInterceptor(t, nil)
	now := time.Now()

	for range testLoginSourceBudget * 3 {
		require.NoError(t, interceptor.check(context.Background(), "/metaxisdata.v1.InstanceService/ListInstances", http.Header{}, "203.0.113.5:4040", now))
	}
}

// TestThrottleInterceptorKeysOnTheResolvedClientAddress pins the M1/M3 contract:
// the bucket follows the address the audit ledger would record, not the bare TCP
// peer and not whatever the client prepended to X-Forwarded-For.
func TestThrottleInterceptorKeysOnTheResolvedClientAddress(t *testing.T) {
	t.Parallel()

	now := time.Now()

	t.Run("a trusted proxy's forwarded address is the key", func(t *testing.T) {
		t.Parallel()

		interceptor := newTestThrottleInterceptor(t, []string{"10.0.0.1"})
		header := http.Header{}
		header.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.7")

		require.NoError(t, allow(t, interceptor, testLoginSourceBudget, v1connect.AuthServiceLoginProcedure, header, "10.0.0.1:5000", now))
		requireResourceExhausted(t, interceptor.check(context.Background(), v1connect.AuthServiceLoginProcedure, header, "10.0.0.1:5000", now))

		// A different real client behind the same proxy has its own budget, and
		// the forged leftmost entry the first client sent changes nothing.
		other := http.Header{}
		other.Set("X-Forwarded-For", "9.9.9.9, 198.51.100.8")
		require.NoError(t, interceptor.check(context.Background(), v1connect.AuthServiceLoginProcedure, other, "10.0.0.1:5000", now))
	})

	t.Run("an untrusted peer's forwarded header cannot rotate the key", func(t *testing.T) {
		t.Parallel()

		interceptor := newTestThrottleInterceptor(t, []string{"10.0.0.1"})
		for i := range testLoginSourceBudget {
			header := http.Header{}
			header.Set("X-Forwarded-For", fmt.Sprintf("9.9.9.%d", i+1))
			require.NoError(t, interceptor.check(context.Background(), v1connect.AuthServiceLoginProcedure, header, "203.0.113.5:4040", now))
		}
		header := http.Header{}
		header.Set("X-Forwarded-For", "9.9.9.250")
		requireResourceExhausted(t, interceptor.check(context.Background(), v1connect.AuthServiceLoginProcedure, header, "203.0.113.5:4040", now))
	})
}
