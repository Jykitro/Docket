package skills

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"plandillo/internal/toolcatalog"
)

func TestReferenceCoversToolCatalogue(t *testing.T) {
	reference, _ := Get("plandillo-reference")
	for _, tool := range toolcatalog.MCPTools {
		if !strings.Contains(reference, "\n| `"+tool.Name+"` |") {
			t.Errorf("reference has no tool entry for %s", tool.Name)
		}
	}
}

func TestCodexPluginLoadsEmbeddedSkillDirectories(t *testing.T) {
	data, err := os.ReadFile(".codex-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Skills []string `json:"skills"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(all, "plandillo*/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, file := range files {
		want = append(want, "./"+strings.TrimSuffix(file, "/SKILL.md"))
	}
	slices.Sort(manifest.Skills)
	if !slices.Equal(manifest.Skills, want) {
		t.Errorf("Codex skill directories %v, want %v", manifest.Skills, want)
	}
}

func TestPluginsConnectToALocalBoard(t *testing.T) {
	read := func(name string, v any) {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var codex, claude struct {
		Version string `json:"version"`
		MCP     string `json:"mcpServers"`
	}
	read(".codex-plugin/plugin.json", &codex)
	read(".claude-plugin/plugin.json", &claude)
	if codex.Version != claude.Version {
		t.Errorf("plugin versions differ: Codex %s, Claude %s", codex.Version, claude.Version)
	}
	// Claude Code expands PLANDILLO_URL for a self-hosted board; Codex does not expand variables in a URL.
	for file, want := range map[string]string{
		".mcp.json": "${PLANDILLO_URL:-http://localhost:7777/mcp}",
		codex.MCP:   "http://localhost:7777/mcp",
	} {
		var config struct {
			Servers map[string]struct {
				URL string `json:"url"`
			} `json:"mcpServers"`
		}
		read(file, &config)
		if got := config.Servers["plandillo"].URL; got != want {
			t.Errorf("%s: plandillo url %q, want %q", file, got, want)
		}
	}
}

// Runs the generated installer against fake clients and a throwaway HOME: every skill must land byte for byte
// (the heredoc must not expand $ or backticks) and the MCP server must be registered by URL for browser sign-in.
func TestInstallScriptWritesSkillsAndRegistersServer(t *testing.T) {
	dir := t.TempDir()
	bin, home, log := filepath.Join(dir, "bin"), filepath.Join(dir, "home"), filepath.Join(dir, "calls")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\necho \"$@\" >> " + log + "\n"
	for _, client := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, client), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	endpoint := "http://127.0.0.1:7777/mcp/code?route=x"
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "PLANDILLO_URL=" + endpoint}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	files, _ := fs.Glob(all, "plandillo*/SKILL.md")
	for _, f := range files {
		name := filepath.Dir(f)
		want, _ := Get(name)
		for _, clientSkills := range []string{".claude/skills", ".agents/skills"} {
			got, err := os.ReadFile(filepath.Join(home, clientSkills, name, "SKILL.md"))
			if err != nil || strings.TrimRight(string(got), "\n") != strings.TrimRight(want, "\n") {
				t.Errorf("skill %s not installed intact in %s: %v", name, clientSkills, err)
			}
		}
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "mcp add --scope user --transport http plandillo "+endpoint+"\n") ||
		!strings.Contains(string(calls), "mcp add plandillo --url "+endpoint+"\n") || strings.Contains(string(calls), "Bearer") {
		t.Fatalf("expected URL-only registration for both clients:\n%s", calls)
	}
}

func TestInstallerOnlyUpdatesSelectedClient(t *testing.T) {
	for _, selected := range []string{"codex", "claude-code"} {
		t.Run(selected, func(t *testing.T) {
			dir := t.TempDir()
			for _, cli := range []string{"codex", "claude"} {
				if err := os.WriteFile(filepath.Join(dir, cli), []byte("#!/bin/sh\necho "+cli+" \"$@\"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("sh")
			cmd.Stdin = strings.NewReader(InstallScript())
			cmd.Env = []string{"HOME=" + dir, "PATH=" + dir + ":/usr/bin:/bin", "PLANDILLO_CLIENT=" + selected, "PLANDILLO_URL=https://board.example/mcp/code"}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatal(err, string(out))
			}
			want, other := "codex", "claude"
			target, untouched := ".agents", ".claude"
			if selected == "claude-code" {
				want, other = other, want
				target, untouched = untouched, target
			}
			if !strings.Contains(string(out), want+" mcp add") || !strings.Contains(string(out), "https://board.example/mcp/code") || strings.Contains(string(out), other+" mcp ") {
				t.Fatalf("unexpected CLI calls: %s", out)
			}
			if _, err := os.Stat(filepath.Join(dir, target, "skills", "plandillo", "SKILL.md")); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, untouched)); !os.IsNotExist(err) {
				t.Fatal("unselected client was modified")
			}
		})
	}
}

func TestInstallScriptFailsWithoutClient(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "PLANDILLO_URL=x"}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "Neither claude nor codex") {
		t.Fatalf("want a failure naming the missing clients, got err=%v\n%s", err, out)
	}
}
