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
	refreshExpiry := time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)
	require.NoError(t, Save(path, &Credentials{
		Server:           "https://mx.example.com",
		Token:            "secret",
		RefreshToken:     "rotating-secret",
		User:             &User{Email: "dev@example.com", Name: "Dev"},
		TokenExpiresAt:   expiry,
		RefreshExpiresAt: refreshExpiry,
	}))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "https://mx.example.com", loaded.Server)
	require.Equal(t, "secret", loaded.Token)
	require.Equal(t, "rotating-secret", loaded.RefreshToken,
		"the session's rotating half must survive a round trip")
	require.Equal(t, "dev@example.com", loaded.User.Email)
	require.True(t, expiry.Equal(loaded.TokenExpiresAt))
	require.True(t, refreshExpiry.Equal(loaded.RefreshExpiresAt))

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
		Server:           "https://mx.example.com",
		Token:            "secret",
		RefreshToken:     "rotating-secret",
		User:             &User{Email: "dev@example.com"},
		TokenExpiresAt:   time.Now(),
		RefreshExpiresAt: time.Now().Add(time.Hour),
	}
	credentials.Clear()

	require.Equal(t, "https://mx.example.com", credentials.Server)
	require.Empty(t, credentials.Token)
	require.Empty(t, credentials.RefreshToken, "a cleared file must not keep a reusable session")
	require.Nil(t, credentials.User)
	require.True(t, credentials.TokenExpiresAt.IsZero())
	require.True(t, credentials.RefreshExpiresAt.IsZero())
}
