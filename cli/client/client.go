// Package client builds the ConnectRPC clients the CLI talks through and maps
// failures onto the exit codes an agent branches on.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/cli/config"
)

// Exit codes. They are part of the CLI's contract: an agent decides what to do
// next from these numbers, so they are documented and tested.
const (
	// ExitOK is success.
	ExitOK = 0
	// ExitUsage is a bad argument or bad local input.
	ExitUsage = 1
	// ExitUnauthenticated means the token is missing, expired or revoked, or
	// that a device login session is gone. The next step is always the same:
	// run `mxd auth login`.
	ExitUnauthenticated = 2
	// ExitNotFound is a missing resource.
	ExitNotFound = 3
	// ExitPermissionDenied is a valid caller without the permission.
	ExitPermissionDenied = 4
	// ExitServerError is anything the server got wrong.
	ExitServerError = 5
	// ExitTimeout is a deadline or a cancellation.
	ExitTimeout = 6
)

// ExitCode maps an error onto its exit code. A nil error is success.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}

	var usage *UsageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	var expired *SessionExpiredError
	if errors.As(err, &expired) {
		return ExitUnauthenticated
	}
	// A local deadline or cancellation never becomes a Connect error, so it has
	// to be recognised here or it would be reported as a server failure.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ExitTimeout
	}

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return ExitServerError
	}
	switch connectErr.Code() {
	case connect.CodeUnauthenticated:
		return ExitUnauthenticated
	case connect.CodePermissionDenied:
		return ExitPermissionDenied
	case connect.CodeNotFound:
		return ExitNotFound
	case connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
		return ExitUsage
	case connect.CodeDeadlineExceeded, connect.CodeCanceled:
		return ExitTimeout
	default:
		return ExitServerError
	}
}

// UsageError is a problem with what the caller asked for, rather than with the
// server. Commands wrap their input validation in it.
//
// Code is the value an agent branches on. Most usage problems share
// "invalid_argument", but a few have their own code because the recovery is
// specific: "server_required" and "scope_required" each name the one command or
// variable that fixes them.
type UsageError struct {
	Code    string
	Message string
	Hint    string
}

func (e *UsageError) Error() string {
	return e.Message
}

