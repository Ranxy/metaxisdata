package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRegistrationRequest(t *testing.T) {
	t.Parallel()

	callback := "https://client.example.com/callback"
	loopback := "http://127.0.0.1:7777/callback"
	tenRedirectURIs := make([]string, 0, registrationMaxRedirectURIs)
	for i := range registrationMaxRedirectURIs {
		tenRedirectURIs = append(tenRedirectURIs, fmt.Sprintf("https://client.example.com/callback/%d", i))
	}
	tooManyRedirectURIs := append([]string{}, tenRedirectURIs...)
	tooManyRedirectURIs = append(tooManyRedirectURIs, callback)

	const callbackPrefix = "https://client.example.com/"
	atCap := callbackPrefix + strings.Repeat("a", redirectURIMaxBytes-len(callbackPrefix))

	tests := []struct {
		name     string
		request  registrationRequest
		want     validatedRegistration
		wantCode string
	}{
		{
			name: "full metadata",
			request: registrationRequest{
				ClientName:              "  Claude Desktop  ",
				RedirectURIs:            []string{callback, loopback},
				TokenEndpointAuthMethod: registrationAuthMethodNone,
				GrantTypes:              []string{registrationGrantType},
				ResponseTypes:           []string{registrationResponseType},
				Scope:                   MCPReadScope,
			},
			// client_name is trimmed before it is stored or echoed back.
			want: validatedRegistration{ClientName: "Claude Desktop", RedirectURIs: []string{callback, loopback}},
		},
		{
			name:    "only redirect_uris is required",
			request: registrationRequest{RedirectURIs: []string{callback}},
			want:    validatedRegistration{RedirectURIs: []string{callback}},
		},
		{
			// Both grants this server runs may be named together, in any order,
			// which is what an MCP client that wants to refresh sends.
			name: "both supported grant types",
			request: registrationRequest{
				RedirectURIs: []string{callback},
				GrantTypes:   []string{registrationRefreshTokenGrant, registrationGrantType},
			},
			want: validatedRegistration{RedirectURIs: []string{callback}},
		},
		{
			// One destination listed twice is still one destination; storing it twice
			// would make the consent page's redirect list read like two.
			name:    "duplicate redirect uris collapse",
			request: registrationRequest{RedirectURIs: []string{callback, callback, loopback}},
			want:    validatedRegistration{RedirectURIs: []string{callback, loopback}},
		},
		{
			name:    "ten redirect uris is the limit",
			request: registrationRequest{RedirectURIs: tenRedirectURIs},
			want:    validatedRegistration{RedirectURIs: tenRedirectURIs},
		},
		{
			// Registration is anonymous and the body may be 64 KiB, so one URI could
			// otherwise be a document that every pending request then copies.
			name:     "a redirect uri over the length cap",
			request:  registrationRequest{RedirectURIs: []string{"https://client.example.com/" + strings.Repeat("a", redirectURIMaxBytes)}},
			wantCode: registrationCodeInvalidRedirectURI,
		},
		{
			name:    "a redirect uri exactly at the length cap",
			request: registrationRequest{RedirectURIs: []string{atCap}},
			want:    validatedRegistration{RedirectURIs: []string{atCap}},
		},
		{
			name: "an empty token endpoint auth method means none",
			request: registrationRequest{
				RedirectURIs:            []string{callback},
				TokenEndpointAuthMethod: "",
				GrantTypes:              []string{},
				ResponseTypes:           []string{},
				Scope:                   "",
			},
			want: validatedRegistration{RedirectURIs: []string{callback}},
		},
		{
			name: "a repeated supported scope is accepted",
			request: registrationRequest{
				RedirectURIs: []string{callback},
				Scope:        MCPReadScope + " " + MCPReadScope,
			},
			want: validatedRegistration{RedirectURIs: []string{callback}},
		},
		{
			name:     "missing redirect uris",
			request:  registrationRequest{ClientName: "no redirects"},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name:     "empty redirect uris",
			request:  registrationRequest{RedirectURIs: []string{}},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name:     "too many redirect uris",
			request:  registrationRequest{RedirectURIs: tooManyRedirectURIs},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name:     "http redirect uri on a non-loopback host",
			request:  registrationRequest{RedirectURIs: []string{"http://client.example.com/callback"}},
			wantCode: registrationCodeInvalidRedirectURI,
		},
		{
			name:     "wildcard redirect uri",
			request:  registrationRequest{RedirectURIs: []string{"https://*.example.com/callback"}},
			wantCode: registrationCodeInvalidRedirectURI,
		},
		{
			name:     "relative redirect uri",
			request:  registrationRequest{RedirectURIs: []string{"/callback"}},
			wantCode: registrationCodeInvalidRedirectURI,
		},
		{
			name:     "redirect uri with a fragment",
			request:  registrationRequest{RedirectURIs: []string{callback + "#fragment"}},
			wantCode: registrationCodeInvalidRedirectURI,
		},
		{
			name: "unsupported token endpoint auth method",
			request: registrationRequest{
				RedirectURIs:            []string{callback},
				TokenEndpointAuthMethod: "client_secret_post",
			},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name: "unsupported grant type",
			request: registrationRequest{
				RedirectURIs: []string{callback},
				GrantTypes:   []string{"client_credentials"},
			},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name: "a repeated grant type",
			request: registrationRequest{
				RedirectURIs: []string{callback},
				GrantTypes:   []string{registrationGrantType, registrationGrantType},
			},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name: "unsupported response type",
			request: registrationRequest{
				RedirectURIs:  []string{callback},
				ResponseTypes: []string{"token"},
			},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name: "unknown scope",
			request: registrationRequest{
				RedirectURIs: []string{callback},
				Scope:        "metaxisdata.mcp.write",
			},
			wantCode: registrationCodeInvalidClientMetadata,
		},
		{
			name: "client name too long",
			request: registrationRequest{
				ClientName:   strings.Repeat("a", registrationMaxClientNameLength+1),
				RedirectURIs: []string{callback},
			},
			wantCode: registrationCodeInvalidClientMetadata,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, registrationErr := validateRegistrationRequest(tc.request)
			if tc.wantCode == "" {
				require.Nil(t, registrationErr)
				require.Equal(t, tc.want, got)
				return
			}
			require.NotNil(t, registrationErr)
			require.Equal(t, tc.wantCode, registrationErr.Code)
			require.NotEmpty(t, registrationErr.Description)
			require.Equal(t, tc.wantCode+": "+registrationErr.Description, registrationErr.Error())
			require.Equal(t, validatedRegistration{}, got)
		})
	}
}

