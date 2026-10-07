package store

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func newTestCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	cipher, err := crypto.NewCipher(key)
	require.NoError(t, err)
	return cipher
}

func newTestKey(t *testing.T) []byte {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	return key
}

// A store without an installed cipher must refuse to read a credential instead
// of treating an unset cipher as "no encryption".
func TestCredentialCipherWithoutOneFails(t *testing.T) {
	t.Parallel()

	s := &Store{}
	_, err := s.credentialCipher()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestSetCipherInstallsTheCipher(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	s := &Store{}
	s.SetCipher(cipher)

	got, err := s.credentialCipher()
	require.NoError(t, err)
	require.Same(t, cipher, got)
}

func TestUnwrapDataKeyBareKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	got, err := unwrapDataKey(base64.StdEncoding.EncodeToString(key), nil)
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.Equal(t, -1, got.wrappedBy)

	for name, stored := range map[string]string{
		"not base64": "!!!",
		"short":      base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		_, err := unwrapDataKey(stored, nil)
		require.Error(t, err, name)
	}
}

func TestUnwrapDataKeyWrappedKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	current := newTestKey(t)
	previous := newTestKey(t)

	// The current key opens it, and the caller learns it needs no re-wrap.
	got, err := unwrapDataKey(wrapKey(t, current, key), [][]byte{current, previous})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.Equal(t, 0, got.wrappedBy)

	// A retired key opens it, and the caller learns it must be re-wrapped.
	got, err = unwrapDataKey(wrapKey(t, previous, key), [][]byte{current, previous})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.Equal(t, 1, got.wrappedBy)

	// A wrapped key with no key-encryption key configured says so, rather than
	// trying to decode it as a bare key.
	_, err = unwrapDataKey(wrapKey(t, current, key), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "none is configured")

	_, err = unwrapDataKey(wrapKey(t, newTestKey(t), key), [][]byte{current, previous})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configured key-encryption key")
}

// The wrapping round-trip the server performs when it re-wraps a bare data key
// must produce a value this code can open again.
func TestWrappedDataKeyRoundTrips(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)
	kekCipher, err := crypto.NewCipher(kek)
	require.NoError(t, err)

	wrapped, err := kekCipher.Encrypt(string(key))
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(wrapped))
	require.False(t, strings.Contains(wrapped, base64.StdEncoding.EncodeToString(key)))

	got, err := unwrapDataKey(wrapped, [][]byte{kek})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
}

// fakeSettings is an in-memory setting table, so the key lifecycle can be tested
// without PostgreSQL. GetSetting returns (nil, nil) for an absent row, exactly as
// the store does.
type fakeSettings struct {
	values map[storepb.SettingName]string
}

func newFakeSettings() *fakeSettings {
	return &fakeSettings{values: map[storepb.SettingName]string{}}
}

func (f *fakeSettings) GetSetting(_ context.Context, name storepb.SettingName) (*SettingMessage, error) {
	value, ok := f.values[name]
	if !ok {
		return nil, nil
	}
	return &SettingMessage{Name: name, Value: value}, nil
}

func (f *fakeSettings) UpsertSetting(_ context.Context, update *SetSettingMessage) (*SettingMessage, error) {
	f.values[update.Name] = update.Value
	return &SettingMessage{Name: update.Name, Value: update.Value}, nil
}

func (f *fakeSettings) stored() string {
	return f.values[storepb.SettingName_ENCRYPTION_KEY]
}

func (f *fakeSettings) set(t *testing.T, value string) {
	t.Helper()
	f.values[storepb.SettingName_ENCRYPTION_KEY] = value
}

// Without a key-encryption key the deployment is zero-config: the data key is
// generated into the setting table and stays there in the clear.
func TestLoadCredentialCipherKeepsTheKeyBareWithoutAKeyEncryptionKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	settings := newFakeSettings()
	settings.set(t, base64.StdEncoding.EncodeToString(key))

	cipher, err := loadCredentialCipher(t.Context(), settings, nil)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(key), settings.stored(), "the key must not be rewritten")

	// The cipher really is the deployment key.
	encrypted, err := cipher.Encrypt("hunter2")
	require.NoError(t, err)
	decrypted, err := mustCipher(t, key).Decrypt(encrypted)
	require.NoError(t, err)
	require.Equal(t, "hunter2", decrypted)
}

