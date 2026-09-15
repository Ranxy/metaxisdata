// Package client builds the ConnectRPC clients the CLI talks through and maps
// failures onto the exit codes an agent branches on.
package client

import (
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
	if errors.Is(err, ErrDeviceSessionExpired) {
		return ExitUnauthenticated
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

// CodeInvalidArgument is the code of a usage problem without a more specific
// recovery.
const CodeInvalidArgument = "invalid_argument"

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

// ErrDeviceSessionExpired marks a device login that is gone (expired, denied or
// already exchanged). It is reported as an authentication problem, because the
// only recovery is to start a new login.
var ErrDeviceSessionExpired = errors.New("device login session expired")

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
}

// New builds the clients for one server address. Every request carries the
// bearer token, which is also what exempts the CLI from the cookie-based CSRF
// protection.
func New(server string, options Options) (*Client, error) {
	baseURL, err := normalizeServer(server)
	if err != nil {
		return nil, err
	}

	transport, err := newTransport(options)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Transport: transport}
	if options.Timeout > 0 {
		httpClient.Timeout = options.Timeout
	}

	return &Client{
		Server:   baseURL,
		Auth:     v1connect.NewAuthServiceClient(httpClient, baseURL),
		User:     v1connect.NewUserServiceClient(httpClient, baseURL),
		Instance: v1connect.NewInstanceServiceClient(httpClient, baseURL),
		Database: v1connect.NewDatabaseServiceClient(httpClient, baseURL),
		Lineage:  v1connect.NewLineageServiceClient(httpClient, baseURL),
	}, nil
}

// normalizeServer validates and normalizes the address, so a user can paste one
// with or without a trailing slash.
func normalizeServer(server string) (string, error) {
	if server == "" {
		return "", Usage("no server address configured").
			WithCode("server_required").
			WithHint("run `mxd auth login --server https://mx.example.com` once; the address is saved for later commands")
	}
	parsed, err := url.Parse(server)
	if err != nil {
		return "", Usage("server address %q is not a valid URL", server)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", Usage("server address %q must use http or https", server)
	}
	if parsed.Host == "" {
		return "", Usage("server address %q has no host", server)
	}
	return strings.TrimSuffix(server, "/"), nil
}

// newTransport wraps the default transport so every request carries the token.
func newTransport(options Options) (http.RoundTripper, error) {
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

	if options.Token == "" {
		return transport, nil
	}
	return &bearerTransport{base: transport, token: options.Token}, nil
}

// bearerTransport attaches the access token to every request.
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		// The request is cloned because a RoundTripper must not modify the one
		// it was handed.
		cloned := req.Clone(req.Context())
		cloned.Header.Set("Authorization", "Bearer "+t.token)
		req = cloned
	}
	return t.base.RoundTrip(req)
}
