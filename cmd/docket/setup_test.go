package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetupCodeOperatorReadAndRotation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	var output bytes.Buffer
	if printSetupCode(dir, &output) == nil || output.Len() != 0 {
		t.Fatal("unexpected pending setup code")
	}
	previous := ""
	for range 2 {
		code, err := createSetupCode(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) < 20 || code == previous {
			t.Fatal("setup code did not rotate")
		}
		info, err := os.Stat(filepath.Join(dir, "setup-code"))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatal("setup file permissions", info.Mode())
		}
		output.Reset()
		if err := printSetupCode(dir, &output); err != nil || strings.TrimSpace(output.String()) != code {
			t.Fatal("operator retrieval failed", err)
		}
		previous = code
	}
}
