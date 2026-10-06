package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/state"
)

// MatchesRedirectURI reports whether the redirect_uri of an authorization request
// is one the client registered.
//
// Everything except loopback must match exactly: OAuth 2.1 requires exact
// matching because an approximate match is an open-redirect hole (a registered
// "https://app.example.com/cb" must not accept "https://app.example.com/cb.evil"
// or "https://app.example.com.evil/cb"). Loopback is the single documented
// relaxation (RFC 8252 §7.3): a native client picks an ephemeral port at
// runtime, so a loopback URI is compared without its port — but the scheme, host
// and path still have to agree.
func MatchesRedirectURI(registered []string, requested string) bool {
	if requested == "" {
		return false
	}
	// A client registered before the registration cap existed could still carry a
	// longer URI. A pending request copies whatever is matched here, so the cap
	// holds on this side too.
	if len(requested) > redirectURIMaxBytes {
		return false
	}
	parsed, err := url.Parse(requested)
	if err != nil || parsed.Fragment != "" {
		return false
	}
	for _, candidate := range registered {
		if candidate == requested {
			return true
		}
		if !isLoopbackRedirect(parsed) {
			continue
		}
		registeredParsed, err := url.Parse(candidate)
		if err != nil || !isLoopbackRedirect(registeredParsed) {
			continue
		}
		if registeredParsed.Scheme == parsed.Scheme &&
			strings.EqualFold(registeredParsed.Hostname(), parsed.Hostname()) &&
			registeredParsed.Path == parsed.Path &&
			registeredParsed.RawQuery == parsed.RawQuery {
			return true
		}
	}
	return false
}

// isLoopbackRedirect reports whether a URI is a loopback http redirect, which is
// the only case where the port may differ between registration and request.
func isLoopbackRedirect(parsed *url.URL) bool {
	return parsed.Scheme == "http" && isLoopbackHost(strings.ToLower(parsed.Hostname()))
}

// Bounds on what one anonymous authorization request may make the server keep.
// A pending request holds these values for ten minutes, the store holds up to
// 10000 of them, and the client's `state` is echoed back on every answer, so an
// unbounded value would let one caller decide how much memory — and how large a
// Location header — a request costs.
const (
	// redirectURIMaxBytes caps one redirect URI, registered or requested. It has
	// to hold a realistic URL, not a document.
	redirectURIMaxBytes = 2 << 10
	// authorizationMaxStateLength is the cap on the client's opaque `state`.
	authorizationMaxStateLength = 512
	// pkceChallengeLength is the length of an S256 code_challenge: RFC 7636 §4.2
	// defines it as the base64url-encoded SHA-256 of the verifier, so it is always
	// 43 characters and never the 43..128 range that belongs to the verifier.
	pkceChallengeLength = 43
	// authorizationMaxScopeBytes caps the scope parameter before it is parsed. One
	// scope is supported and may be repeated any number of times, so without this
	// the list a pending request stores is as large as the caller wants.
	authorizationMaxScopeBytes = 256
)

// ValidateRedirectURI reports whether a client may register this redirect URI.
// It is the registration-time half of the rule MatchesRedirectURI enforces at
// request time: https, or http on a loopback address, never a fragment and never
// a wildcard.
func ValidateRedirectURI(raw string) error {
	if len(raw) > redirectURIMaxBytes {
		return fmt.Errorf("redirect URI must be at most %d bytes", redirectURIMaxBytes)
	}
	if strings.Contains(raw, "*") {
		return fmt.Errorf("redirect URI %q must not contain a wildcard", raw)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("redirect URI %q is not a valid URL: %w", raw, err)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("redirect URI %q must not carry a fragment", raw)
	}
	if parsed.User != nil {
		// A client that registered "https://user@host/cb" would be sent to a URL
		// whose authority reads differently from the one an operator reviewed.
		return fmt.Errorf("redirect URI %q must not carry credentials", raw)
	}
	if parsed.Scheme == "https" && parsed.Host != "" {
		return nil
	}
	if isLoopbackRedirect(parsed) {
		return nil
	}
	return fmt.Errorf("redirect URI %q must use https, or http on a loopback address", raw)
}

// VerifyPKCE reports whether codeVerifier produces the S256 challenge stored with
// an authorization request. Only S256 is supported, so the comparison is a
// constant-time hash comparison; a caller must never accept a plain challenge.
func VerifyPKCE(challenge, codeVerifier string) bool {
	if challenge == "" || codeVerifier == "" {
		return false
	}
	digest := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(challenge), []byte(computed)) == 1
}

// RequestedScopesValid reports whether every requested scope is one this
// authorization server supports. An empty request is valid: the server then
// grants the one scope it has.
func RequestedScopesValid(requested []string) bool {
	for _, scope := range requested {
		if scope != MCPReadScope {
			return false
		}
	}
	return true
}

