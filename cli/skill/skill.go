// Package skill carries the agent-facing skill that ships inside the mxd
// binary.
//
// The skill is embedded rather than read from disk because it is part of the
// CLI's own release: a user who installs the binary has no checkout to read it
// from, and `mxd skill install` is how it reaches the agent runtime on that
// machine.
package skill

import (
	_ "embed"
	"strings"
)

// Name is the skill's identifier. It is what a harness matches a skill request
// against and what the installed directory is called.
const Name = "mxd-cli"

// fileName is the file a harness reads inside a skill directory.
const fileName = "SKILL.md"

//go:embed SKILL.md
var content string

// Content is the skill document.
func Content() string {
	return content
}

// FileName is the file name the content is installed as.
func FileName() string {
	return fileName
}

// Description reads the one-line summary out of the frontmatter, which is what
// a harness shows when it lists the available skills.
func Description() string {
	for _, line := range strings.Split(content, "\n") {
		if rest, ok := strings.CutPrefix(line, "description:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
