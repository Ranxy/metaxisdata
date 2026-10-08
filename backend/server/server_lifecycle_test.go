package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestServerRunAndShutdownStopTheListener drives the real Run/Shutdown path with
// a hand-built Server: it binds a port, serves over H2C, then must release the
// listener and wait for the runners it was given.
func TestServerRunAndShutdownStopTheListener(t *testing.T) {
	t.Parallel()

	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()

	e := echo.New()
	configureEchoRoutersForTest(e, &config.Profile{Mode: common.ReleaseModeProd})

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

// A notification stream is an in-flight HTTP request that never finishes on its own, and
// Shutdown waits for those. Shutdown therefore has to end the subscriptions before it
// drains the server: without that, every stop spends the whole graceful period waiting
// for clients to close their tabs, and the process reports a failed drain.
func TestShutdownEndsAnOpenNotificationStream(t *testing.T) {
	t.Parallel()

	// The hub is all this needs: no message is written and no store call is made, so an
	// empty store is enough to build the service the server drains.
	notifier := notification.New(&store.Store{})

	e := echo.New()
	configureEchoRoutersForTest(e, &config.Profile{Mode: common.ReleaseModeProd})
	inflight := make(chan struct{})
	e.GET("/stream", func(c echo.Context) error {
		events, unsubscribe, err := notifier.Subscribe(7)
		if err != nil {
			return err
		}
		defer unsubscribe()

		c.Response().WriteHeader(http.StatusOK)
		c.Response().Flush()
		close(inflight)
		for {
			select {
			case <-c.Request().Context().Done():
				return nil
			case _, open := <-events:
				if !open {
					// The subscription ended: the server is draining, or this
					// connection fell behind.
					return nil
				}
				if _, err := c.Response().Write([]byte("event")); err != nil {
					return nil
				}
				c.Response().Flush()
			case <-time.After(50 * time.Millisecond):
				// A byte now and then, so the connection stays a live stream rather
				// than an idle one the server may close on its own.
				if _, err := c.Response().Write([]byte(".")); err != nil {
					return nil
				}
				c.Response().Flush()
			}
		}
	})

	s := &Server{echoServer: e, notifier: notifier}

	port := freePort(t)
	require.NoError(t, s.Run(context.Background(), port))
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// The stream is read on its own goroutine: what the test waits for is the client
	// seeing a clean end, which is the difference between draining and abandoning.
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get(baseURL + "/stream")
		if err != nil {
			clientDone <- err
			return
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, response.Body)
		clientDone <- err
	}()

	select {
	case <-inflight:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream never reached the handler")
	}

	started := time.Now()
	require.NoError(t, s.Shutdown(context.Background()))
	require.Less(t, time.Since(started), 2*time.Second,
		"Shutdown waited for the stream instead of ending it")

	select {
	case err := <-clientDone:
		require.NoError(t, err, "the client must see the stream end cleanly")
	case <-time.After(5 * time.Second):
		t.Fatal("the client never saw the stream end")
	}
}
