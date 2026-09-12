package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/config"
)

// TestServerRunAndShutdownStopTheListener drives the real Run/Shutdown path with
// a hand-built Server: it binds a port, serves over H2C, then must release the
// listener and wait for the runners it was given.
func TestServerRunAndShutdownStopTheListener(t *testing.T) {
	t.Parallel()

	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()

	e := echo.New()
	configureEchoRouters(e, &config.Profile{Mode: common.ReleaseModeProd})

	s := &Server{
		echoServer:   e,
		runnerCtx:    runnerCtx,
		runnerCancel: cancelRunner,
	}

	// A runner that only stops when the server cancels its context: Shutdown
	// must not return before it does.
	runnerDone := make(chan struct{})
	s.runnerWG.Add(1)
	go func() {
		defer s.runnerWG.Done()
		<-s.runnerCtx.Done()
		close(runnerDone)
	}()

	port := freePort(t)
	require.NoError(t, s.Run(context.Background(), port))
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	client := &http.Client{Timeout: 2 * time.Second}
	require.Eventually(t, func() bool {
		resp, err := client.Get(baseURL + "/healthz")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 20*time.Millisecond, "server never served /healthz")

	require.NoError(t, s.Shutdown(context.Background()))

	select {
	case <-runnerDone:
	default:
		t.Fatal("Shutdown returned before the runner observed the cancelled context")
	}

	resp, err := client.Get(baseURL + "/healthz")
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "the listener must be closed after Shutdown")
}

// TestServerShutdownWithoutEchoServerIsSafe guards the deferred Shutdown that
// NewServer runs when initialization fails part-way.
func TestServerShutdownWithoutEchoServerIsSafe(t *testing.T) {
	t.Parallel()

	s := &Server{}
	require.NoError(t, s.Shutdown(context.Background()))
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
