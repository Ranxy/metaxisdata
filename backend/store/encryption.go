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

// dataKey is the deployment credential key with the key that wrapped it.
type dataKey struct {
	key []byte
	// wrappedBy indexes the key-encryption key that opened the stored value, or
	// is -1 when the key is stored bare.
	wrappedBy int
}

// ResolveCipher reads the deployment credential key from the setting table,
// unwraps it with the configured key-encryption keys, and installs the
// resulting cipher on the store.
//
// keks holds the configured key-encryption keys, most recent first; an empty
// list means the deployment keeps its data key in the database alone. A key
// stored under a retired key-encryption key, or stored bare while one is now
// configured, is rewritten under the current one: that keeps the database from
// holding a key that is usable without the second secret, without re-encrypting
// a single credential.
func (s *Store) ResolveCipher(ctx context.Context, keks [][]byte) error {
	setting, err := s.GetSetting(ctx, storepb.SettingName_ENCRYPTION_KEY)
	if err != nil {
		return errors.Wrap(err, "failed to read the credential encryption key")
	}
	if setting == nil || setting.Value == "" {
		return errors.New("the credential encryption key is not configured")
	}

	key, err := unwrapDataKey(setting.Value, keks)
	if err != nil {
		return err
	}
	if len(keks) == 0 {
		slog.Info("the credential encryption key is stored unwrapped: a database read or backup recovers every stored credential",
			slog.String("environment", crypto.KeyEnvironment))
	}

	if len(keks) > 0 && key.wrappedBy != 0 {
		kekCipher, err := crypto.NewCipher(keks[0])
		if err != nil {
			return errors.Wrap(err, "failed to build the key-encryption cipher")
		}
		wrapped, err := kekCipher.Encrypt(string(key.key))
		if err != nil {
			return errors.Wrap(err, "failed to wrap the credential encryption key")
		}
		if _, err := s.UpsertSetting(ctx, &SetSettingMessage{
			Name:  storepb.SettingName_ENCRYPTION_KEY,
			Value: wrapped,
		}); err != nil {
			return errors.Wrap(err, "failed to store the wrapped credential encryption key")
		}
		slog.Info("credential encryption key wrapped under the configured key-encryption key",
			slog.Bool("rekeyed", key.wrappedBy > 0))
	}

	cipher, err := crypto.NewCipher(key.key)
	if err != nil {
		return errors.Wrap(err, "failed to build the credential cipher")
	}
	s.SetCipher(cipher)
	return nil
}

// unwrapDataKey returns the credential key from its stored form: the bare base64
// of 32 random bytes, or a v1 ciphertext under one of keks.
func unwrapDataKey(stored string, keks [][]byte) (*dataKey, error) {
	if !crypto.IsCiphertext(stored) {
		key, err := base64.StdEncoding.DecodeString(stored)
		if err != nil {
			return nil, errors.Wrap(err, "the credential encryption key is neither a wrapped key nor base64")
		}
		if len(key) != crypto.KeySize {
			return nil, errors.Errorf("the credential encryption key must be %d bytes, got %d", crypto.KeySize, len(key))
		}
		return &dataKey{key: key, wrappedBy: -1}, nil
	}

	if len(keks) == 0 {
		return nil, errors.New("the credential encryption key is wrapped with a key-encryption key, but none is configured")
	}
	for index, kek := range keks {
		kekCipher, err := crypto.NewCipher(kek)
		if err != nil {
			return nil, errors.Wrap(err, "failed to build the key-encryption cipher")
		}
		plaintext, err := kekCipher.Decrypt(stored)
		if err != nil {
			continue
		}
		key := []byte(plaintext)
		if len(key) != crypto.KeySize {
			return nil, errors.Errorf("the unwrapped credential encryption key must be %d bytes, got %d", crypto.KeySize, len(key))
		}
		return &dataKey{key: key, wrappedBy: index}, nil
	}
	return nil, errors.New("no configured key-encryption key decrypts the credential encryption key")
}
