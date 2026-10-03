package v1

import (
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// validateIDPSubjectMapping checks an identity provider's claim mapping before a
// login reaches the provider at all, so a misconfiguration is reported as one
// instead of surfacing as a failed sign-in.
//
// An account is bound to a subject the provider assigns and the user cannot set
// to someone else's value, so the mapping has to name one — unless the workspace
// has turned on the email-identity switch, which accepts the mutable address
// claim instead.
func validateIDPSubjectMapping(mapping *storepb.FieldMapping, allowEmailIdentity bool) error {
	if allowEmailIdentity {
		return nil
	}
	if mapping.GetSubject() == "" {
		return errors.New(`the field "fieldMapping.subject" is empty but required: map a stable claim such as the OIDC "sub", or turn on "Allow SSO by email identity"`)
	}
	if mapping.GetSubject() == mapping.GetIdentifier() {
		return errors.Errorf(`the field "fieldMapping.subject" has to name a stable claim distinct from "fieldMapping.identifier" (%q)`, mapping.GetIdentifier())
	}
	return nil
}

// idpLoginSubject returns the identity a successful SSO login is bound to.
//
// The subject claim is that identity. With the email-identity switch on — and
// only then — the address claim stands in for it, which asserts that the
// provider verifies addresses: an unverified claim would let whoever can set it
// sign in as the account it names. A subject claim mapped to something else is
// still honoured, and so is a login that reaches this point without one, which
// fails rather than silently falling back to the address.
func idpLoginSubject(mapping *storepb.FieldMapping, info *storepb.IdentityProviderUserInfo, allowEmailIdentity bool) (string, error) {
	useEmail := allowEmailIdentity && (mapping.GetSubject() == "" || mapping.GetSubject() == mapping.GetIdentifier())

	if !useEmail {
		if subject := info.GetSubject(); subject != "" {
			return subject, nil
		}
		return "", errors.New(`missing subject in the user info: map a stable claim such as the OIDC "sub"`)
	}

	// Addresses are case-insensitive and are stored lower-cased everywhere else,
	// so the binding has to be lower-cased too: a provider that reports the same
	// address with another capitalization would otherwise strand the account.
	if email := strings.ToLower(info.GetIdentifier()); email != "" {
		return email, nil
	}
	return "", errors.New("missing identifier (email) in the user info")
}
