package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testEndpoints(context.Context) (Endpoints, bool, error) {
	return Endpoints{Issuer: testExternalURL, Resource: testResource}, true, nil
}

func validAuthorizationValues() url.Values {
	return url.Values{
		"response_type":         {"code"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {MCPReadScope},
		"state":                 {"client-state"},
	}
}

func TestParseAuthorizationRequest(t *testing.T) {
	t.Parallel()

	endpoints := Endpoints{Issuer: testExternalURL, Resource: testResource}

	t.Run("a complete request is accepted", func(t *testing.T) {
		t.Parallel()

		request, err := parseAuthorizationRequest(validAuthorizationValues(), "client-1", "https://app.example.com/callback", endpoints)
		require.NoError(t, err)
		require.Equal(t, "client-1", request.ClientID)
		require.Equal(t, []string{MCPReadScope}, request.Scopes)
		require.Equal(t, "client-state", request.ClientState)
	})

	t.Run("an omitted scope asks for the advertised one", func(t *testing.T) {
		t.Parallel()

		values := validAuthorizationValues()
		values.Del("scope")
		request, err := parseAuthorizationRequest(values, "client-1", "https://app.example.com/callback", endpoints)
		require.NoError(t, err)
		require.Equal(t, []string{MCPReadScope}, request.Scopes)
	})

	t.Run("the state cap itself is allowed", func(t *testing.T) {
		t.Parallel()

		values := validAuthorizationValues()
		values.Set("state", strings.Repeat("s", authorizationMaxStateLength))
		request, err := parseAuthorizationRequest(values, "client-1", "https://app.example.com/callback", endpoints)
		require.NoError(t, err)
		require.Len(t, request.ClientState, authorizationMaxStateLength)
	})

	t.Run("a repeated scope is granted once", func(t *testing.T) {
		t.Parallel()

		values := validAuthorizationValues()
		values.Set("scope", strings.Repeat(MCPReadScope+" ", 3))
		request, err := parseAuthorizationRequest(values, "client-1", "https://app.example.com/callback", endpoints)
		require.NoError(t, err)
		require.Equal(t, []string{MCPReadScope}, request.Scopes,
			"one scope exists, so repeating it must not size the pending record")
	})

	t.Run("an over-long state is not echoed back", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, "client-state", echoedClientState("client-state"))
		require.Equal(t, strings.Repeat("s", authorizationMaxStateLength),
			echoedClientState(strings.Repeat("s", authorizationMaxStateLength)))
		require.Empty(t, echoedClientState(strings.Repeat("s", authorizationMaxStateLength+1)),
			"the request is refused anyway, and echoing it would put a request-sized value in a Location header")
	})

	invalid := []struct {
		name     string
		mutate   func(url.Values)
		wantCode string
	}{
		{name: "an implicit-flow response type", mutate: func(v url.Values) { v.Set("response_type", "token") }, wantCode: "unsupported_response_type"},
		{name: "a plain PKCE method", mutate: func(v url.Values) { v.Set("code_challenge_method", "plain") }, wantCode: "invalid_request"},
		{name: "no PKCE method", mutate: func(v url.Values) { v.Del("code_challenge_method") }, wantCode: "invalid_request"},
		{name: "no PKCE challenge", mutate: func(v url.Values) { v.Del("code_challenge") }, wantCode: "invalid_request"},
		{name: "another resource", mutate: func(v url.Values) { v.Set("resource", "https://other.example.com/mcp") }, wantCode: "invalid_request"},
		{name: "no resource", mutate: func(v url.Values) { v.Del("resource") }, wantCode: "invalid_request"},
		{name: "an unknown scope", mutate: func(v url.Values) { v.Set("scope", "admin") }, wantCode: "invalid_scope"},
		// Both of these are held with the pending request for ten minutes and
		// echoed back on every answer, so a caller-chosen size is memory and an
		// unbounded Location header.
		{name: "a state over the cap", mutate: func(v url.Values) { v.Set("state", strings.Repeat("s", authorizationMaxStateLength+1)) }, wantCode: "invalid_request"},
		// An S256 challenge is exactly 43 characters; anything else can never verify.
		{name: "a PKCE challenge over the length", mutate: func(v url.Values) { v.Set("code_challenge", strings.Repeat("c", pkceChallengeLength+1)) }, wantCode: "invalid_request"},
		{name: "a PKCE challenge under the length", mutate: func(v url.Values) { v.Set("code_challenge", strings.Repeat("c", pkceChallengeLength-1)) }, wantCode: "invalid_request"},
		// The one supported scope may be repeated any number of times, so the list
		// a pending request keeps would otherwise be as large as the caller wants.
		{name: "a scope list over the cap", mutate: func(v url.Values) { v.Set("scope", strings.Repeat(MCPReadScope+" ", authorizationMaxScopeBytes)) }, wantCode: "invalid_scope"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values := validAuthorizationValues()
			tc.mutate(values)
			_, err := parseAuthorizationRequest(values, "client-1", "https://app.example.com/callback", endpoints)

			var authorizationErr *authorizationError
			require.ErrorAs(t, err, &authorizationErr)
			require.Equal(t, tc.wantCode, authorizationErr.Code)
		})
	}
}

