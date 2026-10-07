package store

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func newTestCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key := newTestKey(t)
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
	got, err := unwrapDataKey(base64.StdEncoding.EncodeToString(key), nil, nil)
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.False(t, got.wrappedByCurrent)

	for name, stored := range map[string]string{
		"not base64": "!!!",
		"short":      base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		_, err := unwrapDataKey(stored, nil, nil)
		require.Error(t, err, name)
	}
}

func TestUnwrapDataKeyWrappedKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	current := newTestKey(t)
	retired := newTestKey(t)

	// The current key opens it, and the caller learns it needs no re-wrap.
	got, err := unwrapDataKey(wrapKey(t, current, key), current, [][]byte{retired})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.True(t, got.wrappedByCurrent)

	// A retired key opens it, and the caller learns it must be re-wrapped.
	got, err = unwrapDataKey(wrapKey(t, retired, key), current, [][]byte{retired})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.False(t, got.wrappedByCurrent)

	// A wrapped key with nothing configured says so, rather than trying to
	// decode it as a bare key.
	_, err = unwrapDataKey(wrapKey(t, current, key), nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "none is configured")

	_, err = unwrapDataKey(wrapKey(t, newTestKey(t), key), current, [][]byte{retired})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configured key-encryption key")

	// A retired key can open the value, but that never makes it the current one.
	got, err = unwrapDataKey(wrapKey(t, retired, key), nil, [][]byte{retired})
	require.NoError(t, err)
	require.False(t, got.wrappedByCurrent)
}

// The wrapping round-trip the server performs when it re-wraps a bare data key
// must produce a value this code can open again.
func TestWrappedDataKeyRoundTrips(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)

	wrapped := wrapKey(t, kek, key)
	require.True(t, crypto.IsCiphertext(wrapped))
	require.False(t, strings.Contains(wrapped, base64.StdEncoding.EncodeToString(key)))

	got, err := unwrapDataKey(wrapped, kek, nil)
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.True(t, got.wrappedByCurrent)
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

// stubVerifier stands in for the startup check against stored credentials, so the
// key lifecycle can be driven without a database.
type stubVerifier struct {
	err error
	// verified records the ciphers it was asked about.
	verified []*crypto.Cipher
}

func (s *stubVerifier) verifyCipher(_ context.Context, cipher *crypto.Cipher) error {
	s.verified = append(s.verified, cipher)
	return s.err
}

func okVerifier() *stubVerifier {
	return &stubVerifier{}
}

// Without a key-encryption key the deployment is zero-config: the data key is
// generated into the setting table and stays there in the clear.
func TestLoadCredentialCipherKeepsTheKeyBareWithoutAKeyEncryptionKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	settings := newFakeSettings()
	settings.set(t, base64.StdEncoding.EncodeToString(key))

	cipher, err := loadCredentialCipher(t.Context(), settings, okVerifier(), nil, nil)
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

	cipher, err := loadCredentialCipher(t.Context(), settings, okVerifier(), kek, nil)
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(settings.stored()), "the stored key must be wrapped")
	require.NotContains(t, settings.stored(), base64.StdEncoding.EncodeToString(key))

	// A later startup with the same key-encryption key still resolves the same
	// data key, and it is not rewritten a second time.
	stored := settings.stored()
	again, err := loadCredentialCipher(t.Context(), settings, okVerifier(), kek, nil)
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

	cipher, err := loadCredentialCipher(t.Context(), settings, okVerifier(), current, [][]byte{retired})
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(settings.stored()))
	require.NotEqual(t, wrapKey(t, retired, key), settings.stored(), "the stored key must be re-wrapped under the current key")

	// The retired key is no longer needed, and the data key is unchanged.
	_, err = loadCredentialCipher(t.Context(), settings, okVerifier(), current, nil)
	require.NoError(t, err)
	encrypted, err := cipher.Encrypt("hunter2")
	require.NoError(t, err)
	decrypted, err := mustCipher(t, key).Decrypt(encrypted)
	require.NoError(t, err)
	require.Equal(t, "hunter2", decrypted)
}

// A retired key may open a wrapped data key, but it must never be the key one is
// wrapped under: otherwise clearing the current variable would silently promote
// the retired key back to being the one that protects the deployment.
func TestLoadCredentialCipherNeverWrapsUnderARetiredKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	retired := newTestKey(t)
	settings := newFakeSettings()
	settings.set(t, base64.StdEncoding.EncodeToString(key))

	_, err := loadCredentialCipher(t.Context(), settings, okVerifier(), nil, [][]byte{retired})
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(key), settings.stored(),
		"the data key must stay bare when no current key-encryption key is configured")
}

// Every way the key material can be wrong stops startup rather than leaving the
// store without a cipher.
func TestLoadCredentialCipherFailsClosed(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)

	for name, tc := range map[string]struct {
		settings *fakeSettings
		verifier credentialVerifier
		current  []byte
		previous [][]byte
		want     string
	}{
		"no setting row": {
			settings: newFakeSettings(),
			want:     "not configured",
		},
		"empty setting value": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: ""}},
			want:     "not configured",
		},
		"wrapped key with no key-encryption key": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: wrapKey(t, kek, key)}},
			want:     "none is configured",
		},
		"no key-encryption key opens it": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: wrapKey(t, kek, key)}},
			current:  newTestKey(t),
			want:     "no configured key-encryption key",
		},
		"stored value is not a key": {
			settings: &fakeSettings{values: map[storepb.SettingName]string{storepb.SettingName_ENCRYPTION_KEY: "not-a-key"}},
			want:     "neither a wrapped key nor base64",
		},
	} {
		verifier := tc.verifier
		if verifier == nil {
			verifier = okVerifier()
		}
		cipher, err := loadCredentialCipher(t.Context(), tc.settings, verifier, tc.current, tc.previous)
		require.Error(t, err, name)
		require.Contains(t, err.Error(), tc.want, name)
		require.Nil(t, cipher, name)
	}
}

// The startup check decides whether a resolved key belongs to this deployment.
// No stored credential means nothing to prove; one that opens means the key is the
// right one, even beside a damaged row — a single unreadable row is the running
// server's per-row problem, not a reason to refuse to start.
func TestCheckCipherAgainstCandidates(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	cipher := mustCipher(t, key)

	require.NoError(t, checkCipherAgainstCandidates(cipher, nil))

	good := wrapKey(t, key, []byte("a credential"))
	require.NoError(t, checkCipherAgainstCandidates(cipher, []string{good}))
	require.NoError(t, checkCipherAgainstCandidates(cipher, []string{"v1:not-really", good}),
		"a damaged row beside a readable one must not block startup")

	err := checkCipherAgainstCandidates(cipher, []string{wrapKey(t, newTestKey(t), []byte("another deployment"))})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not belong to this deployment")
}

// A key that opens nothing must not be re-wrapped either: wrapping it under the
// real key-encryption key would erase the evidence that it was substituted.
func TestLoadCredentialCipherVerifiesBeforeItWraps(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)
	settings := newFakeSettings()
	bare := base64.StdEncoding.EncodeToString(key)
	settings.set(t, bare)

	verifier := &stubVerifier{err: errors.New("the key opens nothing")}
	cipher, err := loadCredentialCipher(t.Context(), settings, verifier, kek, nil)
	require.Error(t, err)
	require.Nil(t, cipher)
	require.Len(t, verifier.verified, 1, "the key must be checked before it is stored")
	require.Equal(t, bare, settings.stored(), "a key that opened nothing must not be re-wrapped")
}