// registrationErrorStatus is the status mapping the handler relies on.
func TestRegistrationErrorStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		want int
	}{
		{code: registrationCodeInvalidRequest, want: http.StatusBadRequest},
		{code: registrationCodeInvalidRedirectURI, want: http.StatusBadRequest},
		{code: registrationCodeInvalidClientMetadata, want: http.StatusBadRequest},
		{code: registrationCodeServerError, want: http.StatusInternalServerError},
		// A code this endpoint does not mint is still a client-side failure.
		{code: "invalid_software_statement", want: http.StatusBadRequest},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, registrationErrorStatus(tc.code), tc.code)
	}
}

func TestWriteRegistrationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want int
	}{
		{name: "invalid_request is a 400", code: registrationCodeInvalidRequest, want: http.StatusBadRequest},
		{name: "invalid_redirect_uri is a 400", code: registrationCodeInvalidRedirectURI, want: http.StatusBadRequest},
		{name: "invalid_client_metadata is a 400", code: registrationCodeInvalidClientMetadata, want: http.StatusBadRequest},
		{name: "server_error is a 500", code: registrationCodeServerError, want: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			writeRegistrationError(recorder, newRegistrationError(tc.code, "something went wrong"))

			response := recorder.Result()
			defer func() { _ = response.Body.Close() }()
			require.Equal(t, tc.want, response.StatusCode)
			require.Equal(t, "application/json", response.Header.Get("Content-Type"))
			require.Equal(t, "no-store", response.Header.Get("Cache-Control"))

			var body map[string]string
			require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
			require.Equal(t, map[string]string{
				"error":             tc.code,
				"error_description": "something went wrong",
			}, body)
		})
	}
}

// The success path end to end (mint a client_id, persist the row, answer 201)
// needs the database, so the integration suite covers it and this file does not
// fake a store. This pins the envelope and headers of the response writer.
func TestWriteRegistrationResponse(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeRegistrationJSON(recorder, http.StatusCreated, registrationResponse{
		ClientID:                "minted-client-id",
		ClientName:              "Claude Desktop",
		RedirectURIs:            []string{"https://client.example.com/callback"},
		TokenEndpointAuthMethod: registrationAuthMethodNone,
		GrantTypes:              []string{registrationGrantType},
		ResponseTypes:           []string{registrationResponseType},
	})

	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusCreated, response.StatusCode)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))

	var body map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, map[string]any{
		"client_id":                  "minted-client-id",
		"client_name":                "Claude Desktop",
		"redirect_uris":              []any{"https://client.example.com/callback"},
		"token_endpoint_auth_method": "none",
		"grant_types":                []any{"authorization_code"},
		"response_types":             []any{"code"},
	}, body)
}
