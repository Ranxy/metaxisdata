package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Ranxy/metaxisdata/cli/client"
	"github.com/Ranxy/metaxisdata/cli/skill"
)

var skillFlags struct {
	dir string
}

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the agent skill that teaches an agent to use mxd",
		Long: `Install the agent skill that teaches an agent to use mxd.

The skill ships inside this binary, so a machine that has mxd does not need a
checkout to give its agent the same instructions. It is written to the skill
directory of the agent runtime, where the harness discovers it.`,
	}
	cmd.AddCommand(newSkillInstallCmd(), newSkillShowCmd())
	return cmd
}

// defaultSkillsDir is where the agent runtime reads skills from. It matches the
// convention the harness already uses for the skills shipped with it.
const defaultSkillsDir = ".agents/skills"

func newSkillInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the skill into the agent's skill directory",
		Long: `Write the skill into the agent's skill directory.

The default location is ~/` + defaultSkillsDir + `/` + skill.Name + `. Use --dir to
target something else, such as ~/.claude/skills, or a project's own
.agents/skills directory when the agent runs per project.

An existing file is replaced: the skill belongs to the binary, so installing it
again after an upgrade is how a machine picks up the new wording.`,
		Args: cobra.NoArgs,
		RunE: runSkillInstall,
	}
	cmd.Flags().StringVar(&skillFlags.dir, "dir", "", "skill directory to install into (default ~/"+filepath.ToSlash(defaultSkillsDir)+")")
	return cmd
}

func runSkillInstall(_ *cobra.Command, _ []string) error {
	dir := skillFlags.dir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return client.Usage("failed to locate the home directory: %v", err).
				WithHint("pass --dir to choose where the skill goes")
		}
		dir = filepath.Join(home, filepath.FromSlash(defaultSkillsDir))
	}

	skillDir := filepath.Join(dir, skill.Name)
	path := filepath.Join(skillDir, skill.FileName())

	previous, err := os.ReadFile(path)
	changed := err != nil || string(previous) != skill.Content()

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return client.Usage("failed to create %s: %v", skillDir, err)
	}
	if err := os.WriteFile(path, []byte(skill.Content()), 0o644); err != nil {
		return client.Usage("failed to write %s: %v", path, err)
	}

	return current.out.JSON(map[string]any{
		"name":    skill.Name,
		"path":    path,
		"changed": changed,
	})
}

func newSkillShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the skill this binary carries",
		Long: `Print the skill this binary carries.

The content is returned as JSON like every other result, so a harness that
cannot read a file can still capture it.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return current.out.JSON(map[string]any{
				"name":        skill.Name,
				"fileName":    skill.FileName(),
				"description": skill.Description(),
				"content":     skill.Content(),
			})
		},
	}
}
