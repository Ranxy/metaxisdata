package store

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
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

// credentialWords are the name fragments that mean "this field carries a
// credential". A test has no other handle on a new field — nothing but its name
// says what it holds — so the list is deliberately broad, and any field matching
// one of them has to be a registered half of a plaintext/ciphertext pair. It
// starts from the words the audit redactor already treats as sensitive, because
// the two lists disagreeing is how a field slips through both.
var credentialWords = []string{
	"password", "passwd", "pwd", "passphrase", "secret", "token", "credential",
	"apikey", "api_key", "privatekey", "private_key", "keytab", "bearer",
	"jwt", "session", "auth", "dsn", "connection_string", "key",
}

// unencryptedCredentialLikeFields are the fields whose names match a credential
// word but hold material that is public by design, with the reason. It is one
// registry for every stored message, so an exemption is argued once and applies
// wherever a guard walks a descriptor.
var unencryptedCredentialLikeFields = map[string]string{
	// A bastion's public host key is pinned, not hidden: it is what the SSH client
	// compares the presented key against, and the operator pastes it in.
	"ssh_host_key": "a public host key, not a credential",
	// An OAuth2 identity provider advertises these to the browser: the client
	// builds the authorize URL from them, and every value is visible in the URL a
	// visitor is redirected to. The secret they are paired with is client_secret,
	// which is encrypted.
	"auth_url":   "the authorization endpoint a browser is sent to, not a credential",
	"token_url":  "the token endpoint's address, not the token it issues",
	"auth_style": "which transport carries the client credentials, not the credentials",
	// A oneof selector names which protocol's config follows; it holds no value of
	// its own, and the walk recurses into the message it selects, so a credential
	// inside that config is still checked.
	"oauth2_config": "a oneof selector naming the OAuth2 config, not a value",
}

func looksLikeACredential(name string) bool {
	if _, ok := unencryptedCredentialLikeFields[name]; ok {
		return false
	}
	name = strings.ToLower(name)
	for _, word := range credentialWords {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
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
		if strings.HasSuffix(name, "_ciphertext") {
			ciphertextFields++
			require.True(t, registered[strings.TrimSuffix(name, "_ciphertext")],
				"the stored field %s is not registered with the credential cipher", name)
			continue
		}
		if looksLikeACredential(name) {
			require.True(t, registered[name],
				"the stored field %s looks like a credential but is not registered with the credential cipher", name)
		}
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

	for i := range fields.Len() {
		name := string(fields.Get(i).Name())
		if looksLikeACredential(name) {
			require.Equal(t, "api_key_ciphertext", name,
				"the stored field %s looks like a credential: it must be the ciphertext one", name)
		}
	}
}

// A metadata patch is a merge, not a replace: fields the patch does not name keep
// their stored value, including the encrypted credentials. This is what lets the
// schema syncer update last_sync_time without reverting a concurrent edit to the
// title, the data sources or the activation flag.
func TestApplyInstanceMetadataUpdatesKeepsFieldsItDoesNotName(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	lastSyncAt := timestamppb.Now()
	stored := &storepb.Instance{
		Title:      "prod orders",
		Activation: true,
		DataSources: []*storepb.DataSource{{
			Id:       "admin",
			Type:     storepb.DataSourceType_ADMIN,
			Host:     "db.internal",
			Port:     "3306",
			Username: "root",
			Password: "hunter2",
		}},
	}
	encrypted, err := encryptInstanceMetadata(stored, cipher)
	require.NoError(t, err)
	raw, err := protojson.Marshal(encrypted)
	require.NoError(t, err)

	patched, err := applyInstanceMetadataUpdates(raw, cipher, []func(*storepb.Instance){
		func(metadata *storepb.Instance) {
			metadata.LastSyncTime = lastSyncAt
			metadata.Version = "8.0.36"
			metadata.MysqlLowerCaseTableNames = 1
		},
	})
	require.NoError(t, err)

	got := &storepb.Instance{}
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(patched, got))
	require.NoError(t, decryptInstance(got, cipher))
	require.Equal(t, "prod orders", got.GetTitle())
	require.True(t, got.GetActivation())
	require.Equal(t, "8.0.36", got.GetVersion())
	require.Equal(t, int32(1), got.GetMysqlLowerCaseTableNames())
	require.Equal(t, lastSyncAt.AsTime(), got.GetLastSyncTime().AsTime())
	// The credential the sync did not touch survives the patch, still decrypted
	// to the same plaintext.
	require.Equal(t, "hunter2", got.GetDataSources()[0].GetPassword())
	require.NotEmpty(t, got.GetDataSources()[0].GetPasswordCiphertext())
	require.Equal(t, "db.internal", got.GetDataSources()[0].GetHost())
}

// A patch and a whole-metadata write are two different contracts; accepting both
// in one message would make "the last one wins" depend on argument order.
func TestUpdateInstanceRejectsMetadataAndMetadataUpdatesTogether(t *testing.T) {
	t.Parallel()

	_, err := (&Store{}).UpdateInstance(context.Background(), &UpdateInstanceMessage{
		ResourceID:      "inst-1",
		Metadata:        &storepb.Instance{},
		MetadataUpdates: []func(*storepb.Instance){func(*storepb.Instance) {}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

// The API writes one edited field, the schema syncer writes the sync bookkeeping;
// the two patches merge instead of the later one restoring the metadata it read
// before the other wrote. Updating the title must leave last_sync_time, version
// and the case-sensitivity flag exactly as the syncer stored them.
func TestApplyInstanceMetadataUpdatesKeepsTheSyncFields(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	lastSyncAt := timestamppb.Now()
	stored := &storepb.Instance{
		Title:                    "prod orders",
		Activation:               false,
		LastSyncTime:             lastSyncAt,
		Version:                  "8.0.36",
		MysqlLowerCaseTableNames: 1,
		DataSources: []*storepb.DataSource{{
			Id:       "admin",
			Type:     storepb.DataSourceType_ADMIN,
			Host:     "db.internal",
			Password: "hunter2",
		}},
	}
	encrypted, err := encryptInstanceMetadata(stored, cipher)
	require.NoError(t, err)
	raw, err := protojson.Marshal(encrypted)
	require.NoError(t, err)

	// What UpdateInstance passes for an update_mask of ["title", "activation"].
	patched, err := applyInstanceMetadataUpdates(raw, cipher, []func(*storepb.Instance){
		func(metadata *storepb.Instance) { metadata.Title = "renamed" },
		func(metadata *storepb.Instance) { metadata.Activation = true },
	})
	require.NoError(t, err)

	got := &storepb.Instance{}
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(patched, got))
	require.NoError(t, decryptInstance(got, cipher))
	require.Equal(t, "renamed", got.GetTitle())
	require.True(t, got.GetActivation())
	require.Equal(t, lastSyncAt.AsTime(), got.GetLastSyncTime().AsTime(), "the sync time must survive an API patch")
	require.Equal(t, "8.0.36", got.GetVersion())
	require.Equal(t, int32(1), got.GetMysqlLowerCaseTableNames())
	require.Equal(t, "hunter2", got.GetDataSources()[0].GetPassword())
}
