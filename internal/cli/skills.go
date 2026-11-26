package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"plandillo/skills"
)

// skillState is one embedded skill in one skills folder: current, outdated or missing.
type skillState struct {
	Dir   string `json:"dir"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// agentSkillDirs are where each agent reads personal skills, relative to the home folder.
var agentSkillDirs = map[string]string{"claude-code": filepath.Join(".claude", "skills"), "codex": filepath.Join(".agents", "skills")}

func (a *app) home() string {
	if h := a.getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// skillDirs are the folders Claude Code and Codex read skills from: personal ones and the current project's.
func (a *app) skillDirs() []string {
	var dirs []string
	home := a.home()
	if home != "" {
		dirs = append(dirs, filepath.Join(home, agentSkillDirs["claude-code"]), filepath.Join(home, agentSkillDirs["codex"]))
	}
	if wd, err := os.Getwd(); err == nil && wd != home {
		dirs = append(dirs, filepath.Join(wd, agentSkillDirs["claude-code"]), filepath.Join(wd, agentSkillDirs["codex"]))
	}
	return dirs
}

// skillReport compares the skills built into this CLI with each folder. A folder counts when it already holds one
// of them, or when it was named on the command line (always).
func skillReport(dirs []string, always map[string]bool) []skillState {
	var out []skillState
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		var states []skillState
		found := always[dir]
		for _, name := range skills.Names() {
			want, _ := skills.Get(name)
			got, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md"))
			state := "current"
			switch {
			case err != nil:
				state = "missing"
			case strings.TrimRight(string(got), "\n") != strings.TrimRight(want, "\n"):
				state, found = "outdated", true
			default:
				found = true
			}
			states = append(states, skillState{dir, name, state})
		}
		if found {
			out = append(out, states...)
		}
	}
	return out
}

func stale(report []skillState) (n int, dirs []string) {
	seen := map[string]bool{}
	for _, s := range report {
		if s.State == "current" {
			continue
		}
		n++
		if !seen[s.Dir] {
			seen[s.Dir] = true
			dirs = append(dirs, s.Dir)
		}
	}
	return n, dirs
}

// skills reports (status, the default) or rewrites (update) the Plandillo skills of Claude Code and Codex.
func (a *app) skills(args []string) error {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	agent := fs.String("agent", "", "")
	dir := fs.String("dir", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || (sub != "status" && sub != "update") {
		return usageError("usage: plandillo-cli skills [status|update] [--agent claude-code|codex] [--dir DIR]")
	}
	always := map[string]bool{}
	dirs := a.skillDirs()
	for _, name := range strings.Split(*agent, ",") {
		if name == "" {
			continue
		}
		rel, ok := agentSkillDirs[name]
		if !ok {
			return usageError("--agent must be claude-code or codex, got %q", name)
		}
		d := filepath.Join(a.home(), rel)
		always[d] = true
		dirs = append(dirs, d)
	}
	if *dir != "" {
		d, err := filepath.Abs(*dir)
		if err != nil {
			return err
		}
		always[d] = true
		dirs = append(dirs, d)
	}
	report := skillReport(dirs, always)

	if sub == "status" {
		n, _ := stale(report)
		b, _ := json.Marshal(map[string]any{"version": Version, "skills": report, "to_update": n})
		if n > 0 {
			fmt.Fprintf(a.stderr, "Plandillo skills outdated or missing: %d; run plandillo-cli skills update\n", n)
		}
		if len(report) == 0 {
			fmt.Fprintln(a.stderr, "No Plandillo skills found; install them with plandillo-cli skills update --agent claude-code (or codex)")
		}
		return a.print(string(b), false)
	}

	if len(report) == 0 {
		return usageError("no Plandillo skills found in ~/.claude/skills, ~/.agents/skills or this project; " +
			"choose where to install them: plandillo-cli skills update --agent claude-code (or codex, or --dir DIR)")
	}
	updated := []string{}
	for _, s := range report {
		if s.State == "current" {
			continue
		}
		body, _ := skills.Get(s.Name)
		path := filepath.Join(s.Dir, s.Name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeAtomic(path, []byte(body), 0o644); err != nil {
			return err
		}
		updated = append(updated, path)
	}
	if len(updated) > 0 {
		fmt.Fprintln(a.stderr, "Skills updated; start a new agent session to load them.")
	}
	b, _ := json.Marshal(map[string]any{"version": Version, "updated": updated})
	return a.print(string(b), false)
}

// skillsLine is the skills row of plandillo-cli status; it reads only local files.
func (a *app) skillsLine() string {
	report := skillReport(a.skillDirs(), nil)
	if len(report) == 0 {
		return "not installed (plandillo-cli skills update --agent claude-code or codex)"
	}
	n, dirs := stale(report)
	if n == 0 {
		return "up to date"
	}
	return fmt.Sprintf("%d to update in %s (plandillo-cli skills update)", n, strings.Join(dirs, ", "))
}

// writeAtomic replaces path through a temporary file in the same folder, so a reader never sees half a file.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".plandillo-*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), perm)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return errors.Join(fmt.Errorf("write %s", path), err)
	}
	return nil
}
