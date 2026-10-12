package store

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func oauth2IdentityProviderConfig(clientSecret string) *storepb.IdentityProviderConfig {
	return &storepb.IdentityProviderConfig{
		Config: &storepb.IdentityProviderConfig_Oauth2Config{
			Oauth2Config: &storepb.OAuth2IdentityProviderConfig{
				ClientId:     "client-id",
				ClientSecret: clientSecret,
				TokenUrl:     "https://idp.example.com/token",
				UserInfoUrl:  "https://idp.example.com/userinfo",
			},
		},
	}
}

// The stored row must never carry the secret in the clear, and decrypting the
// stored value must give it back. The original message is left alone so a caller
// that keeps using it after a write does not hold a ciphertext.
func TestIdentityProviderSecretIsEncryptedAtRest(t *testing.T) {
	t.Parallel()

	s := &Store{}
	s.SetCipher(newTestCipher(t))

	config := oauth2IdentityProviderConfig("s3cr3t")
	encrypted, err := s.encryptIdentityProviderConfig(config)
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(encrypted.GetOauth2Config().GetClientSecret()),
		"the stored client secret is not a ciphertext")
	require.Equal(t, "s3cr3t", config.GetOauth2Config().GetClientSecret(),
		"encryption mutated the caller's message")

	decrypted, err := s.decryptIdentityProviderConfig(encrypted)
	require.NoError(t, err)
	require.Equal(t, "s3cr3t", decrypted.GetOauth2Config().GetClientSecret())
	require.Equal(t, "client-id", decrypted.GetOauth2Config().GetClientId(),
		"the non-secret half of the config must survive the round trip")
}

// An empty secret is a state, not a value: it stays empty so "keep the stored
// secret" can be represented by an absent one.
func TestIdentityProviderEmptySecretStaysEmpty(t *testing.T) {
	t.Parallel()

	s := &Store{}
	s.SetCipher(newTestCipher(t))

	encrypted, err := s.encryptIdentityProviderConfig(oauth2IdentityProviderConfig(""))
	require.NoError(t, err)
	require.Empty(t, encrypted.GetOauth2Config().GetClientSecret())

	decrypted, err := s.decryptIdentityProviderConfig(oauth2IdentityProviderConfig(""))
	require.NoError(t, err)
	require.Empty(t, decrypted.GetOauth2Config().GetClientSecret())
}

// A secret that is not a ciphertext this deployment can open is an error. The
// alternative — handing the stored bytes to the provider as the credential —
// would present a key mistake as a wrong secret.
func TestIdentityProviderSecretReadFailsClosed(t *testing.T) {
	t.Parallel()

	t.Run("plaintext row", func(t *testing.T) {
		t.Parallel()
		s := &Store{}
		s.SetCipher(newTestCipher(t))
		_, err := s.decryptIdentityProviderConfig(oauth2IdentityProviderConfig("plaintext-secret"))
		require.Error(t, err)
	})

	t.Run("no cipher installed", func(t *testing.T) {
		t.Parallel()
		s := &Store{}
		_, err := s.decryptIdentityProviderConfig(oauth2IdentityProviderConfig("v1:whatever"))
		require.Error(t, err)
	})
}

// A config with no OAuth2 half is returned untouched rather than rejected: the
// type check constraint is what keeps non-OAuth2 rows out, not this helper.
func TestIdentityProviderConfigWithoutOAuth2IsPassedThrough(t *testing.T) {
	t.Parallel()

	s := &Store{}
	s.SetCipher(newTestCipher(t))

	config := &storepb.IdentityProviderConfig{}
	encrypted, err := s.encryptIdentityProviderConfig(config)
	require.NoError(t, err)
	require.Nil(t, encrypted.GetOauth2Config())

	decrypted, err := s.decryptIdentityProviderConfig(config)
	require.NoError(t, err)
	require.Nil(t, decrypted.GetOauth2Config())
}

