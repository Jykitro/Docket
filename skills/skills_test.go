package skills

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
			if !strings.Contains(string(calls), "mcp add --scope user --transport http "+tc.name+" http://127.0.0.1:7777/mcp/code?agent=claude&route=docket --header Authorization: Bearer ${"+name+"}") {
				t.Error("Claude registration did not reference the endpoint credential")
			}
			if !strings.Contains(string(calls), "mcp add "+tc.name+" --url http://127.0.0.1:7777/mcp/code?agent=codex&route=docket --bearer-token-env-var "+name) {
				t.Error("Codex registration did not reference the endpoint credential")
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