// Usage builds a UsageError.
func Usage(format string, args ...any) *UsageError {
	return &UsageError{Code: CodeInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

// Error codes with a recovery of their own.
const (
	// CodeInvalidArgument is a usage problem without a more specific recovery.
	CodeInvalidArgument = "invalid_argument"
	// CodeServerRequired means no server address is configured yet. It names
	// the one command that fixes it, so it gets its own code.
	CodeServerRequired = "server_required"
	// CodeDeviceSessionExpired means the device login has to be started again.
	CodeDeviceSessionExpired = "device_session_expired"
)

// ServerRequired is the error for an invocation that has no server address.
//
// It is defined once because two paths need it: building the clients, and the
// command layer's own check before it gets that far. Two copies had already
// drifted, so one of them reported a different code than the other.
func ServerRequired() *UsageError {
	return Usage("no server address configured").
		WithCode(CodeServerRequired).
		WithHint("run `mxd auth login --server https://mx.example.com` once; the address is saved for later commands")
}

// WithHint attaches an actionable next step.
func (e *UsageError) WithHint(format string, args ...any) *UsageError {
	e.Hint = fmt.Sprintf(format, args...)
	return e
}

// WithCode overrides the reported code.
func (e *UsageError) WithCode(code string) *UsageError {
	e.Code = code
	return e
}

// SessionExpiredError is a device login that is gone: expired, denied, or no
// longer held by the server. It is reported as an authentication problem,
// because the only recovery is to start a new login.
//
// It carries both the exit status and the code string, so the two cannot
// disagree, and its message is the reason that applies to the path that
// produced it.
type SessionExpiredError struct {
	Reason string
}

func (e *SessionExpiredError) Error() string { return e.Reason }

// Code is the value an agent branches on. It is deliberately not one of the
// Connect codes: a consumed or denied request is a different situation from a
// rejected token, even though both end in the same recovery.
func (*SessionExpiredError) Code() string { return CodeDeviceSessionExpired }

// DeviceSessionExpired builds the error for a login that has to be restarted.
func DeviceSessionExpired(reason string) error { return &SessionExpiredError{Reason: reason} }

// Options are the transport settings resolved from flags and the environment.
type Options struct {
	// Token is sent as a bearer credential. Empty means anonymous.
	Token string
	// CACert is an extra PEM file to trust, for a self-signed deployment.
	CACert string
	// Insecure skips certificate verification. It is opt-in and the caller
	// warns about it.
	Insecure bool
	// Timeout bounds one request.
	Timeout time.Duration
	// RefreshToken, Credentials and CredentialsPath tie the access token to the
	// CLI session stored in the credentials file. They are set only when the token
	// came from that file: a --token or METAXISDATA_TOKEN value is never rotated
	// and never written back.
	RefreshToken    string
	Credentials     *config.Credentials
	CredentialsPath string
}

// Client holds one connection per service the CLI uses.
type Client struct {
	// Server is the normalized address the client talks to.
	Server string

	Auth     v1connect.AuthServiceClient
	User     v1connect.UserServiceClient
	Instance v1connect.InstanceServiceClient
	Database v1connect.DatabaseServiceClient
	Lineage  v1connect.LineageServiceClient

	// tokens is the live bearer token; a rotation replaces it for every later
	// request in the same process.
	tokens *tokenSource
	// session is nil when the token cannot be renewed (it came from a flag or the
	// environment, or no refresh credential is stored).
	session *sessionRefresher
}

// HasSession reports whether this invocation holds a stored session it can renew.
// It is false for a --token or METAXISDATA_TOKEN value, and for a stored session
// whose server is not the one this invocation talks to.
func (c *Client) HasSession() bool {
	return c.session != nil
}

// RefreshSession rotates the stored session so a caller about to present the
// access token has a live one. It is a no-op when the token cannot be renewed.
func (c *Client) RefreshSession(ctx context.Context) error {
	if c.session == nil {
		return nil
	}
	_, err := c.session.refresh(ctx)
	return err
}

// maxRedirects is how many redirects the client follows before giving up. It
// mirrors the cap the standard library applies, which a custom CheckRedirect
// replaces.
const maxRedirects = 10

// New builds the clients for one server address. Every request to that address
// carries the bearer token, which is also what exempts the CLI from the
// cookie-based CSRF protection. The token stays bound to that address: a
// redirect that leaves it is refused rather than followed.
func New(server string, options Options) (*Client, error) {
	baseURL, err := normalizeServer(server)
	if err != nil {
		return nil, err
	}

	tokens := &tokenSource{token: options.Token}
	transport, err := newTransport(baseURL, options, tokens)
	if err != nil {
		return nil, err
	}
	// The token belongs to the configured server and this client puts it back on
	// every hop, so a redirect is not a way to reach another host: the standard
	// library drops Authorization when a redirect leaves the server's domain, but
	// this client re-attaches it, and the library counts a subdomain of that
	// domain as the same domain anyway.
	//
	// Setting CheckRedirect replaces the standard library's own, so the cap it
	// applies has to be kept here: without it a server that keeps redirecting to
	// itself would be followed until the timeout (and forever with --timeout 0),
	// turning a misconfigured entry point into unbounded work.
	redirectPolicy := func(req *http.Request, via []*http.Request) error {
		// The policy comes first so a redirect that breaks it is reported as such,
		// even on the one hop where the cap would also apply. The host is quoted
		// because it is caller-chosen text: the message ends up on someone's
		// terminal.
		if req.URL.Host != baseURL.Host {
			return fmt.Errorf("the server redirected the request to another host (%q); refusing to follow a redirect off %q", req.URL.Host, baseURL.Host)
		}
		if baseURL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("the server redirected the request from https to %s; refusing to downgrade the connection", req.URL.Scheme)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	httpClient := &http.Client{Transport: transport, CheckRedirect: redirectPolicy}
	if options.Timeout > 0 {
		httpClient.Timeout = options.Timeout
	}

	base := baseURL.String()
	var session *sessionRefresher
	var clientOptions []connect.ClientOption
	if options.RefreshToken != "" && options.Credentials != nil && options.CredentialsPath != "" {
		// The refresh call must not carry the access token it is replacing: an
		// expired bearer in the header would only be ignored, but a second client
		// keeps the rule explicit.
		bareTransport, err := newTransport(baseURL, options, &tokenSource{})
		if err != nil {
			return nil, err
		}
		bareClient := &http.Client{Transport: bareTransport, CheckRedirect: redirectPolicy}
		if options.Timeout > 0 {
			bareClient.Timeout = options.Timeout
		}
		session = &sessionRefresher{
			endpoint:     v1connect.NewAuthServiceClient(bareClient, base),
			tokens:       tokens,
			path:         options.CredentialsPath,
			credentials:  options.Credentials,
			refreshToken: options.RefreshToken,
			now:          time.Now,
			sleep:        time.Sleep,
		}
		clientOptions = append(clientOptions, connect.WithInterceptors(session.interceptor()))
	}

	return &Client{
		Server:   base,
		tokens:   tokens,
		session:  session,
		Auth:     v1connect.NewAuthServiceClient(httpClient, base),
		User:     v1connect.NewUserServiceClient(httpClient, base, clientOptions...),
		Instance: v1connect.NewInstanceServiceClient(httpClient, base, clientOptions...),
		Database: v1connect.NewDatabaseServiceClient(httpClient, base, clientOptions...),
		Lineage:  v1connect.NewLineageServiceClient(httpClient, base, clientOptions...),
	}, nil
}

// SameServer reports whether two addresses name the same server. The stored
// session's refresh credential belongs to the server that issued it, so an
// invocation pointed elsewhere must not be allowed to send it away.
//
// The comparison is on the identity a request actually reaches, not on the
// spelling: host names are case-insensitive and an explicit default port does not
// name a different server, so respelling --server must not silently disable
// renewal for the stored session.
func SameServer(a, b string) bool {
	first, err := normalizeServer(a)
	if err != nil {
		return false
	}
	second, err := normalizeServer(b)
	if err != nil {
		return false
	}
	return first.Scheme == second.Scheme &&
		strings.EqualFold(first.Hostname(), second.Hostname()) &&
		effectivePort(first) == effectivePort(second) &&
		first.Path == second.Path
}

// effectivePort is the port the URL actually connects to, so "https://mx" and
// "https://mx:443" compare equal. normalizeServer has already restricted the
// scheme to http or https.
func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// normalizeServer validates the address and returns the form the clients are
// built from, so what is checked is what is used. A trailing slash is dropped
// because the Connect paths already start with one.
func normalizeServer(server string) (*url.URL, error) {
	if server == "" {
		return nil, ServerRequired()
	}
	parsed, err := url.Parse(server)
	if err != nil {
		return nil, Usage("server address %q is not a valid URL", server)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, Usage("server address %q must use http or https", server)
	}
	if parsed.Host == "" {
		return nil, Usage("server address %q has no host", server)
	}
	if parsed.User != nil {
		return nil, Usage("server address %q must not carry credentials", server)
	}
	// ForceQuery is a bare "?" with nothing after it: RawQuery is empty but the
	// address still spells a query, and it would otherwise survive into the
	// base URL the Connect paths are appended to, putting the procedure name
	// into the query string.
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, Usage("server address %q must not carry a query or fragment", server)
	}
	// A path is kept: the server may be mounted under a prefix. url.Parse
	// lower-cases the scheme, so HTTP:// is accepted like any other spelling.
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed, nil
}

// newTransport wraps the default transport so every request carries the token.
// The server is passed along so the token layer can tell which requests are
// actually going to it.
func newTransport(server *url.URL, options Options, tokens *tokenSource) (http.RoundTripper, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("the default HTTP transport has an unexpected type")
	}
	transport := base.Clone()

	if options.Insecure || options.CACert != "" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if options.Insecure {
			// The caller warns about this; it exists for self-hosted servers
			// with a self-signed certificate.
			tlsConfig.InsecureSkipVerify = true
		}
		if options.CACert != "" {
			pool := x509.NewCertPool()
			pem, err := os.ReadFile(options.CACert)
			if err != nil {
				return nil, Usage("failed to read the CA certificate %s: %v", options.CACert, err)
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, Usage("no certificate found in %s", options.CACert)
			}
			tlsConfig.RootCAs = pool
		}
		transport.TLSClientConfig = tlsConfig
	}

	if tokens == nil || tokens.get() == "" {
		return transport, nil
	}
	return &bearerTransport{base: transport, tokens: tokens, server: server}, nil
}

// bearerTransport attaches the access token to requests for the server it was
// built for, and to those only.
type bearerTransport struct {
	base   http.RoundTripper
	tokens *tokenSource
	server *url.URL
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The token is read per request, not captured at construction: a rotation
	// replaces it and every later request in this process must carry the new one.
	if token := t.tokens.get(); token != "" && req.Header.Get("Authorization") == "" && t.carriesCredentials(req.URL) {
		// The request is cloned because a RoundTripper must not modify the one
		// it was handed.
		cloned := req.Clone(req.Context())
		cloned.Header.Set("Authorization", "Bearer "+token)
		req = cloned
	}
	return t.base.RoundTrip(req)
}

// carriesCredentials reports whether a request goes to the configured server.
// It is the second half of the redirect policy in New: even a redirect that was
// followed would leave the server without the token. A plain-http deployment
// may be redirected up to https on the same host, which keeps the token; the
// reverse may not, so a redirect cannot strip TLS from under it.
func (t *bearerTransport) carriesCredentials(target *url.URL) bool {
	if target.Host != t.server.Host {
		return false
	}
	return target.Scheme == t.server.Scheme || t.server.Scheme == "http"
}
