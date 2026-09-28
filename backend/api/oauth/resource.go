// Package oauth implements the deployment's OAuth 2.1 authorization server,
// which exists to authorize MCP clients: MCP clients discover it through the
// protected-resource metadata document, send the user through the
// authorization-code flow with PKCE, and then present the resulting
// audience-bound access token to the MCP endpoint.
//
// The protocol endpoints (/oauth/authorize, /oauth/authorize/complete,
// /oauth/token, /oauth/register) are plain HTTP because OAuth clients do not
// speak ConnectRPC. The identifier every one of them is built from is derived
// here, from the workspace's external_url, because the issuer and the resource
// identifier must be byte-identical everywhere they appear.
package oauth

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// MCPReadScope is the single OAuth scope the MCP endpoint requires. The real
// authorization decision stays with the platform's IAM, per tool call; the OAuth
// scope only says "this token may talk to the MCP surface at all", so a second,
// finer-grained scope vocabulary is deliberately not invented here.
const MCPReadScope = "metaxisdata.mcp.read"

// MCPResourcePath is appended to the issuer to form the MCP resource identifier.
const MCPResourcePath = "/mcp"

// ErrExternalURLRequired means the workspace has no external_url, so the issuer
// and resource identifier cannot be derived. It is deliberately not a soft
// failure: falling back to the request's Host header would let the audience drift
// with the caller and would make an issuer mix-up attack possible.
var ErrExternalURLRequired = errors.New("the workspace external_url is required for the MCP endpoint")

// Endpoints are the identifiers derived from the workspace's external_url.
type Endpoints struct {
	// Issuer identifies the authorization server (RFC 8414 issuer, and the RFC
	// 9207 `iss` value in authorization responses).
	Issuer string
	// Resource is the MCP endpoint's resource identifier (RFC 8707 `resource`,
	// RFC 9728 `resource`, and the JWT audience).
	Resource string
}

// CanonicalizeExternalURL turns the workspace's external_url into the issuer and
// resource identifiers.
//
// The result is canonical so that comparisons are exact: the scheme and host are
// lower-cased, a default port is dropped and any trailing slash is trimmed.
// Only loopback may use http, because OAuth 2.1 requires the authorization
// server's endpoints to be served over TLS.
func CanonicalizeExternalURL(raw string) (Endpoints, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Endpoints{}, ErrExternalURLRequired
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Endpoints{}, fmt.Errorf("external_url %q is not a valid URL: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Endpoints{}, fmt.Errorf("external_url %q must use http or https", raw)
	}
	if parsed.Host == "" {
		return Endpoints{}, fmt.Errorf("external_url %q has no host", raw)
	}
	if parsed.User != nil {
		return Endpoints{}, fmt.Errorf("external_url %q must not carry credentials", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return Endpoints{}, fmt.Errorf("external_url %q must not carry a query or fragment", raw)
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return Endpoints{}, fmt.Errorf("external_url %q has no host", raw)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(host) {
		return Endpoints{}, fmt.Errorf("external_url %q must use https unless it is a loopback address", raw)
	}
	port := parsed.Port()
	if port != "" && !isDefaultPort(parsed.Scheme, port) {
		host = net.JoinHostPort(host, port)
	}

	issuer := parsed.Scheme + "://" + host + strings.TrimSuffix(parsed.Path, "/")
	return Endpoints{Issuer: issuer, Resource: issuer + MCPResourcePath}, nil
}

// isLoopbackHost reports whether host names the local machine, which is the only
// case where plain http is acceptable (and the usual case in development).
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isDefaultPort(scheme, port string) bool {
	return (scheme == "https" && port == "443") || (scheme == "http" && port == "80")
}

// AuthorizationEndpoint is where a client sends the user agent to ask for
// consent.
func (e Endpoints) AuthorizationEndpoint() string { return e.Issuer + "/oauth/authorize" }

// CompletionEndpoint is where the browser goes after consent. It is not
// advertised in the metadata documents: clients never call it, the consent page
// navigates to it so that the authorization code is minted by the server and
// never travels through an RPC response or through page JavaScript.
func (e Endpoints) CompletionEndpoint() string { return e.Issuer + "/oauth/authorize/complete" }

// TokenEndpoint is where a client exchanges an authorization code for a token.
func (e Endpoints) TokenEndpoint() string { return e.Issuer + "/oauth/token" }

// RegistrationEndpoint is where a client registers itself (RFC 7591).
func (e Endpoints) RegistrationEndpoint() string { return e.Issuer + "/oauth/register" }
