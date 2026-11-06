// Package skills embeds the agent skills that teach an assistant to run Docket as a personal planner:
// docket (the workflow, also sent to every MCP client as the server instructions), docket-reference and docket-setup.
package skills

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed docket*/SKILL.md
var all embed.FS

//go:embed install.sh
var installTemplate string

// InstallScript is a POSIX shell script that writes every embedded skill into a client's skills folder and
// registers the MCP server. The skills travel inside the script, so it needs no second download.
func InstallScript() string {
	files, _ := fs.Glob(all, "docket*/SKILL.md")
	var b strings.Builder
	for _, f := range files {
		name := path.Dir(f)
		body, _ := Get(name)
		// A quoted delimiter keeps $ and backticks in the skill text literal.
		b.WriteString("  mkdir -p \"$1/" + name + "\" && cat > \"$1/" + name + "/SKILL.md\" <<'DOCKET_SKILL_EOF'\n")
		b.WriteString(strings.TrimRight(body, "\n") + "\nDOCKET_SKILL_EOF\n")
	}
	return strings.Replace(installTemplate, "#SKILLS#\n", b.String(), 1)
}

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
