package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
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
	require.Equal(t, "https://mx.example.com", normalized.String())

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
		require.Equal(t, tc.want, got.String(), "address %q", tc.given)
	}

	// A base URL cannot carry credentials, a query or a fragment. A bare "?"
	// counts: the Connect paths are appended to this string, so leaving it in
	// would turn the procedure name into a query.
	for _, given := range []string{"https://user:pass@mx.example.com", "https://mx.example.com?a=1", "https://mx.example.com#f", "https://mx.example.com/x?"} {
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

	transport, err := newTransport(mustParseURL(t, "https://mx.example.com"), Options{Token: "secret"})
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

// The credential belongs to the server it was issued by. A request for another
// host — a redirect that was followed, a subdomain the standard library would
// treat as the same domain, or a downgrade to plain HTTP — leaves without it.
func TestBearerTransportOnlyAttachesTheTokenToTheConfiguredServer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		server     string
		target     string
		wantHeader string
	}{
		{name: "the configured server", server: "https://mx.example.com", target: "https://mx.example.com/v1/x", wantHeader: "Bearer secret"},
		{name: "a subdomain of the configured host", server: "https://mx.example.com", target: "https://evil.mx.example.com/v1/x", wantHeader: ""},
		{name: "another host", server: "https://mx.example.com", target: "https://evil.example.com/v1/x", wantHeader: ""},
		{name: "a downgrade on the same host", server: "https://mx.example.com", target: "http://mx.example.com/v1/x", wantHeader: ""},
		{name: "another port on the same host", server: "https://mx.example.com:8443", target: "https://mx.example.com/v1/x", wantHeader: ""},
		{name: "an upgrade on the same host", server: "http://mx.example.com", target: "https://mx.example.com/v1/x", wantHeader: "Bearer secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var seen string
			recorder := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				seen = req.Header.Get("Authorization")
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			})
			transport, err := newTransport(mustParseURL(t, tc.server), Options{Token: "secret"})
			require.NoError(t, err)
			bearer, ok := transport.(*bearerTransport)
			require.True(t, ok)
			bearer.base = recorder

			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tc.target, nil)
			require.NoError(t, err)
			response, err := transport.RoundTrip(request)
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()

			require.Equal(t, tc.wantHeader, seen)
		})
	}
}

// A redirect to another host must fail the call rather than carry the token
// anywhere else. The third-party server is where the token would have leaked,
// so it must never see a request at all. It is TLS like the entry point on
// purpose: an http target would also be stopped by the downgrade rule, and this
// test is about the host.
func TestClientRefusesARedirectToAnotherHost(t *testing.T) {
	t.Parallel()

	leaked := make(chan string, 1)
	thirdParty := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked <- r.Header.Get("Authorization")
	}))
	defer thirdParty.Close()

	entry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, thirdParty.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer entry.Close()

	connection, err := New(entry.URL, Options{Token: "SECRET-TOKEN", Insecure: true})
	require.NoError(t, err)

	_, err = connection.Auth.Login(context.Background(), connect.NewRequest(&v1pb.LoginRequest{
		Email:    "dev@example.com",
		Password: "secret",
	}))

	// The leak is the security-critical assertion, so it is checked first: a
	// failing error message would otherwise stop the test before it ran.
	select {
	case header := <-leaked:
		t.Fatalf("the third-party host received a request with Authorization %q", header)
	default:
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "another host")
}

// Setting CheckRedirect replaces the standard library's own redirect cap, so
// the cap has to be kept here: a server that keeps pointing at itself is a
// misconfiguration, not a reason to follow it until the timeout runs out.
func TestClientStopsAfterTooManyRedirects(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/metaxisdata.v1.AuthService/Login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusTemporaryRedirect)
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	// The timeout only bounds the failure, should the cap ever go missing: the
	// cap itself makes this return immediately.
	connection, err := New(server.URL, Options{Token: "SECRET-TOKEN", Insecure: true, Timeout: 5 * time.Second})
	require.NoError(t, err)

	_, err = connection.Auth.Login(context.Background(), connect.NewRequest(&v1pb.LoginRequest{
		Email:    "dev@example.com",
		Password: "secret",
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "redirects")
}

// The policy refuses the host change, not the redirect: staying on the server
// still works, and the token goes along.
func TestClientFollowsARedirectWithinTheServer(t *testing.T) {
	t.Parallel()

	tokens := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/metaxisdata.v1.AuthService/Login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/redirected", func(w http.ResponseWriter, r *http.Request) {
		tokens <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/proto")
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	connection, err := New(server.URL, Options{Token: "SECRET-TOKEN", Insecure: true})
	require.NoError(t, err)
	_, _ = connection.Auth.Login(context.Background(), connect.NewRequest(&v1pb.LoginRequest{
		Email:    "dev@example.com",
		Password: "secret",
	}))

	select {
	case token := <-tokens:
		require.Equal(t, "Bearer SECRET-TOKEN", token)
	default:
		t.Fatal("a redirect that stays on the server must still be followed")
	}
}

// A redirect that would put the token on a plaintext connection is refused even
// though the host is unchanged.
func TestClientRefusesADowngradeToHTTP(t *testing.T) {
	t.Parallel()

	entry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer entry.Close()

	connection, err := New(entry.URL, Options{Token: "SECRET-TOKEN", Insecure: true})
	require.NoError(t, err)

	_, err = connection.Auth.Login(context.Background(), connect.NewRequest(&v1pb.LoginRequest{
		Email:    "dev@example.com",
		Password: "secret",
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "downgrade")
}

func TestTransportWithoutATokenIsUnchanged(t *testing.T) {
	t.Parallel()

	transport, err := newTransport(mustParseURL(t, "https://mx.example.com"), Options{})
	require.NoError(t, err)
	_, ok := transport.(*bearerTransport)
	require.False(t, ok, "an anonymous invocation needs no token layer")
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
