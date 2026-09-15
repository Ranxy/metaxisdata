package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/cli/env"
)

func TestPathHonoursTheOverride(t *testing.T) {
	t.Setenv(env.ConfigEnv, "/tmp/agent-a/mxd.json")

	path, err := Path()
	require.NoError(t, err)
	require.Equal(t, "/tmp/agent-a/mxd.json", path)
}

func TestLoadOfAMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()

	credentials, err := Load(filepath.Join(t.TempDir(), "config.json"))
	require.NoError(t, err)
	require.Empty(t, credentials.Server)
	require.Empty(t, credentials.Token)
}

func TestSaveWritesOwnerOnlyAndRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "config.json")
	expiry := time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC)
	require.NoError(t, Save(path, &Credentials{
		Server:         "https://mx.example.com",
		Token:          "secret",
		User:           &User{Email: "dev@example.com", Name: "Dev"},
		TokenExpiresAt: expiry,
	}))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "https://mx.example.com", loaded.Server)
	require.Equal(t, "secret", loaded.Token)
	require.Equal(t, "dev@example.com", loaded.User.Email)
	require.True(t, expiry.Equal(loaded.TokenExpiresAt))

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the token must not be readable by anyone else")
	}
}

func TestSaveRejectsAnInvalidFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := Load(path)
	require.ErrorContains(t, err, "not a valid credentials file")
}

// Signing out keeps the address: it does not change, and asking for it again
// would be pointless.
func TestClearKeepsTheServer(t *testing.T) {
	t.Parallel()

	credentials := &Credentials{
		Server:         "https://mx.example.com",
		Token:          "secret",
		User:           &User{Email: "dev@example.com"},
		TokenExpiresAt: time.Now(),
	}
	credentials.Clear()

	require.Equal(t, "https://mx.example.com", credentials.Server)
	require.Empty(t, credentials.Token)
	require.Nil(t, credentials.User)
	require.True(t, credentials.TokenExpiresAt.IsZero())
}
