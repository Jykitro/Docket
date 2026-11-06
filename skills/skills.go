// Package skills embeds the agent skills that teach an assistant to run Docket as a personal planner:
// docket (the workflow, also sent to every MCP client as the server instructions), docket-reference and docket-setup.
package skills

import (
	"embed"
	"strings"
)

//go:embed docket*/SKILL.md
var all embed.FS

// Get returns the SKILL.md of the named skill. Only embedded names resolve; anything else, a path included, does not.
func Get(name string) (string, bool) {
	b, err := all.ReadFile(name + "/SKILL.md")
	return string(b), err == nil
}

// Instructions is the docket skill without its YAML frontmatter.
func Instructions() string {
	file, _ := Get("docket")
	_, body, ok := strings.Cut(strings.TrimPrefix(file, "---\n"), "\n---\n")
	if !ok {
		return file
	}
	return strings.TrimSpace(body)
}
