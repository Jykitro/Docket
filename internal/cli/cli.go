// Package cli is plandillo-cli: it calls the board's MCP tools from a shell, so an agent or a script
// can plan and track work without an MCP client. Tool commands come from the server's tools/list.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Version is set at release time: -ldflags "-X plandillo/internal/cli.Version=v1.2.3".
var Version = "dev"

const (
	exitOK      = 0
	exitTool    = 1 // the tool reported an error, or anything unexpected
	exitUsage   = 2
	exitAuth    = 3 // HTTP 401/403 or an expired sign-in
	exitNetwork = 4 // the board did not answer
)

const usage = `plandillo-cli calls Plandillo board tools from a shell.

Usage:
  plandillo-cli [--url URL] [--agent NAME] [--pretty] COMMAND [ARGS]

Commands:
  login [URL]            sign in in the browser (also: --with-token reads a workspace token from stdin;
                         --code also authorizes Code Mode)
  logout                 forget the saved sign-in
  status                 board address, sign-in and available tools
  tools [--json]         list the tools; --json adds their argument schemas
  TOOL [JSON] [--ARG V]  call a tool: get_today, get_task --id PLD-12, create_tasks '{"tasks":[...]}'
                         JSON may be @file.json or - for stdin; --ARG sets one top-level argument
  code JS                run Code Mode JavaScript (also @file.js or -): 'return await codemode.get_today({})'
  version

Output is the tool result as JSON on stdout; errors go to stderr as "code: message".
Exit codes: 0 ok, 1 tool error, 2 usage, 3 not signed in or not allowed, 4 board unreachable.
Environment: PLANDILLO_URL, PLANDILLO_TOKEN (workspace token, overrides the saved sign-in),
PLANDILLO_AGENT (name in task history), PLANDILLO_CLI_CONFIG (config file path).
`

type app struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
	url, agent     string
	pretty         bool
}

// exitError carries the exit code and the message printed on stderr.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func usageError(format string, args ...any) error {
	return &exitError{exitUsage, fmt.Sprintf(format, args...)}
}

// Run executes one command and returns the process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr, getenv: getenv}
	fs := flag.NewFlagSet("plandillo-cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.url, "url", "", "")
	fs.StringVar(&a.agent, "agent", getenv("PLANDILLO_AGENT"), "")
	fs.BoolVar(&a.pretty, "pretty", false, "")
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return exitOK
	case err != nil:
		return a.report(usageError("%v\n\n%s", err, usage))
	case fs.NArg() == 0:
		return a.report(usageError("%s", usage))
	}
	return a.report(a.run(ctx, fs.Arg(0), fs.Args()[1:]))
}

func (a *app) run(ctx context.Context, cmd string, args []string) error {
	switch cmd {
	case "help":
		fmt.Fprint(a.stdout, usage)
		return nil
	case "version":
		fmt.Fprintln(a.stdout, Version)
		return nil
	case "login":
		return a.login(ctx, args)
	case "logout":
		return a.logout()
	case "status":
		return a.status(ctx)
	case "tools":
		return a.tools(ctx, args)
	case "code":
		return a.code(ctx, args)
	}
	return a.call(ctx, cmd, args)
}

func (a *app) report(err error) int {
	if err == nil {
		return exitOK
	}
	var e *exitError
	if !errors.As(err, &e) {
		e = &exitError{exitTool, err.Error()}
	}
	if e.msg != "" {
		fmt.Fprintln(a.stderr, strings.TrimRight(e.msg, "\n"))
	}
	return e.code
}

// print writes a tool result: its text (compact JSON from Plandillo) on stdout, or the error on stderr.
func (a *app) print(text string, isError bool) error {
	if isError {
		return &exitError{exitTool, text}
	}
	if a.pretty {
		var v any
		if json.Unmarshal([]byte(text), &v) == nil {
			b, _ := json.MarshalIndent(v, "", "  ")
			text = string(b)
		}
	}
	fmt.Fprintln(a.stdout, text)
	return nil
}