// ParseScopeList reads the OAuth `scope` parameter, which is a space-delimited
// list. An empty value yields no scopes rather than one empty scope.
func ParseScopeList(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// redirectWith sends the user agent to an already validated redirect target with
// the given query parameters.
func redirectWith(w http.ResponseWriter, r *http.Request, target string, parameters url.Values) {
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	http.Redirect(w, r, target+separator+parameters.Encode(), http.StatusFound)
}

// RedirectWithError sends the user agent back to the client with an RFC 6749
// error, preserving the client's state. It is how the authorization endpoint
// reports a failure that happened after a valid client_id and redirect_uri were
// established; a failure before that must be rendered by the server instead,
// because redirecting to an unvalidated URI is exactly the open redirect this
// endpoint exists to avoid.
// RedirectWithError reports a failure to the client. The issuer travels with the
// error exactly as it does with a code: RFC 9207's mix-up defence has to hold on
// the path where a client is told no, which is a response it can reach without
// ever seeing a successful one.
func RedirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, clientState, code, description, issuer string) {
	parameters := url.Values{"error": {code}}
	if description != "" {
		parameters.Set("error_description", description)
	}
	if clientState != "" {
		parameters.Set("state", clientState)
	}
	if issuer != "" {
		parameters.Set("iss", issuer)
	}
	redirectWith(w, r, redirectURI, parameters)
}

// RedirectWithCode sends the user agent back to the client with the authorization
// code, the client's opaque state and — per RFC 9207 — the issuer, which is what
// lets a client detect a mix-up between authorization servers.
func RedirectWithCode(w http.ResponseWriter, r *http.Request, redirectURI, code, clientState, issuer string) {
	parameters := url.Values{"code": {code}, "iss": {issuer}}
	if clientState != "" {
		parameters.Set("state", clientState)
	}
	redirectWith(w, r, redirectURI, parameters)
}

// ErrRedirectURINotRegistered is returned when an authorization request names a
// redirect URI the client did not register. The caller must not redirect.
var ErrRedirectURINotRegistered = errors.New("the redirect_uri is not registered for this client")

// deniedRequest returns the request when the user denied it, which is what lets
// the completion endpoint answer with the RFC 6749 `access_denied` error instead
// of leaving the client waiting for a redirect.
func deniedRequest(requests *state.OAuthAuthorizationRequestStore, requestID string, userID int, now time.Time) *state.OAuthAuthorizationRequest {
	record, err := requests.Get(requestID, userID, now)
	if err != nil || record.State != state.OAuthAuthorizationDenied {
		return nil
	}
	return &record
}

// echoedClientState returns the client's `state` for a redirect, or an empty
// string when it is over the cap. The request is refused either way, and echoing
// an over-long value would only put a request-sized value into a Location header.
func echoedClientState(raw string) string {
	if len(raw) > authorizationMaxStateLength {
		return ""
	}
	return raw
}

// authorizationRequest is a validated /oauth/authorize request.
type authorizationRequest struct {
	ClientID            string
	RedirectURI         string
	ClientState         string
	Resource            string
	Scopes              []string
	CodeChallenge       string
	CodeChallengeMethod string
}

// authorizationError is an RFC 6749 error the client has to be told about. It is
// reported by redirecting whenever the redirect target is trusted.
type authorizationError struct {
	Code        string
	Description string
}

func (e *authorizationError) Error() string { return e.Code + ": " + e.Description }

// parseAuthorizationRequest validates everything about an authorization request
// except the client and its redirect URI, which the handler checks first: until
// those are known, answering with a redirect would be an open redirect, so the
// handler has to know whether it may redirect at all.
func parseAuthorizationRequest(query url.Values, clientID, redirectURI string, endpoints Endpoints) (authorizationRequest, error) {
	request := authorizationRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		ClientState:         query.Get("state"),
		Resource:            query.Get("resource"),
		CodeChallenge:       query.Get("code_challenge"),
		CodeChallengeMethod: query.Get("code_challenge_method"),
	}
	// Both values are stored with the pending request and echoed back to the
	// client, so an unbounded one is memory the caller picked. A too-long state is
	// refused rather than shortened: half of an opaque value is not the value.
	if len(request.ClientState) > authorizationMaxStateLength {
		return authorizationRequest{}, &authorizationError{Code: "invalid_request", Description: fmt.Sprintf("state must be at most %d bytes", authorizationMaxStateLength)}
	}
	if query.Get("response_type") != "code" {
		return authorizationRequest{}, &authorizationError{Code: "unsupported_response_type", Description: "only response_type=code is supported"}
	}
	// PKCE is mandatory and S256-only: without it, a public client's code would be
	// redeemable by anyone who saw it leave the browser.
	if request.CodeChallengeMethod != "S256" {
		return authorizationRequest{}, &authorizationError{Code: "invalid_request", Description: "code_challenge_method must be S256"}
	}
	if len(request.CodeChallenge) != pkceChallengeLength {
		return authorizationRequest{}, &authorizationError{Code: "invalid_request", Description: "code_challenge must be 43 characters, the S256 digest of the verifier"}
	}
	// RFC 8707: the client names the resource it wants a token for, and it must
	// be this deployment's. The comparison is exact because both sides come from
	// the same canonicalisation; a difference means a different resource.
	if request.Resource != endpoints.Resource {
		return authorizationRequest{}, &authorizationError{Code: "invalid_request", Description: "resource must be " + endpoints.Resource}
	}
	scopes, scopeErr := requestedScopes(query.Get("scope"))
	if scopeErr != nil {
		return authorizationRequest{}, scopeErr
	}
	request.Scopes = scopes
	return request, nil
}

