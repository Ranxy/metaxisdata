package oauth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// registrationMaxBodySize caps the metadata document this endpoint reads, so
	// an anonymous caller cannot make the server buffer an unbounded body.
	registrationMaxBodySize = 64 << 10
	// registrationMaxRedirectURIs bounds one registration's redirect list.
	registrationMaxRedirectURIs = 10
	// registrationMaxClientNameLength is the byte cap on client_name, which is
	// echoed back and later shown on the consent page.
	registrationMaxClientNameLength = 200
	// registrationClientIDLength is the length of a minted client_id: 43 base62
	// characters carry about 256 bits of entropy.
	registrationClientIDLength = 43
	// registrationClientIDAttempts bounds the retries when a minted client_id
	// collides with an existing one.
	registrationClientIDAttempts = 5
	// registrationAuthMethodNone is the only token endpoint auth method this
	// server accepts: the token endpoint requires PKCE, so a client secret would
	// only add a credential that can leak.
	registrationAuthMethodNone = "none"
	// registrationGrantType is the only grant the authorization server runs.
	registrationGrantType = "authorization_code"
	// registrationResponseType is the only response type it issues.
	registrationResponseType = "code"
)

// RFC 7591 error codes this endpoint returns.
const (
	registrationCodeInvalidRequest        = "invalid_request"
	registrationCodeInvalidRedirectURI    = "invalid_redirect_uri"
	registrationCodeInvalidClientMetadata = "invalid_client_metadata"
	registrationCodeServerError           = "server_error"
)

// registrationRequest is the RFC 7591 client metadata document as submitted.
// Only the fields this server honours are read; RFC 7591 requires unknown ones
// to be ignored.
type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

// validatedRegistration is the part of a request this server stores. The other
// fields are fixed by policy, so they are not carried past validation.
type validatedRegistration struct {
	ClientName   string
	RedirectURIs []string
}

// registrationError is an RFC 7591 error: the machine-readable code and a
// description that is safe to return to the anonymous caller.
type registrationError struct {
	Code        string
	Description string
}

// Error implements error.
func (e *registrationError) Error() string {
	return e.Code + ": " + e.Description
}

// RegisterHandler serves POST /oauth/register (RFC 7591 dynamic client
// registration).
//
// The caller wires the route and the per-IP rate limiter. Registration is
// anonymous and creates public clients only: grant_types and response_types are
// fixed and token_endpoint_auth_method is "none", because the authorization-code
// flow requires PKCE regardless.
func RegisterHandler(stores *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The workspace switch is checked before the method so that a disabled
		// deployment answers 404 to every verb instead of advertising the route
		// with a 405.
		resolution, err := ResolveEndpoints(r.Context(), stores)
		if err != nil {
			writeUnavailable(w)
			return
		}
		if !resolution.Enabled {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body := http.MaxBytesReader(w, r.Body, registrationMaxBodySize)
		defer func() { _ = body.Close() }()
		var request registrationRequest
		if err := json.NewDecoder(body).Decode(&request); err != nil {
			writeRegistrationError(w, newRegistrationError(registrationCodeInvalidRequest,
				"the request body must be a JSON client metadata document of at most 64 KiB"))
			return
		}

		registration, registrationErr := validateRegistrationRequest(request)
		if registrationErr != nil {
			writeRegistrationError(w, registrationErr)
			return
		}
		response, registrationErr := persistRegistration(r.Context(), stores, registration)
		if registrationErr != nil {
			writeRegistrationError(w, registrationErr)
			return
		}
		writeRegistrationJSON(w, http.StatusCreated, response)
	})
}

// validateRegistrationRequest checks a submitted client metadata document
// against RFC 7591 and this server's policy. It is pure, so the rules are
// testable without a workspace or a database.
func validateRegistrationRequest(request registrationRequest) (validatedRegistration, *registrationError) {
	clientName := strings.TrimSpace(request.ClientName)
	if len(clientName) > registrationMaxClientNameLength {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			"client_name must be at most 200 bytes")
	}

	if len(request.RedirectURIs) == 0 {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			"redirect_uris is required and must contain at least one URI")
	}
	if len(request.RedirectURIs) > registrationMaxRedirectURIs {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			"redirect_uris must contain at most 10 URIs")
	}
	redirectURIs := make([]string, 0, len(request.RedirectURIs))
	for _, redirectURI := range request.RedirectURIs {
		if err := ValidateRedirectURI(redirectURI); err != nil {
			return validatedRegistration{}, newRegistrationError(registrationCodeInvalidRedirectURI, err.Error())
		}
		redirectURIs = append(redirectURIs, redirectURI)
	}

	if request.TokenEndpointAuthMethod != "" && request.TokenEndpointAuthMethod != registrationAuthMethodNone {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			`token_endpoint_auth_method must be "none": the token endpoint requires PKCE and does not authenticate clients`)
	}
	if !registrationGrantTypesAccepted(request.GrantTypes) {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			`grant_types must be ["authorization_code"]`)
	}
	if !registrationResponseTypesAccepted(request.ResponseTypes) {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			`response_types must be ["code"]`)
	}
	if !RequestedScopesValid(ParseScopeList(request.Scope)) {
		return validatedRegistration{}, newRegistrationError(registrationCodeInvalidClientMetadata,
			"scope must be a space-delimited list of the supported scopes ("+MCPReadScope+")")
	}

	return validatedRegistration{ClientName: clientName, RedirectURIs: redirectURIs}, nil
}

