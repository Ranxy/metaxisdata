package store

import (
	"context"
	"encoding/base64"
	"log/slog"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// SetCipher installs the data key every stored credential is encrypted with.
// The server resolves it once at startup; a store without one refuses to read
// or write a credential rather than storing plaintext.
func (s *Store) SetCipher(c *crypto.Cipher) {
	s.cipherMu.Lock()
	defer s.cipherMu.Unlock()
	s.cipher = c
}

// credentialCipher returns the installed data key cipher.
func (s *Store) credentialCipher() (*crypto.Cipher, error) {
	s.cipherMu.RLock()
	defer s.cipherMu.RUnlock()
	if s.cipher == nil {
		return nil, errors.New("the credential cipher is not configured")
	}
	return s.cipher, nil
}

// settingStore is the slice of the store the credential key lifecycle needs. It
// exists so the decisions can be tested without a database: they are what
// decides whether the operator's key-encryption key actually protects anything.
type settingStore interface {
	GetSetting(ctx context.Context, name storepb.SettingName) (*SettingMessage, error)
	UpsertSetting(ctx context.Context, update *SetSettingMessage) (*SettingMessage, error)
}

// dataKey is the deployment credential key and how it was stored.
type dataKey struct {
	key []byte
	// wrappedByCurrent reports whether the stored value was opened with the
	// current key-encryption key. A bare key, or one opened by a retired key, has
	// to be re-wrapped before the current key is the only one that opens it.
	wrappedByCurrent bool
}

// ResolveCipher reads the deployment credential key from the setting table,
// unwraps it with the configured key-encryption keys, and installs the
// resulting cipher on the store.
//
// current is the operator's key-encryption key, or nil when none is configured:
// the deployment then keeps its data key in the database alone. previous holds
// retired keys, most recent first, and is only ever used to open a wrapped key —
// it never wraps one, so retiring a key cannot quietly promote it back. A data
// key found bare while a key-encryption key is configured, or opened by a retired
// one, is rewritten under the current key without re-encrypting a single
// credential.
func (s *Store) ResolveCipher(ctx context.Context, current []byte, previous [][]byte) error {
	cipher, err := loadCredentialCipher(ctx, s, current, previous)
	if err != nil {
		return err
	}
	s.SetCipher(cipher)
	return nil
}

// loadCredentialCipher resolves the credential key and returns the cipher that
// encrypts and decrypts credentials with it.
func loadCredentialCipher(ctx context.Context, settings settingStore, current []byte, previous [][]byte) (*crypto.Cipher, error) {
	setting, err := settings.GetSetting(ctx, storepb.SettingName_ENCRYPTION_KEY)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the credential encryption key")
	}
	if setting == nil || setting.Value == "" {
		return nil, errors.New("the credential encryption key is not configured")
	}

	key, err := unwrapDataKey(setting.Value, current, previous)
	if err != nil {
		return nil, err
	}
	if current == nil {
		slog.Info("the credential encryption key is stored unwrapped: a database read or backup recovers every stored credential",
			slog.String("environment", crypto.KeyEnvironment))
	}

	if current != nil && !key.wrappedByCurrent {
		currentCipher, err := crypto.NewCipher(current)
		if err != nil {
			return nil, errors.Wrap(err, "failed to build the key-encryption cipher")
		}
		wrapped, err := currentCipher.Encrypt(string(key.key))
		if err != nil {
			return nil, errors.Wrap(err, "failed to wrap the credential encryption key")
		}
		if _, err := settings.UpsertSetting(ctx, &SetSettingMessage{
			Name:  storepb.SettingName_ENCRYPTION_KEY,
			Value: wrapped,
		}); err != nil {
			return nil, errors.Wrap(err, "failed to store the wrapped credential encryption key")
		}
		slog.Info("credential encryption key wrapped under the configured key-encryption key",
			slog.Bool("retired_key", len(previous) > 0))
	}

	cipher, err := crypto.NewCipher(key.key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build the credential cipher")
	}
	return cipher, nil
}

// unwrapDataKey returns the credential key from its stored form: the bare base64
// of 32 random bytes, or a v1 ciphertext under current or one of previous.
func unwrapDataKey(stored string, current []byte, previous [][]byte) (*dataKey, error) {
	if !crypto.IsCiphertext(stored) {
		key, err := base64.StdEncoding.DecodeString(stored)
		if err != nil {
			return nil, errors.Wrap(err, "the credential encryption key is neither a wrapped key nor base64")
		}
		if len(key) != crypto.KeySize {
			return nil, errors.Errorf("the credential encryption key must be %d bytes, got %d", crypto.KeySize, len(key))
		}
		return &dataKey{key: key}, nil
	}

	if current == nil && len(previous) == 0 {
		return nil, errors.New("the credential encryption key is wrapped with a key-encryption key, but none is configured")
	}
	if current != nil {
		if key, err := openDataKey(stored, current); err == nil {
			return &dataKey{key: key, wrappedByCurrent: true}, nil
		}
	}
	for _, kek := range previous {
		if key, err := openDataKey(stored, kek); err == nil {
			return &dataKey{key: key}, nil
		}
	}
	return nil, errors.New("no configured key-encryption key decrypts the credential encryption key")
}

// openDataKey opens a wrapped credential key and checks its length.
func openDataKey(stored string, kek []byte) ([]byte, error) {
	kekCipher, err := crypto.NewCipher(kek)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build the key-encryption cipher")
	}
	plaintext, err := kekCipher.Decrypt(stored)
	if err != nil {
		return nil, err
	}
	key := []byte(plaintext)
	if len(key) != crypto.KeySize {
		return nil, errors.Errorf("the unwrapped credential encryption key must be %d bytes, got %d", crypto.KeySize, len(key))
	}
	return key, nil
}
