package server

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestNewEchoServerBoundsSlowClients pins the connection bounds of the API
// listener. The body limit bounds how many bytes a request may carry, not how
// long a client may take to send them: without a read header deadline an
// anonymous caller can hold goroutines and file descriptors open indefinitely,
// which is a resource-exhaustion path rather than a slow request.
func TestNewEchoServerBoundsSlowClients(t *testing.T) {
	t.Parallel()

	e := newEchoServer()
	require.NotNil(t, e.Server)
	require.Equal(t, httpReadHeaderTimeout, e.Server.ReadHeaderTimeout)
	require.Equal(t, httpReadTimeout, e.Server.ReadTimeout)
	require.Equal(t, httpIdleTimeout, e.Server.IdleTimeout)
	require.Equal(t, httpMaxHeaderBytes, e.Server.MaxHeaderBytes)
	require.Zero(t, e.Server.WriteTimeout, "streaming RPC responses must not be cut by a write deadline")
}

// TestConfigureHTTPServerTimeoutsAppliesToAnExistingEcho covers the helper used
// by tests and embedders that build their own Echo instance.
func TestConfigureHTTPServerTimeoutsAppliesToAnExistingEcho(t *testing.T) {
	t.Parallel()

	e := echo.New()
	configureHTTPServerTimeouts(e)
	require.Equal(t, httpReadHeaderTimeout, e.Server.ReadHeaderTimeout)
	require.Equal(t, httpReadTimeout, e.Server.ReadTimeout)
	require.Equal(t, httpIdleTimeout, e.Server.IdleTimeout)
	require.Equal(t, httpMaxHeaderBytes, e.Server.MaxHeaderBytes)
}