// A key-encryption key configured after the fact wraps the existing data key in
// place: the credentials themselves are untouched.
func TestLoadCredentialCipherWrapsABareKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)
	settings := newFakeSettings()
	settings.set(t, base64.StdEncoding.EncodeToString(key))

	cipher, err := loadCredentialCipher(t.Context(), settings, [][]byte{kek})
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(settings.stored()), "the stored key must be wrapped")
	require.NotContains(t, settings.stored(), base64.StdEncoding.EncodeToString(key))

	// A later startup with the same key-encryption key still resolves the same
	// data key, and it is not rewritten a second time.
	stored := settings.stored()
	again, err := loadCredentialCipher(t.Context(), settings, [][]byte{kek})
	require.NoError(t, err)
	require.Equal(t, stored, settings.stored(), "a key already wrapped under the current key must be left alone")

	for _, c := range []*crypto.Cipher{cipher, again} {
		encrypted, err := c.Encrypt("hunter2")
		require.NoError(t, err)
		decrypted, err := mustCipher(t, key).Decrypt(encrypted)
		require.NoError(t, err)
		require.Equal(t, "hunter2", decrypted)
	}
}

// A rotated key-encryption key re-wraps the data key, so the retired one can be
// dropped: this is what makes a rotation possible without re-encrypting a
// single credential.
func TestLoadCredentialCipherRewrapsUnderTheCurrentKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	retired := newTestKey(t)
	current := newTestKey(t)
	settings := newFakeSettings()
	settings.set(t, wrapKey(t, retired, key))

	cipher, err := loadCredentialCipher(t.Context(), settings, [][]byte{current, retired})
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(settings.stored()))
	require.NotEqual(t, wrapKey(t, retired, key), settings.stored(), "the stored key must be re-wrapped under the current key")

	// The retired key is no longer needed, and the data key is unchanged.
	_, err = loadCredentialCipher(t.Context(), settings, [][]byte{current})
	require.NoError(t, err)
	encrypted, err := cipher.Encrypt("hunter2")
	require.NoError(t, err)
	decrypted, err := mustCipher(t, key).Decrypt(encrypted)
	require.NoError(t, err)
	require.Equal(t, "hunter2", decrypted)
}

// Every way the key material can be wrong stops startup rather than leaving the
// store without a cipher.
func TestLoadCredentialCipherFailsClosed(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)

	cases := map[string]struct {
		settings *fakeSettings
		keks     [][]byte
		want     string
	}{
		"no setting row": {
			settings: newFakeSettings(),
			want:     "not configured",
		},
		"empty setting value": {
			settings: newFakeSettings(),
			want:     "not configured",
		},
		"wrapped key with no key-encryption key": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: wrapKey(t, kek, key)}},
			want:     "none is configured",
		},
		"no key-encryption key opens it": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: wrapKey(t, kek, key)}},
			keks:     [][]byte{newTestKey(t)},
			want:     "no configured key-encryption key",
		},
		"stored value is not a key": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: "not-a-key"}},
			want:     "neither a wrapped key nor base64",
		},
	}
	for name, tc := range cases {
		if name == "empty setting value" {
			tc.settings.set(t, "")
		}
		cipher, err := loadCredentialCipher(t.Context(), tc.settings, tc.keks)
		require.Error(t, err, name)
		require.Contains(t, err.Error(), tc.want, name)
		require.Nil(t, cipher, name)
	}
}

func mustCipher(t *testing.T, key []byte) *crypto.Cipher {
	t.Helper()
	cipher, err := crypto.NewCipher(key)
	require.NoError(t, err)
	return cipher
}

func wrapKey(t *testing.T, kek, key []byte) string {
	t.Helper()
	wrapped, err := mustCipher(t, kek).Encrypt(string(key))
	require.NoError(t, err)
	return wrapped
}
