package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func TestExitCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: ExitOK},
		{name: "usage", err: Usage("bad flag"), want: ExitUsage},
		{name: "usage with a specific code", err: Usage("no scope").WithCode("scope_required"), want: ExitUsage},
		{name: "wrapped usage", err: errors.Join(errors.New("context"), Usage("bad flag")), want: ExitUsage},
		{name: "device session gone", err: DeviceSessionExpired("no longer available"), want: ExitUnauthenticated},
		{name: "unauthenticated", err: connect.NewError(connect.CodeUnauthenticated, errors.New("nope")), want: ExitUnauthenticated},
		{name: "permission denied", err: connect.NewError(connect.CodePermissionDenied, errors.New("nope")), want: ExitPermissionDenied},
		{name: "not found", err: connect.NewError(connect.CodeNotFound, errors.New("nope")), want: ExitNotFound},
		{name: "invalid argument", err: connect.NewError(connect.CodeInvalidArgument, errors.New("nope")), want: ExitUsage},
		{name: "failed precondition", err: connect.NewError(connect.CodeFailedPrecondition, errors.New("nope")), want: ExitUsage},
		{name: "deadline", err: connect.NewError(connect.CodeDeadlineExceeded, errors.New("nope")), want: ExitTimeout},
		{name: "internal", err: connect.NewError(connect.CodeInternal, errors.New("nope")), want: ExitServerError},
		{name: "not a connect error", err: errors.New("boom"), want: ExitServerError},
		// A local deadline or cancellation never becomes a Connect error, and
		// reporting it as a server failure would hide the one useful fact.
		{name: "local deadline", err: context.DeadlineExceeded, want: ExitTimeout},
		{name: "wrapped local deadline", err: fmt.Errorf("gave up waiting: %w", context.DeadlineExceeded), want: ExitTimeout},
		{name: "cancelled", err: context.Canceled, want: ExitTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ExitCode(tc.err))
		})
	}
}

func TestNormalizeServer(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeServer("https://mx.example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://mx.example.com", normalized)

	for _, server := range []string{"", "mx.example.com", "ftp://mx.example.com", "https://"} {
		_, err := normalizeServer(server)
		require.Error(t, err, "address %q must be rejected", server)
	}

	// A missing address has its own code, because it names the one command that
	// fixes it, and both the builder and the command layer must report it the
	// same way.
	_, err = normalizeServer("")
	var usage *UsageError
	require.ErrorAs(t, err, &usage)
	require.Equal(t, CodeServerRequired, usage.Code)
	require.ErrorAs(t, ServerRequired(), &usage)
	require.Equal(t, CodeServerRequired, usage.Code)
}

// What is validated is what is used: an address that passes the checks must be
// the one the clients are built from.
func TestNormalizeServerReturnsTheValidatedForm(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ given, want string }{
		{"https://mx.example.com", "https://mx.example.com"},
		{"https://mx.example.com/", "https://mx.example.com"},
		{"HTTPS://mx.example.com", "https://mx.example.com"},
		{"https://mx.example.com/prefix/", "https://mx.example.com/prefix"},
	} {
		got, err := normalizeServer(tc.given)
		require.NoError(t, err, "address %q", tc.given)
		require.Equal(t, tc.want, got, "address %q", tc.given)
	}

	// A base URL cannot carry credentials, a query or a fragment.
	for _, given := range []string{"https://user:pass@mx.example.com", "https://mx.example.com?a=1", "https://mx.example.com#f"} {
		_, err := normalizeServer(given)
		require.Error(t, err, "address %q must be rejected", given)
	}
}

// The token has to reach the wire as a bearer credential: that is what exempts
// the CLI from the cookie-based CSRF protection and what the server's auth
// interceptor reads.
func TestBearerTransportAttachesTheToken(t *testing.T) {
	t.Parallel()

	var seen string
	recorder := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})

	transport, err := newTransport(Options{Token: "secret"})
	require.NoError(t, err)

	// Swap the base transport for the recorder while keeping the token layer.
	bearer, ok := transport.(*bearerTransport)
	require.True(t, ok)
	bearer.base = recorder

	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://mx.example.com/v1/x", nil)
	require.NoError(t, err)
	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	require.Equal(t, "Bearer secret", seen)
	require.Empty(t, request.Header.Get("Authorization"), "the caller's request must not be mutated")
}

func TestTransportWithoutATokenIsUnchanged(t *testing.T) {
	t.Parallel()

	transport, err := newTransport(Options{})
	require.NoError(t, err)
	_, ok := transport.(*bearerTransport)
	require.False(t, ok, "an anonymous invocation needs no token layer")
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
