package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/cli/output"
	"github.com/Ranxy/metaxisdata/cli/skill"
)

// skillTestApp points the resolved invocation at a buffer. The command layer
// keeps its state in package variables, so these tests run in sequence.
func skillTestApp(t *testing.T) *bytes.Buffer {
	t.Helper()

	var stdout, stderr bytes.Buffer
	previous := current
	current = &app{out: output.NewWith(output.FormatJSON, &stdout, &stderr)}
	t.Cleanup(func() {
		current = previous
		skillFlags.dir = ""
	})
	return &stdout
}

func decodeEnvelope(t *testing.T, payload *bytes.Buffer) map[string]any {
	t.Helper()

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(payload.Bytes(), &envelope))
	return envelope
}

func TestSkillInstallWritesTheSkill(t *testing.T) {
	dir := t.TempDir()
	stdout := skillTestApp(t)
	skillFlags.dir = dir

	require.NoError(t, runSkillInstall(nil, nil))

	envelope := decodeEnvelope(t, stdout)
	path := filepath.Join(dir, skill.Name, skill.FileName())
	require.Equal(t, path, envelope["path"])
	require.Equal(t, skill.Name, envelope["name"])
	require.Equal(t, true, envelope["changed"])

	installed, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, skill.Content(), string(installed))
}

// Installing again after an upgrade has to be the way a machine picks up new
// wording, so an existing file is replaced rather than refused.
func TestSkillInstallReplacesAndReportsWhetherItChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, skill.Name, skill.FileName())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("an older skill"), 0o644))

	stdout := skillTestApp(t)
	skillFlags.dir = dir
	require.NoError(t, runSkillInstall(nil, nil))
	require.Equal(t, true, decodeEnvelope(t, stdout)["changed"])

	installed, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, skill.Content(), string(installed))

	// A second install has nothing to do.
	stdout.Reset()
	require.NoError(t, runSkillInstall(nil, nil))
	require.Equal(t, false, decodeEnvelope(t, stdout)["changed"])
}

func TestSkillShowPrintsTheContent(t *testing.T) {
	stdout := skillTestApp(t)

	require.NoError(t, newSkillShowCmd().RunE(nil, nil))

	envelope := decodeEnvelope(t, stdout)
	require.Equal(t, skill.Name, envelope["name"])
	require.Equal(t, skill.FileName(), envelope["fileName"])
	require.Equal(t, skill.Content(), envelope["content"])
	require.NotEmpty(t, envelope["description"])
}
