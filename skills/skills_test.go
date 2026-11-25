package skills

import (
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"docket/internal/toolcatalog"
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

func TestLegacySkillKeepsDistributionAddresses(t *testing.T) {
	setup, _ := Get("docket-setup")
	for _, command := range []string{
		"npx skills add huntly-digital/plandillo --skill plandillo plandillo-reference plandillo-setup",
		"codex plugin add plandillo@huntly-digital",
		"claude plugin install plandillo@huntly-digital",
	} {
		if !strings.Contains(setup, command) {
			t.Errorf("missing upstream installation command %q in legacy skill", command)
		}
	}
	workflow, _ := Get("docket")
	for _, sibling := range []string{"docket-reference", "docket-setup"} {
		if !strings.Contains(workflow, "`"+sibling+"`") {
			t.Errorf("legacy workflow lost its reference to installed skill %s", sibling)
		}
	}
}

// Runs the generated installer against a fake `claude` and a throwaway HOME: every skill must land byte for byte
// (the heredoc must not expand $ or backticks) and the MCP server must be registered with the given address and token.
func TestInstallScriptWritesSkillsAndRegistersServer(t *testing.T) {
	for _, tc := range []struct {
		label, variable, name string
		upgrade               bool
	}{
		{"fresh", "PLANDILLO_URL", "plandillo", false},
		{"old command", "DOCKET_URL", "docket", false},
		{"existing installation", "PLANDILLO_URL", "docket", true},
	} {
		t.Run(tc.label, func(t *testing.T) {
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

			if tc.upgrade {
				for _, clientSkills := range []string{".claude/skills", ".agents/skills"} {
					dir := filepath.Join(home, clientSkills, "docket")
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("old skill"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			cmd := exec.Command("sh", script)
			cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, tc.variable + "=http://127.0.0.1:7777/mcp/code?route=docket", "DOCKET_TOKEN=dkt_test"}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("install.sh: %v\n%s", err, out)
			}

			for _, name := range []string{tc.name, tc.name + "-reference", tc.name + "-setup"} {
				want, _ := Get(name)
				got, err := os.ReadFile(filepath.Join(home, ".claude", "skills", name, "SKILL.md"))
				if err != nil || strings.TrimRight(string(got), "\n") != strings.TrimRight(want, "\n") {
					t.Errorf("skill %s not installed intact: %v", name, err)
				}
			}
			calls, _ := os.ReadFile(log)
			name := "DOCKET_MCP_" + hex.EncodeToString([]byte("http://127.0.0.1:7777/mcp/code?route=docket"))
			if strings.Contains(string(calls), "dkt_test") {
				t.Fatal("credential passed on a client command line")
			}
			if tc.variable == "PLANDILLO_URL" {
				endpoint := "http://127.0.0.1:7777/mcp/code?route=docket"
				if !strings.Contains(string(calls), "mcp add --scope user --transport http "+tc.name+" "+endpoint+"\n") || !strings.Contains(string(calls), "mcp add "+tc.name+" --url "+endpoint+"\n") || strings.Contains(string(calls), "Bearer") {
					t.Fatal("expected URL-only OAuth registration for both clients")
				}
				return
			}
			if !strings.Contains(string(calls), "mcp add --scope user --transport http "+tc.name+" http://127.0.0.1:7777/mcp/code?agent=claude&route=docket --header Authorization: Bearer ${"+name+"}") {
				t.Error("Claude registration did not reference the endpoint credential")
			}
			if !strings.Contains(string(calls), "mcp add "+tc.name+" --url http://127.0.0.1:7777/mcp/code?agent=codex&route=docket --bearer-token-env-var "+name) {
				t.Error("Codex registration did not reference the endpoint credential")
			}
		})
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

func TestInstallerChangesCredentialWhenEndpointChanges(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for _, endpoint := range []string{"https://one.example/mcp", "https://two.example/mcp"} {
		cmd := exec.Command("sh")
		cmd.Stdin = strings.NewReader(InstallScript())
		cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "DOCKET_URL=" + endpoint, "DOCKET_MCP_TOKEN=dkt_old"}
		if previous != "" {
			cmd.Env = append(cmd.Env, previous+"=dkt_previous")
		}
		out, err := cmd.CombinedOutput()
		name := "DOCKET_MCP_" + hex.EncodeToString([]byte(endpoint))
		if err != nil || !strings.Contains(string(out), "--bearer-token-env-var\n"+name+"\n") || !strings.Contains(string(out), "read -rs") {
			t.Fatal("expected endpoint-specific registration and hidden credential prompt")
		}
		if strings.Contains(string(out), "dkt_old") || strings.Contains(string(out), "dkt_previous") || previous != "" && strings.Contains(string(out), previous) {
			t.Fatal("installer reused the old credential or its name")
		}
		previous = name
	}
}

func TestInstallScriptFailsWithoutClient(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "DOCKET_URL=x", "DOCKET_TOKEN=y"}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "Neither claude nor codex") {
		t.Fatalf("want a failure naming the missing clients, got err=%v\n%s", err, out)
	}
}
