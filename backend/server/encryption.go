package server

import (
	"context"
	"os"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
)

// resolveCredentialCipher installs the data key every stored credential is
// encrypted with. The key is generated on first startup and kept in the setting
// table; when the operator supplies a key-encryption key, the data key is stored
// wrapped under it, and a key found under a retired one is re-wrapped under the
// current one. Without a configured key-encryption key the deployment still works
// and still keeps plaintext out of the database, but database read access is
// enough to decrypt every credential — the same exposure obfuscation had.
func (s *Server) resolveCredentialCipher(ctx context.Context) error {
	current, err := crypto.ParseKeys(os.Getenv(crypto.KeyEnvironment))
	if err != nil {
		return errors.Wrapf(err, "%s is not a usable key-encryption key", crypto.KeyEnvironment)
	}
	previous, err := crypto.ParseKeys(os.Getenv(crypto.PreviousKeyEnvironment))
	if err != nil {
		return errors.Wrapf(err, "%s is not a usable key-encryption key", crypto.PreviousKeyEnvironment)
	}
	if err := s.store.ResolveCipher(ctx, append(current, previous...)); err != nil {
		return errors.Wrap(err, "failed to resolve the credential encryption key")
	}
	return nil
}
