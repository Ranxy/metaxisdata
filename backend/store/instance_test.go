package store

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Reading a page of instances resolves the cipher once, so the per-row helper
// has to decrypt every credential field with the cipher it was handed.
func TestDecryptInstanceDecryptsEveryCredential(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	plaintext := []string{"password", "ca", "cert", "key", "ssh-password", "ssh-private-key"}

	ds := &storepb.DataSource{Id: "admin"}
	fields := credentialFields(ds)
	require.Len(t, fields, len(plaintext))
	for i, field := range fields {
		encrypted, err := cipher.Encrypt(plaintext[i])
		require.NoError(t, err)
		*field.ciphertext = encrypted
	}

	instance := &storepb.Instance{DataSources: []*storepb.DataSource{ds}}
	require.NoError(t, decryptInstance(instance, cipher))
	require.Equal(t, plaintext,
		[]string{ds.Password, ds.SslCa, ds.SslCert, ds.SslKey, ds.SshPassword, ds.SshPrivateKey})
}

// A credential that cannot be authenticated must fail the read and name the
// field: silently returning garbage is what let a bad key destroy every
// credential on the next write.
func TestDecryptInstanceFailsOnAnUnauthenticatedCredential(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	underAnotherKey, err := newTestCipher(t).Encrypt("password")
	require.NoError(t, err)

	for name, stored := range map[string]string{
		"retired obfuscation": base64.StdEncoding.EncodeToString([]byte("password")),
		"another key":         underAnotherKey,
	} {
		instance := &storepb.Instance{DataSources: []*storepb.DataSource{{
			Id:                 "admin",
			PasswordCiphertext: stored,
		}}}
		err := decryptInstance(instance, cipher)
		require.Error(t, err, name)
		require.Contains(t, err.Error(), "password", name)
		require.Contains(t, err.Error(), "admin", name)
	}

	// An empty credential is the "not set" state, not a value to decrypt.
	instance := &storepb.Instance{DataSources: []*storepb.DataSource{{Id: "admin"}}}
	require.NoError(t, decryptInstance(instance, cipher))
	require.Empty(t, instance.GetDataSources()[0].GetPassword())
}

// A credential written back to the database is only ever its ciphertext: the
// stored instance must not carry the plaintext.
func TestEncryptInstanceStoresOnlyTheCiphertext(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	s := &Store{}
	s.SetCipher(cipher)

	instance := &storepb.Instance{DataSources: []*storepb.DataSource{{
		Id:            "admin",
		Password:      "hunter2",
		SslCa:         "-----BEGIN CERTIFICATE-----",
		SshPrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
	}}}
	stored, err := s.encryptInstance(instance)
	require.NoError(t, err)

	ds := stored.GetDataSources()[0]
	require.Empty(t, ds.GetPassword())
	require.Empty(t, ds.GetSslCa())
	require.Empty(t, ds.GetSshPrivateKey())
	require.True(t, strings.HasPrefix(ds.GetPasswordCiphertext(), "v1:"))
	require.True(t, strings.HasPrefix(ds.GetSslCaCiphertext(), "v1:"))
	require.True(t, strings.HasPrefix(ds.GetSshPrivateKeyCiphertext(), "v1:"))

	// The caller's instance is untouched, and the ciphertext round-trips.
	require.Equal(t, "hunter2", instance.GetDataSources()[0].GetPassword())
	back := &storepb.Instance{DataSources: []*storepb.DataSource{{Id: "admin", PasswordCiphertext: ds.GetPasswordCiphertext()}}}
	require.NoError(t, decryptInstance(back, cipher))
	require.Equal(t, "hunter2", back.GetDataSources()[0].GetPassword())
}

// A store that never had a cipher installed must refuse to write a credential
// rather than storing it in the clear.
func TestEncryptInstanceWithoutACipherFails(t *testing.T) {
	t.Parallel()

	s := &Store{}
	_, err := s.encryptInstance(&storepb.Instance{DataSources: []*storepb.DataSource{{Id: "admin", Password: "hunter2"}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

// Every credential field of the stored shapes must be registered with the cipher.
// A field added to proto/store without an entry here would be written to the
// database in the clear, which is exactly what this test exists to prevent.
func TestEveryStoredCredentialFieldIsEncrypted(t *testing.T) {
	t.Parallel()

	ds := &storepb.DataSource{}
	registered := map[string]bool{}
	for _, field := range credentialFields(ds) {
		registered[field.name] = true

		plaintext := ds.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(field.name))
		require.NotNil(t, plaintext, "credential field %q has no plaintext field", field.name)
		require.NotNil(t, ds.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(field.name+"_ciphertext")),
			"credential field %q has no ciphertext field", field.name)
	}

	fields := ds.ProtoReflect().Descriptor().Fields()
	ciphertextFields := 0
	for i := range fields.Len() {
		name := string(fields.Get(i).Name())
		if !strings.HasSuffix(name, "_ciphertext") {
			continue
		}
		ciphertextFields++
		require.True(t, registered[strings.TrimSuffix(name, "_ciphertext")],
			"the stored field %s is not registered with the credential cipher", name)
	}
	require.Equal(t, ciphertextFields, len(registered), "a registered credential field has no stored counterpart")
}

// The LLM profile keeps its key in one field, and that field must be the
// ciphertext one: a field named for the key in the clear would be persisted.
func TestLLMProfileStoresOnlyTheCiphertext(t *testing.T) {
	t.Parallel()

	fields := (&storepb.LlmProviderProfile{}).ProtoReflect().Descriptor().Fields()
	require.NotNil(t, fields.ByName("api_key_ciphertext"))
	require.Nil(t, fields.ByName("api_key"))
}
