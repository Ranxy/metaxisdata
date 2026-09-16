// Package config reads and writes the CLI's credentials file.
//
// The file holds a server address and a token, and nothing else. Analysis
// scopes are not stored: they come from the process environment (see the env
// package), because several agents can share a machine while serving different
// projects.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/cli/env"
)

// Credentials is the on-disk shape. The token is written in clear text: the CLI
// treats a credential file as equivalent to a seven day session, which matches
// how the server already stores instance and provider secrets (obfuscated, not
// encrypted) in this self-hosted deployment.
type Credentials struct {
	Server         string    `json:"server"`
	Token          string    `json:"token"`
	User           *User     `json:"user,omitempty"`
	TokenExpiresAt time.Time `json:"tokenExpiresAt,omitzero"`
}

// User is the signed-in identity, kept for `auth status` and for the
// confirmation the CLI prints after a login.
type User struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Path returns the credentials file the current invocation reads and writes.
// METAXISDATA_CONFIG points at a specific file, which is how one machine keeps
// several identities apart; otherwise the XDG configuration directory is used.
func Path() (string, error) {
	if override := os.Getenv(env.ConfigEnv); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to locate the user configuration directory")
	}
	return filepath.Join(dir, "metaxisdata", "config.json"), nil
}

// Load reads the credentials file. A missing file is not an error: it simply
// means nothing has been configured yet, and the caller decides what to do.
func Load(path string) (*Credentials, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Credentials{}, nil
		}
		return nil, errors.Wrapf(err, "failed to read %s", path)
	}

	var credentials Credentials
	if err := json.Unmarshal(content, &credentials); err != nil {
		return nil, errors.Errorf("%s is not a valid credentials file: %v", path, err)
	}
	return &credentials, nil
}

// Save writes the file with owner-only permissions, creating its directory if
// needed. It writes a temporary file and renames it, so a crash cannot leave a
// half-written credentials file behind.
func Save(path string, credentials *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.Wrapf(err, "failed to create the directory for %s", path)
	}

	content, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to encode the credentials")
	}
	content = append(content, '\n')

	// The temporary file is created with the final mode so the token is never
	// briefly world readable.
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return errors.Wrapf(err, "failed to create a temporary file next to %s", path)
	}
	tempName := temp.Name()
	defer func() {
		_ = os.Remove(tempName)
	}()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return errors.Wrap(err, "failed to restrict the credentials file permissions")
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return errors.Wrap(err, "failed to write the credentials")
	}
	if err := temp.Close(); err != nil {
		return errors.Wrap(err, "failed to close the credentials file")
	}
	if err := os.Rename(tempName, path); err != nil {
		return errors.Wrapf(err, "failed to replace %s", path)
	}
	return nil
}

// Clear removes the token but keeps the server address: signing out is not the
// same as pointing at another server, and the address does not change.
func (c *Credentials) Clear() {
	c.Token = ""
	c.User = nil
	c.TokenExpiresAt = time.Time{}
}
