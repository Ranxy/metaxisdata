package skill

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The skill ships inside the binary, so nothing else verifies it: a truncated
// embed or a renamed skill would only show up on a user's machine.
func TestContentIsAUsableSkill(t *testing.T) {
	t.Parallel()

	content := Content()
	require.True(t, strings.HasPrefix(content, "---\n"), "a skill starts with its frontmatter")
	require.Contains(t, content, "name: "+Name, "the frontmatter names the skill")
	require.NotEmpty(t, Description(), "the frontmatter describes when to use it")
	require.NotContains(t, Description(), "\n")

	// The instructions are the point; a stub would install nothing useful.
	require.Greater(t, len(strings.Fields(content)), 500, "the skill carries real instructions")

	// A user's machine has no checkout, so the skill must not send an agent to
	// repository paths that are not there.
	for _, forbidden := range []string{"cli/README.md", "plan/", "AGENTS.md"} {
		require.NotContains(t, content, forbidden, "the skill must not reference %q: it is installed outside the repository", forbidden)
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "SKILL.md", FileName())
}