// registrationGrantTypesAccepted reports whether the request omits grant_types
// or asks for the one grant this server implements.
func registrationGrantTypesAccepted(grantTypes []string) bool {
	if len(grantTypes) == 0 {
		return true
	}
	return len(grantTypes) == 1 && grantTypes[0] == registrationGrantType
}

// registrationResponseTypesAccepted reports whether the request omits
// response_types or asks for the one response type this server issues.
func registrationResponseTypesAccepted(responseTypes []string) bool {
	if len(responseTypes) == 0 {
		return true
	}
	return len(responseTypes) == 1 && responseTypes[0] == registrationResponseType
}

// persistRegistration mints a client_id and stores the registration, retrying
// when a minted id collides with an existing client. A store failure is logged
// and reported as server_error; the database error itself never reaches the
// anonymous caller.
func persistRegistration(ctx context.Context, stores *store.Store, registration validatedRegistration) (*registrationResponse, *registrationError) {
	for range registrationClientIDAttempts {
		clientID, err := common.RandomString(registrationClientIDLength)
		if err != nil {
			slog.Error("failed to mint an OAuth client_id", log.WithError(err))
			return nil, registrationServerFailure()
		}
		stored, err := stores.CreateOAuthClient(ctx, &store.OAuthClient{
			ClientID:                clientID,
			ClientName:              registration.ClientName,
			RedirectURIs:            registration.RedirectURIs,
			TokenEndpointAuthMethod: registrationAuthMethodNone,
		})
		if err != nil {
			if common.ErrorCode(err) != common.Conflict {
				slog.Error("failed to store an OAuth client registration", log.WithError(err))
				return nil, registrationServerFailure()
			}
			// A collision on a 43-character random id is astronomically
			// unlikely; retry with a fresh one rather than fail the request.
			continue
		}
		return &registrationResponse{
			ClientID:                stored.ClientID,
			ClientName:              stored.ClientName,
			RedirectURIs:            stored.RedirectURIs,
			TokenEndpointAuthMethod: registrationAuthMethodNone,
			GrantTypes:              []string{registrationGrantType},
			ResponseTypes:           []string{registrationResponseType},
		}, nil
	}
	slog.Error("failed to mint a unique OAuth client_id after retries")
	return nil, registrationServerFailure()
}

// registrationResponse is the RFC 7591 client information response, as served.
type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// registrationErrorBody is the RFC 7591 error response, as served.
type registrationErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func newRegistrationError(code, description string) *registrationError {
	return &registrationError{Code: code, Description: description}
}

func registrationServerFailure() *registrationError {
	return newRegistrationError(registrationCodeServerError, "the client registration could not be stored")
}

// writeRegistrationError renders an RFC 7591 error envelope with the HTTP status
// its error code maps to.
func writeRegistrationError(w http.ResponseWriter, registrationErr *registrationError) {
	writeRegistrationJSON(w, registrationErrorStatus(registrationErr.Code), registrationErrorBody{
		Error:            registrationErr.Code,
		ErrorDescription: registrationErr.Description,
	})
}

// registrationErrorStatus maps an RFC 7591 error code to its HTTP status. The
// client-side codes are all 400; only a store failure is a 500.
func registrationErrorStatus(code string) int {
	switch code {
	case registrationCodeInvalidRequest, registrationCodeInvalidRedirectURI, registrationCodeInvalidClientMetadata:
		return http.StatusBadRequest
	case registrationCodeServerError:
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

func writeRegistrationJSON(w http.ResponseWriter, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	// The response carries a freshly minted client_id, so it must never be
	// cached by a proxy or the browser.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}
