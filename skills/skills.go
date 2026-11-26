// Package skills embeds the agent skills that teach an assistant to run Plandillo as a personal planner:
// plandillo (the workflow and MCP instructions), plandillo-reference, plandillo-setup and plandillo-infographics.
package skills

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed plandillo*/SKILL.md
var all embed.FS

//go:embed install.sh
var installTemplate string

// InstallScript is a POSIX shell script that writes every embedded skill into a client's skills folder and
// registers the MCP server. The skills travel inside the script, so it needs no second download.
func InstallScript() string {
	var b strings.Builder
	for _, name := range Names() {
		body, _ := Get(name)
		// A quoted delimiter keeps $ and backticks in the skill text literal.
		b.WriteString("  mkdir -p \"$1/" + name + "\" && cat > \"$1/" + name + "/SKILL.md\" <<'PLANDILLO_SKILL_EOF'\n")
		b.WriteString(strings.TrimRight(body, "\n") + "\nPLANDILLO_SKILL_EOF\n")
	}
	return strings.Replace(installTemplate, "#plandillo-skills#\n", b.String(), 1)
}

// Names lists the embedded skills, such as plandillo and plandillo-reference.
func Names() []string {
	files, _ := fs.Glob(all, "plandillo*/SKILL.md")
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = path.Dir(f)
	}
	return names
}

// Get returns the SKILL.md of the named skill. Only embedded names resolve; anything else, a path included, does not.
func Get(name string) (string, bool) {
	b, err := all.ReadFile(name + "/SKILL.md")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Instructions is the Plandillo skill without its YAML frontmatter.
func Instructions() string {
	file, _ := Get("plandillo")
	_, body, ok := strings.Cut(strings.TrimPrefix(file, "---\n"), "\n---\n")
	if !ok {
		return file
	}
	return strings.TrimSpace(body)
}
