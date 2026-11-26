package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// release writes a fake GitHub release into dir: the archive holds a plandillo-cli script that logs its arguments.
func release(t *testing.T, dir, file, log string, goodSum bool) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\necho \"plandillo-cli $*\" >> " + log + "\n"
	for name, text := range map[string]string{"LICENSE": "MIT\n", "plandillo-cli": body} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(text))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(text))
	}
	tw.Close()
	gz.Close()
	f.Close()
	data, _ := os.ReadFile(filepath.Join(dir, file))
	sum := sha256.Sum256(data)
	if !goodSum {
		sum[0] ^= 0xff
	}
	os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(hex.EncodeToString(sum[:])+"  "+file+"\n"), 0o644)
}

// Runs install.sh with a fake curl that serves the fake release: it must download from the latest release, check the
// checksum, install into PLANDILLO_BIN_DIR and sign in to the board; a wrong checksum installs nothing.
func TestInstallShInstallsAndSignsIn(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("install.sh is for macOS and Linux")
	}
	file := "plandillo-cli_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	for _, good := range []bool{true, false} {
		dir := t.TempDir()
		fakes, fixtures, bin, log := filepath.Join(dir, "fakes"), filepath.Join(dir, "release"), filepath.Join(dir, "bin"), filepath.Join(dir, "log")
		os.MkdirAll(fakes, 0o755)
		os.MkdirAll(fixtures, 0o755)
		release(t, fixtures, file, log, good)
		curl := "#!/bin/sh\nout=; url=\nwhile [ $# -gt 0 ]; do case $1 in -o) out=$2; shift 2;; -*) shift;; *) url=$1; shift;; esac; done\n" +
			"echo \"curl $url\" >> " + log + "\ncp \"" + fixtures + "/${url##*/}\" \"$out\"\n"
		os.WriteFile(filepath.Join(fakes, "curl"), []byte(curl), 0o755)
		script := filepath.Join(dir, "install.sh")
		os.WriteFile(script, []byte(Sh), 0o755)

		cmd := exec.Command("sh", script)
		cmd.Env = []string{"PATH=" + fakes + ":/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "PLANDILLO_BIN_DIR=" + bin, "PLANDILLO_URL=https://board.example"}
		out, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(log)
		_, statErr := os.Stat(filepath.Join(bin, "plandillo-cli"))
		if !good {
			if err == nil || statErr == nil || !strings.Contains(string(out), "checksum") {
				t.Fatalf("a wrong checksum must stop the install: err=%v installed=%v\n%s", err, statErr == nil, out)
			}
			continue
		}
		if err != nil || statErr != nil {
			t.Fatalf("install.sh: %v %v\n%s", err, statErr, out)
		}
		for _, want := range []string{
			"curl https://github.com/huntly-digital/plandillo/releases/latest/download/" + file,
			"curl https://github.com/huntly-digital/plandillo/releases/latest/download/checksums.txt",
			"plandillo-cli version",
			"plandillo-cli login https://board.example",
		} {
			if !strings.Contains(string(calls), want+"\n") {
				t.Errorf("missing %q in:\n%s", want, calls)
			}
		}
		if !strings.Contains(string(out), "Add "+bin+" to PATH") {
			t.Errorf("no PATH hint for a folder outside PATH:\n%s", out)
		}
	}
}

// Runs install.ps1 under pwsh (on the CI runner; skipped where PowerShell is missing). Invoke-WebRequest and Copy-Item
// are shadowed so the fake release is served from disk and the fake plandillo-cli.exe stays executable off Windows.
func TestInstallPs1InstallsAndSignsIn(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not installed")
	}
	dir := t.TempDir()
	fixtures, bin, log := filepath.Join(dir, "release"), filepath.Join(dir, "bin"), filepath.Join(dir, "log")
	os.MkdirAll(fixtures, 0o755)
	file := "plandillo-cli_windows_amd64.zip"
	f, _ := os.Create(filepath.Join(fixtures, file))
	zw := zip.NewWriter(f)
	w, _ := zw.Create("plandillo-cli.exe")
	w.Write([]byte("#!/bin/sh\necho \"plandillo-cli $*\" >> " + log + "\n"))
	zw.Close()
	f.Close()
	data, _ := os.ReadFile(filepath.Join(fixtures, file))
	sum := sha256.Sum256(data)
	os.WriteFile(filepath.Join(fixtures, "checksums.txt"), []byte(hex.EncodeToString(sum[:])+"  "+file+"\n"), 0o644)

	prelude := `function Invoke-WebRequest { param([switch]$UseBasicParsing, $Uri, $OutFile)
  Add-Content $env:LOG "get $Uri"
  Microsoft.PowerShell.Management\Copy-Item (Join-Path $env:FIXTURES (Split-Path $Uri -Leaf)) $OutFile }
function Copy-Item { param($Path, $Destination, [switch]$Force)
  Microsoft.PowerShell.Management\Copy-Item $Path $Destination -Force
  chmod +x $Destination }
`
	script := filepath.Join(dir, "install.ps1")
	os.WriteFile(script, []byte(prelude+Ps1), 0o644)
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", script)
	cmd.Env = append(os.Environ(), "PLANDILLO_BIN_DIR="+bin, "PLANDILLO_URL=https://board.example", "FIXTURES="+fixtures, "LOG="+log, "PROCESSOR_ARCHITECTURE=AMD64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install.ps1: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"get https://github.com/huntly-digital/plandillo/releases/latest/download/" + file,
		"plandillo-cli version",
		"plandillo-cli login https://board.example",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("missing %q in:\n%s", want, calls)
		}
	}
}
