package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestVercelDeploysOnlyTheStaticLanding(t *testing.T) {
	var config struct {
		Services                                     map[string]json.RawMessage
		Rewrites                                     []json.RawMessage
		BuildCommand, OutputDirectory, IgnoreCommand string
	}
	data, err := os.ReadFile("../../vercel.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Services) != 0 || len(config.Rewrites) != 0 {
		t.Fatalf("expected a plain static deployment without services or rewrites: %s", data)
	}
	if config.BuildCommand != "make -C landing build" || config.OutputDirectory != "landing/dist" {
		t.Fatalf("unexpected landing build settings: %q %q", config.BuildCommand, config.OutputDirectory)
	}
	// Root-relative pathspecs keep the skip rule right whatever directory Vercel runs it from.
	if !strings.Contains(config.IgnoreCommand, "-- :/landing ") {
		t.Fatalf("landing changes must still trigger a build: %q", config.IgnoreCommand)
	}
	ignore, err := os.ReadFile("../../.vercelignore")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(ignore), "\n") {
		if strings.TrimSpace(line) == ".git/" || strings.TrimSpace(line) == ".git" {
			t.Fatal(".vercelignore must keep .git: without it the ignoreCommand cannot diff and every push builds")
		}
	}
}
