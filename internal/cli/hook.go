package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

const (
	startReminder = "Plandillo: for tracked work, follow the Plandillo workflow. Answer board questions with reads and respect a request to skip tracking."
	endChecklist  = "Plandillo end checklist: tick finished steps, add_note the result, move_tasks to done only if checks passed, " +
		"record findings with report_issue and list their IDs, check get_board doing and offer the next task by priority. " +
		"For read-only requests or untracked work, finish without planner writes."
)

// hook answers the SessionStart and Stop hooks of Claude Code and Codex the same way on every system. It reads only
// stdin and local files, never the board, and exits 0 so a hook never breaks the agent.
func (a *app) hook(args []string) error {
	if len(args) != 1 {
		return usageError("usage: plandillo-cli hook session-start|stop")
	}
	switch args[0] {
	case "session-start":
		// The hook's stdout becomes context for the session.
		fmt.Fprintln(a.stdout, startReminder)
		if n, _ := stale(skillReport(a.skillDirs(), nil)); n > 0 {
			fmt.Fprintf(a.stdout, "Plandillo skills outdated or missing: %d; suggest that the user runs plandillo-cli skills update.\n", n)
		}
		return nil
	case "stop":
		// Block the first stop of a turn with the checklist; the second stop (stop_hook_active) passes.
		var in struct {
			Active bool `json:"stop_hook_active"`
		}
		b, _ := io.ReadAll(io.LimitReader(a.stdin, 1<<20))
		if json.Unmarshal(b, &in) == nil && in.Active {
			return nil
		}
		out, _ := json.Marshal(map[string]string{"decision": "block", "reason": endChecklist})
		fmt.Fprintln(a.stdout, string(out))
		return nil
	}
	return usageError("unknown hook %q; use session-start or stop", args[0])
}
