package oauth

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testExternalURL = "https://mx.example.com"
	testResource    = testExternalURL + "/mcp"
)

func TestCanonicalizeExternalURL(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name     string
		raw      string
		issuer   string
		resource string
	}{
		{name: "plain https", raw: testExternalURL, issuer: testExternalURL, resource: testResource},
		{name: "trailing slash", raw: testExternalURL + "/", issuer: testExternalURL, resource: testResource},
		{name: "default https port is dropped", raw: "https://mx.example.com:443", issuer: testExternalURL, resource: testResource},
		{name: "a default port written with leading zeros is dropped", raw: "https://mx.example.com:0443", issuer: testExternalURL, resource: testResource},
		{name: "repeated trailing slashes are dropped", raw: testExternalURL + "//", issuer: testExternalURL, resource: testResource},
		{name: "explicit port is kept", raw: "https://mx.example.com:8443", issuer: "https://mx.example.com:8443", resource: "https://mx.example.com:8443/mcp"},
		{name: "host case is lowered", raw: "https://MX.Example.COM", issuer: testExternalURL, resource: testResource},
		{name: "a path prefix is kept", raw: "https://mx.example.com/mx", issuer: "https://mx.example.com/mx", resource: "https://mx.example.com/mx/mcp"},
		{name: "http on localhost", raw: "http://localhost:8083", issuer: "http://localhost:8083", resource: "http://localhost:8083/mcp"},
		{name: "http on an ipv4 loopback", raw: "http://127.0.0.1:8083", issuer: "http://127.0.0.1:8083", resource: "http://127.0.0.1:8083/mcp"},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			endpoints, err := CanonicalizeExternalURL(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.issuer, endpoints.Issuer)
			require.Equal(t, tc.resource, endpoints.Resource)
		})
	}

	invalid := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "whitespace only", raw: "   "},
		{name: "no scheme", raw: "mx.example.com"},
		{name: "unsupported scheme", raw: "ftp://mx.example.com"},
		{name: "no host", raw: "https://"},
		{name: "credentials", raw: "https://user:pass@mx.example.com"},
		{name: "query", raw: testExternalURL + "?a=b"},
		{name: "fragment", raw: testExternalURL + "#frag"},
		{name: "plain http off loopback", raw: "http://mx.example.com"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := CanonicalizeExternalURL(tc.raw)
			require.Error(t, err)
		})
	}

	t.Run("a missing url is reported as its own error", func(t *testing.T) {
		t.Parallel()

		_, err := CanonicalizeExternalURL("")
		require.ErrorIs(t, err, ErrExternalURLRequired)
	})
}

func TestMetadataDocuments(t *testing.T) {
	t.Parallel()

	endpoints := Endpoints{Issuer: testExternalURL, Resource: testResource}

	t.Run("protected resource metadata", func(t *testing.T) {
		t.Parallel()

		document := asMap(t, endpoints.ProtectedResourceMetadata())
		require.Equal(t, testResource, document["resource"])
		require.Equal(t, []any{testExternalURL}, document["authorization_servers"])
		require.Equal(t, []any{MCPReadScope}, document["scopes_supported"])
		require.Equal(t, []any{"header"}, document["bearer_methods_supported"])
	})

	t.Run("authorization server metadata", func(t *testing.T) {
		t.Parallel()

		document := asMap(t, endpoints.AuthServerMetadata())
		require.Equal(t, testExternalURL, document["issuer"])
		require.Equal(t, testExternalURL+"/oauth/authorize", document["authorization_endpoint"])
		require.Equal(t, testExternalURL+"/oauth/token", document["token_endpoint"])
		require.Equal(t, testExternalURL+"/oauth/register", document["registration_endpoint"])
		require.Equal(t, []any{"code"}, document["response_types_supported"])
		require.Equal(t, []any{"authorization_code", "refresh_token"}, document["grant_types_supported"])
		require.Equal(t, []any{"S256"}, document["code_challenge_methods_supported"])
		require.Equal(t, []any{"none"}, document["token_endpoint_auth_methods_supported"])
		require.Equal(t, []any{MCPReadScope}, document["scopes_supported"])

		issSupported, ok := document["authorization_response_iss_parameter_supported"].(bool)
		require.True(t, ok)
		require.True(t, issSupported, "we emit the RFC 9207 iss parameter, so the document must say so")

		// Tokens are signed with the deployment's symmetric secret and their only
		// consumer is this same process, so there is no public key to publish.
		require.NotContains(t, document, "jwks_uri")
	})
}

func asMap(t *testing.T, document any) map[string]any {
	t.Helper()

	payload, err := json.Marshal(document)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(payload, &parsed))
	return parsed
}
