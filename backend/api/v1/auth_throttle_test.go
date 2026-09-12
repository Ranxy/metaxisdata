package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The throttle key must ignore the port and email casing, so "Alice@x" from
// 10.0.0.1:1111 and 10.0.0.1:2222 share one window.
func TestLoginThrottleKey(t *testing.T) {
	t.Parallel()

	require.Equal(t, "alice@example.com|10.0.0.1", loginThrottleKey("  Alice@Example.com ", "10.0.0.1:1111"))
	require.Equal(t, loginThrottleKey("alice@example.com", "10.0.0.1:1111"), loginThrottleKey("ALICE@example.com", "10.0.0.1:2222"))
	require.NotEqual(t, loginThrottleKey("alice@example.com", "10.0.0.1:1111"), loginThrottleKey("bob@example.com", "10.0.0.1:1111"))
	require.NotEqual(t, loginThrottleKey("alice@example.com", "10.0.0.1:1111"), loginThrottleKey("alice@example.com", "10.0.0.2:1111"))

	// A peer address without a port (as seen in tests and some transports) is
	// used verbatim rather than dropped.
	require.Equal(t, "alice@example.com|bufconn", loginThrottleKey("alice@example.com", "bufconn"))
}