func TestRedirectsCarryWhatTheClientNeeds(t *testing.T) {
	t.Parallel()

	const registered = "https://app.example.com/callback?existing=1"

	t.Run("an error keeps the client's state and query", func(t *testing.T) {
		t.Parallel()

		recorder := httptest.NewRecorder()
		RedirectWithError(recorder, httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil), registered, "client-state", "access_denied", "the user said no", "https://mx.example.com")

		location, err := url.Parse(recorder.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "https://app.example.com/callback", location.Scheme+"://"+location.Host+location.Path)
		require.Equal(t, "access_denied", location.Query().Get("error"))
		require.Equal(t, "the user said no", location.Query().Get("error_description"))
		require.Equal(t, "client-state", location.Query().Get("state"))
		require.Equal(t, "https://mx.example.com", location.Query().Get("iss"), "RFC 9207: the mix-up defence holds on the error path too")
		require.Equal(t, "1", location.Query().Get("existing"), "an existing query must survive")
	})

	t.Run("a code carries the issuer for RFC 9207", func(t *testing.T) {
		t.Parallel()

		recorder := httptest.NewRecorder()
		RedirectWithCode(recorder, httptest.NewRequest(http.MethodGet, "/oauth/authorize/complete", nil), registered, "the-code", "client-state", testExternalURL)

		location, err := url.Parse(recorder.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "the-code", location.Query().Get("code"))
		require.Equal(t, "client-state", location.Query().Get("state"))
		require.Equal(t, testExternalURL, location.Query().Get("iss"))
	})
}

// TestTokenEndpointRejectsBadRequestsBeforeTheStore covers the checks that run
// before any state is touched. The exchange itself needs the state store and a
// database, so it is covered by the integration suite.
func TestTokenEndpointRejectsBadRequestsBeforeTheStore(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Endpoints: testEndpoints})

	t.Run("a GET is refused", func(t *testing.T) {
		t.Parallel()

		recorder := httptest.NewRecorder()
		server.TokenHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/oauth/token", nil))
		require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
		require.Equal(t, http.MethodPost, recorder.Header().Get("Allow"))
	})

	t.Run("another grant type is refused", func(t *testing.T) {
		t.Parallel()

		recorder := httptest.NewRecorder()
		body := strings.NewReader("grant_type=refresh_token")
		request := httptest.NewRequest(http.MethodPost, "/oauth/token", body)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		server.TokenHandler().ServeHTTP(recorder, request)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "unsupported_grant_type")
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	})

	t.Run("a request without this resource is refused", func(t *testing.T) {
		t.Parallel()

		recorder := httptest.NewRecorder()
		body := strings.NewReader("grant_type=authorization_code&code=x&client_id=c&redirect_uri=https%3A%2F%2Fapp.example.com%2Fcallback")
		request := httptest.NewRequest(http.MethodPost, "/oauth/token", body)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		server.TokenHandler().ServeHTTP(recorder, request)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Contains(t, recorder.Body.String(), "invalid_target")
	})
}

// TestAuthorizeEndpointRejectsANonGet covers the one authorization-endpoint
// check that needs no client lookup.
func TestAuthorizeEndpointRejectsANonGet(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Endpoints: testEndpoints})
	recorder := httptest.NewRecorder()
	server.AuthorizeHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/oauth/authorize", nil))

	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	require.Equal(t, http.MethodGet, recorder.Header().Get("Allow"))
}
