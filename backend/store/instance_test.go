package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Reading a page of instances resolves the secret once, so the per-row helper
// has to decrypt every credential field with the secret it was handed.
func TestUnObfuscateInstanceWithSecretDecryptsEveryCredential(t *testing.T) {
	t.Parallel()

	const secret = "test-secret-test-secret"
	plaintext := []string{"password", "ca", "cert", "key", "ssh-password", "ssh-private-key"}
	obfuscated := make([]string, 0, len(plaintext))
	for _, value := range plaintext {
		encrypted, err := common.Obfuscate(value, secret)
		require.NoError(t, err)
		obfuscated = append(obfuscated, encrypted)
	}

	instance := &storepb.Instance{DataSources: []*storepb.DataSource{{
		ObfuscatedPassword:      obfuscated[0],
		ObfuscatedSslCa:         obfuscated[1],
		ObfuscatedSslCert:       obfuscated[2],
		ObfuscatedSslKey:        obfuscated[3],
		ObfuscatedSshPassword:   obfuscated[4],
		ObfuscatedSshPrivateKey: obfuscated[5],
	}}}

	require.NoError(t, unObfuscateInstanceWithSecret(instance, secret))

	ds := instance.GetDataSources()[0]
	require.Equal(t, plaintext, []string{ds.Password, ds.SslCa, ds.SslCert, ds.SslKey, ds.SshPassword, ds.SshPrivateKey})

	// The wrong secret must not produce a credential.
	wrong := &storepb.Instance{DataSources: []*storepb.DataSource{{ObfuscatedPassword: obfuscated[0]}}}
	require.NoError(t, unObfuscateInstanceWithSecret(wrong, "another-secret-another-secret"))
	require.NotEqual(t, plaintext[0], wrong.GetDataSources()[0].Password)
}
