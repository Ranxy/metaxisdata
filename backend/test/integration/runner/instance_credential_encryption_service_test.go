//go:build integration

package runner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// The credentials an administrator enters must reach the database only as
// ciphertext, and the server must still connect with them. This drives the real
// server and then reads the row it wrote.
func TestStoredInstanceCredentialsAreEncryptedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := t.Context()

	instanceID := "credential-encryption"
	instance, err := env.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)
	defer func() {
		_, _ = env.DeleteInstance(ctx, instance.GetName())
	}()

	adminCiphertext := storedCredentialCiphertext(t, env, instanceID)
	require.True(t, strings.HasPrefix(adminCiphertext, "v1:"),
		"the admin password must be stored encrypted, got %q", adminCiphertext)

	// The row holds no plaintext credential of its own: protojson omits an empty
	// field, so the only password in it is the ciphertext.
	var metadata string
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx,
		`SELECT metadata::text FROM instance WHERE resource_id = $1`, instanceID).Scan(&metadata))
	require.NotContains(t, metadata, `"password":`)

	// The server decrypts what it stored: this sync connects with the admin
	// credential, so a ciphertext the server could not open fails the test here.
	database := env.EnsureDatabaseVisible(ctx, t, instance.GetName(), "postgres")
	require.Equal(t, common.FormatDatabase(instanceID, "postgres"), database.GetName())

	// The observer store runs the same read path the server does, and gets the
	// credential back.
	stored, err := env.Store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	require.NoError(t, err)
	require.Equal(t, "postgres", stored.Metadata.GetDataSources()[0].GetPassword())

	// An update that does not name the credential keeps it: the store re-encrypts
	// the metadata it read back, so a masked patch that dropped the password would
	// leave an instance nothing could connect with.
	_, err = env.UpdateDataSource(ctx, instance.GetDataSources()[0].GetName(),
		&v1pb.DataSource{Database: "postgres"}, []string{"database"}, false)
	require.NoError(t, err)
	require.NotEqual(t, adminCiphertext, storedCredentialCiphertext(t, env, instanceID),
		"a re-encryption must use a fresh nonce")
	env.SyncDatabase(ctx, t, database.GetName())
	stored, err = env.Store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	require.NoError(t, err)
	require.Equal(t, "postgres", stored.Metadata.GetDataSources()[0].GetPassword())

	// The same password in a second instance produces a different ciphertext: a
	// fresh nonce per value is what stops a reused password from being detectable
	// across rows.
	otherID := "credential-encryption-other"
	other, err := env.CreatePostgresInstance(ctx, otherID)
	require.NoError(t, err)
	defer func() {
		_, _ = env.DeleteInstance(ctx, other.GetName())
	}()
	require.NotEqual(t, adminCiphertext, storedCredentialCiphertext(t, env, otherID))
}

func storedCredentialCiphertext(t *testing.T, env *integrationenv.ServiceEnv, instanceID string) string {
	t.Helper()

	var ciphertext string
	require.NoError(t, env.Store.GetDB().QueryRowContext(t.Context(),
		`SELECT metadata->'dataSources'->0->>'passwordCiphertext' FROM instance WHERE resource_id = $1`,
		instanceID).Scan(&ciphertext))
	return ciphertext
}
