// Package skills embeds the agent skill that teaches an assistant to run Docket as a personal planner.
// The same text is installed as a SKILL.md and sent to every MCP client as the server instructions.
package skills

import (
	_ "embed"
	"strings"
)

//go:embed docket/SKILL.md
var File string

// Instructions is the skill without its YAML frontmatter.
func Instructions() string {
	_, body, ok := strings.Cut(strings.TrimPrefix(File, "---\n"), "\n---\n")
	if !ok {
		return File
	}
	return strings.TrimSpace(body)
}
