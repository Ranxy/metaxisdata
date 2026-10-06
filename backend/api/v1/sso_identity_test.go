package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The subject is the identity an account is bound to, so a provider that maps no
// subject — or maps the mutable address claim onto it — is refused unless the
// workspace has explicitly accepted the address as an identity.
func TestValidateIDPSubjectMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mapping     *storepb.FieldMapping
		allowEmail  bool
		containsErr string
	}{
		{
			name:    "a stable subject claim is accepted",
			mapping: &storepb.FieldMapping{Identifier: "email", Subject: "sub"},
		},
		{
			name:        "no subject claim is refused by default",
			mapping:     &storepb.FieldMapping{Identifier: "email"},
			containsErr: `the field "fieldMapping.subject" is empty: map a stable claim such as the OIDC "sub"`,
		},
		{
			name:        "the identifier claim as the subject is refused by default",
			mapping:     &storepb.FieldMapping{Identifier: "email", Subject: "email"},
			containsErr: `has to name a stable claim distinct from "fieldMapping.identifier"`,
		},
		{
			name:       "the email-identity switch accepts either",
			mapping:    &storepb.FieldMapping{Identifier: "email"},
			allowEmail: true,
		},
		{
			name:       "the email-identity switch accepts the identifier claim",
			mapping:    &storepb.FieldMapping{Identifier: "email", Subject: "email"},
			allowEmail: true,
		},
		{
			name:       "the email-identity switch tolerates a missing mapping",
			mapping:    nil,
			allowEmail: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateIDPSubjectMapping(test.mapping, test.allowEmail)
			if test.containsErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.containsErr)
		})
	}
}

// idpLoginSubject is what a login binds an account to: the provider's subject,
// or — only with the switch on and only when the address is what the mapping
// calls the identity — the lower-cased address claim.
func TestIDPLoginSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mapping     *storepb.FieldMapping
		info        *storepb.IdentityProviderUserInfo
		allowEmail  bool
		want        string
		containsErr string
	}{
		{
			name:    "the mapped subject wins",
			mapping: &storepb.FieldMapping{Identifier: "email", Subject: "sub"},
			info:    &storepb.IdentityProviderUserInfo{Identifier: "Alice@Example.com", Subject: "00u1"},
			want:    "00u1",
		},
		{
			name:        "a missing subject fails by default",
			mapping:     &storepb.FieldMapping{Identifier: "email", Subject: "sub"},
			info:        &storepb.IdentityProviderUserInfo{Identifier: "alice@example.com"},
			containsErr: "missing subject",
		},
		{
			name:       "the switch falls back to the address, lower-cased",
			mapping:    &storepb.FieldMapping{Identifier: "email"},
			info:       &storepb.IdentityProviderUserInfo{Identifier: "Alice@Example.com"},
			allowEmail: true,
			want:       "alice@example.com",
		},
		{
			name:       "a subject claim mapped onto the address is the address, lower-cased",
			mapping:    &storepb.FieldMapping{Identifier: "email", Subject: "email"},
			info:       &storepb.IdentityProviderUserInfo{Identifier: "Alice@Example.com", Subject: "Alice@Example.com"},
			allowEmail: true,
			want:       "alice@example.com",
		},
		{
			// An explicitly mapped subject that the response omits is not
			// silently replaced by the address the switch tolerates elsewhere.
			name:        "a mapped subject is not replaced by the address",
			mapping:     &storepb.FieldMapping{Identifier: "email", Subject: "sub"},
			info:        &storepb.IdentityProviderUserInfo{Identifier: "alice@example.com"},
			allowEmail:  true,
			containsErr: "missing subject",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			subject, err := idpLoginSubject(test.mapping, test.info, test.allowEmail)
			if test.containsErr != "" {
				require.ErrorContains(t, err, test.containsErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, subject)
		})
	}
}
