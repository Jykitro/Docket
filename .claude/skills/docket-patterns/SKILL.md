---
name: docket-patterns
description: "Use when working in the Docket repo, especially before writing a commit message, adding a test, or editing handler, service, store or web files: conventions measured from git history"
metadata:
  version: "1.0.0"
  source: local-git-analysis
  analyzed_commits: "51"
---

# Docket Patterns

Measured on 51 commits. Commands, layout and rules are in `AGENTS.md`; this skill is what the history shows people actually do.

## Commit Conventions

- 45 of 51 subjects (88%) follow `type(scope): lowercase subject`. The 6 that don't are the first commits, before the convention settled.
- Types seen: `fix` (25), `feat` (15), `docs` (2), `test`, `refactor`, `chore` (1 each). Scopes seen: `web` (16), `selfhost` (14), `auth` (9), `mcp` (1).
- Subjects average 54 characters (max 99), no trailing period. They say the user-visible effect: `fix(web): leave /invite/<token> when already signed in`, `feat(web): revoke a pending invite`, `fix(web): send no Referer header`.
- A `fix(web)` commit usually touches one file (8 of 11 sampled). Features may span several.
- No attribution lines in commit messages or PR descriptions.

## Code Architecture

- `cmd/docket` wires; `internal/service` owns rules and validation; `internal/store` owns SQL; `internal/httpapi` and `internal/mcpserver` are thin adapters that call only `service`.
- Selfhost mode adds `internal/auth` (users, sessions) and `internal/selfhost` (one database per profile). The two together are the most edited area: `selfhost/handler.go` (10 commits) and `auth/db.go` (7).
- Frontend: one screen per file in `web/src` (`Board.tsx`, `Today.tsx`, `Mcp.tsx`, ...), shared pieces in `ui.tsx`, API calls and types in `api.ts` (8 commits, the most edited web file).

## Workflows

| Change | Files that move together |
|---|---|
| Selfhost or auth behaviour | `internal/selfhost/handler.go` + `handler_test.go`; `internal/auth/db.go` + `db_test.go` (every commit so far) |
| New or changed MCP tool | `internal/service/mcp.go` (`MCPTools`), `internal/mcpserver/mcpserver.go`, README tool table, `skills/docket-reference/SKILL.md` |
| Skill text or hook JSON | `skills/docket*/SKILL.md`, `web/src/McpAgent.tsx` (hook JSON, skill list), `web/src/messages.ts` for new UI strings |
| New UI string | `tr('Русский текст')` in the component and the en/uk pair in `web/src/messages.ts` |
| New REST endpoint | `internal/httpapi/httpapi.go`, the matching `service` function, a case in `httpapi_test.go`, the README REST table, `web/src/api.ts` |

## Testing Patterns

- Go tests sit next to the code (`*_test.go`) and change in the same commit as the code. `internal/httpapi/httpapi_test.go` builds a real server with `httptest` and a temporary SQLite file; `newEnv(t)` and `connect(t, e)` (an MCP client session) are the helpers to reuse.
- Tests open a local port, so a sandbox that forbids binding fails them with `bind: operation not permitted`.
- The frontend has no test runner. `make typecheck` and `make knip` are its gate; `make check` runs everything.
- A skill change is covered by `TestDocketSkillIsServedAndSentAsInstructions` (each skill is served, the `docket` body stays under 2048 bytes).