// An update that names no field must fail before it reaches the database: the
// alternative is a generated `SET  WHERE` statement.
func TestUpdateIdentityProviderRejectsAnEmptyPatch(t *testing.T) {
	t.Parallel()

	s := &Store{}
	_, err := s.UpdateIdentityProvider(t.Context(), &UpdateIdentityProviderMessage{ResourceID: "acme"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no field to update")
}

// Every credential-like field of the stored config must be in the registry that
// actually drives encryption. `client_secret` is stored encrypted in place, so
// there is no `_ciphertext` sibling to catch it: this walk is what fails when a
// future field is added and forgotten, and forgetting it means writing that
// credential to the database in the clear.
func TestEveryIdentityProviderCredentialIsEncrypted(t *testing.T) {
	t.Parallel()

	registered := map[string]bool{}
	for _, field := range idpCredentialFields() {
		registered[field.name] = true
	}
	require.True(t, registered["client_secret"], "client_secret must be registered")

	// Walk the outer message and everything nested in it. A credential on
	// IdentityProviderConfig itself, or inside FieldMapping or a future nested
	// block, is exactly as exposed as one on the OAuth2 config.
	visited := map[protoreflect.FullName]bool{}
	var walk func(descriptor protoreflect.MessageDescriptor)
	walk = func(descriptor protoreflect.MessageDescriptor) {
		if visited[descriptor.FullName()] {
			return
		}
		visited[descriptor.FullName()] = true
		fields := descriptor.Fields()
		for i := range fields.Len() {
			field := fields.Get(i)
			if looksLikeACredential(string(field.Name())) {
				require.True(t, registered[string(field.Name())],
					"the stored field %s looks like a credential but is not registered with the credential cipher", field.Name())
			}
			if field.Kind() == protoreflect.MessageKind && !field.IsMap() {
				walk(field.Message())
			}
		}
	}
	walk((&storepb.IdentityProviderConfig{}).ProtoReflect().Descriptor())

	// And the registry must not name a field that no longer exists, which would
	// otherwise encrypt nothing while reading as if it did.
	inner := (&storepb.OAuth2IdentityProviderConfig{}).ProtoReflect().Descriptor().Fields()
	for name := range registered {
		require.NotNil(t, inner.ByName(protoreflect.Name(name)),
			"registered credential field %q has no stored counterpart", name)
	}
}

// The config JSONB holds the marshalled **outer** `IdentityProviderConfig`, so
// decoding it has to read the oneof wrapper. Decoding the inner message instead
// silently drops every field under `DiscardUnknown` and leaves a provider whose
// logins fail on a blank mapping — which is what this pins shut.
func TestIdentityProviderConfigRoundTripsThroughTheColumn(t *testing.T) {
	t.Parallel()

	s := &Store{}
	s.SetCipher(newTestCipher(t))

	config := oauth2IdentityProviderConfig("s3cr3t")
	config.GetOauth2Config().FieldMapping = &storepb.FieldMapping{
		Identifier:  "email",
		Subject:     "sub",
		DisplayName: "name",
		Groups:      "groups",
	}

	encrypted, err := s.encryptIdentityProviderConfig(config)
	require.NoError(t, err)
	// Exactly what the write path puts in the column.
	stored, err := protojson.Marshal(encrypted)
	require.NoError(t, err)
	require.Contains(t, string(stored), "oauth2Config",
		"the column no longer holds the outer IdentityProviderConfig")

	decoded, err := s.convertIdentityProviderConfigString(storepb.IdentityProviderType_OAUTH2, string(stored))
	require.NoError(t, err)
	oauth2Config := decoded.GetOauth2Config()
	require.NotNil(t, oauth2Config)
	require.Equal(t, "s3cr3t", oauth2Config.GetClientSecret(), "the secret did not survive the round trip")
	require.Equal(t, "sub", oauth2Config.GetFieldMapping().GetSubject())
	require.Equal(t, "email", oauth2Config.GetFieldMapping().GetIdentifier())
	require.Equal(t, "name", oauth2Config.GetFieldMapping().GetDisplayName())
	require.Equal(t, "groups", oauth2Config.GetFieldMapping().GetGroups())
	require.Equal(t, "client-id", oauth2Config.GetClientId())
}

// A row this build cannot serve is an error rather than an empty config, and a
// type with no config at all is a nil config rather than a failure.
func TestIdentityProviderConfigRejectsARowItCannotServe(t *testing.T) {
	t.Parallel()

	s := &Store{}
	s.SetCipher(newTestCipher(t))

	for _, stored := range []string{
		`{"oidcConfig":{"issuer":"https://idp.example.com"}}`,
		`{}`,
		// The inner message, which is what the old integration test hand-wrote and
		// what this read path used to expect. It is refused now on purpose: the
		// column holds the outer message, and accepting both would mean guessing
		// which shape a row is.
		`{"clientId":"client","clientSecret":"v1:whatever","fieldMapping":{"identifier":"email","subject":"sub"}}`,
	} {
		_, err := s.convertIdentityProviderConfigString(storepb.IdentityProviderType_OAUTH2, stored)
		require.Error(t, err, "stored config %s was accepted", stored)
	}

	decoded, err := s.convertIdentityProviderConfigString(storepb.IdentityProviderType_IDENTITY_PROVIDER_TYPE_UNSPECIFIED, "{}")
	require.NoError(t, err)
	require.Nil(t, decoded)
}
