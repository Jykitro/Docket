// Package skills embeds the agent skills that teach an assistant to run Plandillo as a personal planner:
// plandillo (the workflow and MCP instructions), plandillo-reference and plandillo-setup.
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
	script := installTemplate
	for _, prefix := range []string{"plandillo", "docket"} {
		script = strings.Replace(script, "#"+prefix+"-skills#\n", installerSkills(prefix), 1)
	}
	return script
}

func installerSkills(prefix string) string {
	files, _ := fs.Glob(all, "plandillo*/SKILL.md")
	var b strings.Builder
	for _, f := range files {
		name := prefix + strings.TrimPrefix(path.Dir(f), "plandillo")
		body, _ := Get(name)
		// A quoted delimiter keeps $ and backticks in the skill text literal.
		b.WriteString("  mkdir -p \"$1/" + name + "\" && cat > \"$1/" + name + "/SKILL.md\" <<'DOCKET_SKILL_EOF'\n")
		b.WriteString(strings.TrimRight(body, "\n") + "\nDOCKET_SKILL_EOF\n")
	}
	return b.String()
}

// Get returns the SKILL.md of the named skill. Only embedded names resolve; anything else, a path included, does not.
func Get(name string) (string, bool) {
	canonical := name
	switch name {
	case "docket", "docket-reference", "docket-setup":
		canonical = "plandillo" + strings.TrimPrefix(name, "docket")
	}
	b, err := all.ReadFile(canonical + "/SKILL.md")
	if err != nil {
		return "", false
	}
	if name != canonical {
		// Rename the installed skills, keeping upstream repository URLs and plugin install commands intact.
		legacy := strings.Replace(string(b), "\nname: "+canonical+"\n", "\nname: "+name+"\n", 1)
		for _, skill := range []string{"plandillo", "plandillo-reference", "plandillo-setup"} {
			legacy = strings.ReplaceAll(legacy, "`"+skill+"`", "`docket"+strings.TrimPrefix(skill, "plandillo")+"`")
		}
		return legacy, true
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