// requestedScopes validates the scope parameter and returns the scopes to grant.
// Exactly one scope exists, so a request that passes grants it once however many
// times it repeated it: the list a pending request stores is never caller-sized.
func requestedScopes(raw string) ([]string, *authorizationError) {
	if len(raw) > authorizationMaxScopeBytes {
		return nil, &authorizationError{Code: "invalid_scope", Description: "scope must be at most 256 bytes"}
	}
	if !RequestedScopesValid(ParseScopeList(raw)) {
		return nil, &authorizationError{Code: "invalid_scope", Description: "the only supported scope is " + MCPReadScope}
	}
	// Asking for nothing means asking for what the metadata advertises; asking for
	// the one scope any number of times means asking for it once.
	return []string{MCPReadScope}, nil
}

// AuthorizeHandler serves GET /oauth/authorize: it validates the request, makes
// sure a signed-in user is present, records a pending authorization request and
// sends the browser to the consent page.
func (s *Server) AuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoints, ok := s.endpoints(w, r)
		if !ok {
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeBrowserError(w, http.StatusMethodNotAllowed, "invalid_request", "the authorization endpoint is a GET")
			return
		}
		query := r.URL.Query()
		clientID := query.Get("client_id")
		redirectURI := query.Get("redirect_uri")

		// The client and its redirect URI come first: until both check out, any
		// answer that redirects would be an open redirect.
		client, err := s.config.Stores.GetOAuthClient(r.Context(), clientID)
		if err != nil {
			writeBrowserError(w, http.StatusInternalServerError, "server_error", "failed to load the client")
			return
		}
		if client == nil || !MatchesRedirectURI(client.RedirectURIs, redirectURI) {
			writeBrowserError(w, http.StatusBadRequest, "invalid_request", "unknown client_id, or a redirect_uri that is not registered for it")
			return
		}

		request, err := parseAuthorizationRequest(query, clientID, redirectURI, endpoints)
		if err != nil {
			code, description := "server_error", ""
			var authorizationErr *authorizationError
			if errors.As(err, &authorizationErr) {
				code, description = authorizationErr.Code, authorizationErr.Description
			}
			RedirectWithError(w, r, redirectURI, echoedClientState(query.Get("state")), code, description, endpoints.Issuer)
			return
		}

		user, err := s.sessionUser(r)
		if err != nil {
			// No usable session: the SPA signs the user in and returns here. This
			// redirect goes to our own sign-in page, never to the client.
			http.Redirect(w, r, signInURL(endpoints, r.URL.RequestURI()), http.StatusFound)
			return
		}

		pending, err := s.config.State.OAuthAuthorizationRequestStore.Create(state.OAuthAuthorizationRequest{
			ClientID:            clientID,
			ClientName:          client.ClientName,
			RedirectURI:         redirectURI,
			Resource:            request.Resource,
			Scopes:              request.Scopes,
			CodeChallenge:       request.CodeChallenge,
			CodeChallengeMethod: request.CodeChallengeMethod,
			ClientState:         request.ClientState,
			UserID:              user.ID,
			RequestIP:           s.requestIP(r),
		}, s.now())
		if err != nil {
			writeBrowserError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "too many authorization requests are pending")
			return
		}
		http.Redirect(w, r, consentURL(endpoints, pending.RequestID), http.StatusFound)
	})
}

// CompletionHandler serves GET /oauth/authorize/complete. The consent page
// navigates here after approval; the server mints the authorization code and
// redirects it to the client, so the code never travels through an RPC response
// or through page JavaScript.
func (s *Server) CompletionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoints, ok := s.endpoints(w, r)
		if !ok {
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeBrowserError(w, http.StatusMethodNotAllowed, "invalid_request", "the completion endpoint is a GET")
			return
		}
		user, err := s.sessionUser(r)
		if err != nil {
			http.Redirect(w, r, signInURL(endpoints, r.URL.RequestURI()), http.StatusFound)
			return
		}
		requestID := r.URL.Query().Get("request_id")
		requests := s.config.State.OAuthAuthorizationRequestStore
		completed, err := requests.Complete(requestID, user.ID, s.now())
		if err != nil {
			// A denial is not a dead end: RFC 6749 has the authorization server
			// tell the client, so it stops waiting for a callback that will never
			// come. Anything else has no target to redirect to and is rendered here.
			if denied := deniedRequest(requests, requestID, user.ID, s.now()); denied != nil {
				RedirectWithError(w, r, denied.RedirectURI, denied.ClientState, "access_denied", "the request was denied", endpoints.Issuer)
				return
			}
			writeBrowserError(w, http.StatusBadRequest, "invalid_request", "the authorization request was not approved, or is no longer valid")
			return
		}
		RedirectWithCode(w, r, completed.RedirectURI, completed.Code, completed.ClientState, endpoints.Issuer)
	})
}
