@AGENTS.md

## Claude Code

- Finish a change with `make check` (typecheck, Knip, Go tests) and report what it printed. If the sandbox blocks the Go tests' local port, say that instead of claiming they passed.
- Don't run `docker build` or `make rebuild` unless asked: they rebuild the user's running container.
- Use the `docket` skill when a Docket MCP server is connected; `docket-reference` and `docket-setup` are in `skills/`. The measured repo conventions (commit style, files that change together, where tests go) are in the `docket-patterns` skill.
