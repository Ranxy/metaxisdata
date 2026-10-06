package oauth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// ProtectedResourcePath is where the RFC 9728 document is served. Clients also
// try the path-insertion form, which the router mounts as well.
const ProtectedResourcePath = "/.well-known/oauth-protected-resource"

// EndpointsFunc reports the deployment's identifiers and whether the MCP surface
// is enabled at all. Handlers take one instead of reaching for the settings
// themselves, so a handler can be exercised without a database.
type EndpointsFunc func(ctx context.Context) (Endpoints, bool, error)

// WorkspaceEndpoints returns an EndpointsFunc backed by the workspace settings.
// It is read per request, so turning the surface off, or moving the deployment
// to another address, takes effect without a restart.
func WorkspaceEndpoints(stores *store.Store) EndpointsFunc {
	return func(ctx context.Context) (Endpoints, bool, error) {
		resolution, err := ResolveEndpoints(ctx, stores)
		if err != nil {
			return Endpoints{}, false, err
		}
		return resolution.Endpoints, resolution.Enabled, nil
	}
}

// Resolution is the outcome of reading the workspace settings for one request:
// whether the MCP surface is served at all, and, when it is, the identifiers the
// OAuth documents are built from.
type Resolution struct {
	Endpoints Endpoints
	Enabled   bool
}

// ResolveEndpoints reads the workspace's MCP switch and derives the OAuth
// identifiers from its external_url.
//
// It is read per request (the setting read is cached by the store), so turning
// the surface on or off, or moving the deployment to another address, takes
// effect without a restart.
func ResolveEndpoints(ctx context.Context, stores *store.Store) (Resolution, error) {
	setting, err := stores.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return Resolution{}, err
	}
	resolution := Resolution{Enabled: setting.GetMcpEnabled()}
	if !resolution.Enabled {
		return resolution, nil
	}
	endpoints, err := CanonicalizeExternalURL(setting.GetExternalUrl())
	if err != nil {
		return Resolution{}, err
	}
	resolution.Endpoints = endpoints
	return resolution, nil
}

// ProtectedResourceHandler serves the RFC 9728 document that tells an MCP client
// which authorization server to use for this resource. It answers 404 while the
// surface is switched off, so a disabled deployment does not advertise one.
func ProtectedResourceHandler(stores *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolution, err := ResolveEndpoints(r.Context(), stores)
		if err != nil {
			writeUnavailable(w)
			return
		}
		if !resolution.Enabled {
			http.NotFound(w, r)
			return
		}
		writeMetadata(w, resolution.Endpoints.ProtectedResourceMetadata())
	})
}

// AuthServerMetadataHandler serves the RFC 8414 document a client reads to find
// the authorization, token and registration endpoints.
func AuthServerMetadataHandler(stores *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolution, err := ResolveEndpoints(r.Context(), stores)
		if err != nil {
			writeUnavailable(w)
			return
		}
		if !resolution.Enabled {
			http.NotFound(w, r)
			return
		}
		writeMetadata(w, resolution.Endpoints.AuthServerMetadata())
	})
}

// ProtectedResourceMetadata is the RFC 9728 document, as served. It is a local
// struct rather than the SDK's so that the JSON this server publishes is
// literally what the tests read.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
}

// ProtectedResourceMetadata builds the metadata document for these endpoints.
func (e Endpoints) ProtectedResourceMetadata() ProtectedResourceMetadata {
	return ProtectedResourceMetadata{
		Resource:               e.Resource,
		AuthorizationServers:   []string{e.Issuer},
		ScopesSupported:        []string{MCPReadScope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "metaxisdata MCP",
	}
}

// AuthServerMetadata is the RFC 8414 document, as served. There is deliberately
// no jwks_uri: tokens are signed with the deployment's symmetric AUTH_SECRET and
// their only consumer is this same process, so no third party ever needs a public
// key. See docs/security-posture.md.
type AuthServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

// AuthServerMetadata builds the authorization server metadata document.
func (e Endpoints) AuthServerMetadata() AuthServerMetadata {
	return AuthServerMetadata{
		Issuer:                e.Issuer,
		AuthorizationEndpoint: e.AuthorizationEndpoint(),
		TokenEndpoint:         e.TokenEndpoint(),
		RegistrationEndpoint:  e.RegistrationEndpoint(),
		ScopesSupported:       []string{MCPReadScope},
		ResponseTypesSupported: []string{
			"code",
		},
		// Both grants: an access token is short-lived relative to a session, and
		// the refresh token issued beside it rotates into a new pair without
		// another browser round trip.
		GrantTypesSupported: []string{
			grantTypeAuthorizationCode,
			grantTypeRefreshToken,
		},
		CodeChallengeMethodsSupported: []string{
			"S256",
		},
		// Public clients only: a client that can keep a secret is not required,
		// and PKCE is mandatory for everyone.
		TokenEndpointAuthMethodsSupported: []string{
			"none",
		},
		AuthorizationResponseIssParameterSupported: true,
	}
}

func writeMetadata(w http.ResponseWriter, document any) {
	payload, err := json.Marshal(document)
	if err != nil {
		http.Error(w, "failed to encode metadata", http.StatusInternalServerError)
		return
	}
	// The documents are public configuration, and clients fetch them from another
	// origin during discovery.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func writeUnavailable(w http.ResponseWriter) {
	http.Error(w, "the MCP endpoint is not available: configure the workspace external_url and enable it first", http.StatusServiceUnavailable)
}
