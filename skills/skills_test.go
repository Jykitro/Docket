package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs the generated installer against a fake `claude` and a throwaway HOME: every skill must land byte for byte
// (the heredoc must not expand $ or backticks) and the MCP server must be registered with the given address and token.
func TestInstallScriptWritesSkillsAndRegistersServer(t *testing.T) {
	dir := t.TempDir()
	bin, home, log := filepath.Join(dir, "bin"), filepath.Join(dir, "home"), filepath.Join(dir, "calls")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\necho \"$@\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(InstallScript()), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", script)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "DOCKET_URL=http://127.0.0.1:7777/mcp", "DOCKET_TOKEN=dkt_test"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}

	for _, name := range []string{"docket", "docket-reference", "docket-setup"} {
		want, _ := Get(name)
		got, err := os.ReadFile(filepath.Join(home, ".claude", "skills", name, "SKILL.md"))
		if err != nil || strings.TrimRight(string(got), "\n") != strings.TrimRight(want, "\n") {
			t.Errorf("skill %s not installed intact: %v", name, err)
		}
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "mcp add --scope user --transport http docket http://127.0.0.1:7777/mcp --header Authorization: Bearer dkt_test") {
		t.Errorf("claude mcp add not called as expected:\n%s", calls)
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
