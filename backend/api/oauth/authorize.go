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

// ValidateRedirectURI reports whether a client may register this redirect URI.
// It is the registration-time half of the rule MatchesRedirectURI enforces at
// request time: https, or http on a loopback address, never a fragment and never
// a wildcard.
func ValidateRedirectURI(raw string) error {
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

// RedirectWithError sends the user agent back to the client with an RFC 6749
// error, preserving the client's state. It is how the authorization endpoint
// reports a failure that happened after a valid client_id and redirect_uri were
// established; a failure before that must be rendered by the server instead,
// because redirecting to an unvalidated URI is exactly the open redirect this
// endpoint exists to avoid.
func RedirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, clientState, code, description string) {
	parameters := url.Values{"error": {code}}
	if description != "" {
		parameters.Set("error_description", description)
	}
	if clientState != "" {
		parameters.Set("state", clientState)
	}
	target := redirectURI
	if strings.Contains(target, "?") {
		target += "&" + parameters.Encode()
	} else {
		target += "?" + parameters.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// ErrRedirectURINotRegistered is returned when an authorization request names a
// redirect URI the client did not register. The caller must not redirect.
var ErrRedirectURINotRegistered = errors.New("the redirect_uri is not registered for this client")
