# AGENTS.md

Docket is a personal task planner: one Go process, one SQLite database (a database per user in selfhost mode), a React UI embedded in the binary, and an MCP server so an assistant can read and keep the tasks. This file is for agents that work on the Docket code. The README covers usage; read it for features, configuration and the REST API.

## Commands

Requirements: Go (version in `go.mod`), [Bun](https://bun.sh), `make`, Docker for the image.

| Command | What it does |
|---|---|
| `make install` | Frontend dependencies (`bun install --frozen-lockfile`) |
| `make check` | Typecheck + Knip (unused code and dependencies) + Go tests. Run it before saying a change is done |
| `make test` | `go test ./...` |
| `make typecheck`, `make knip` | Frontend checks alone |
| `make dev-api` | Backend on `:7777` with a separate `./data/dev.db` |
| `make dev-web` | Vite with hot reload; proxies `/api` to `:7777` |
| `make web`, `make build` | Frontend build into `internal/web/dist`; `bin/docket` with the UI embedded |
| `make rebuild`, `make logs`, `make stop` | Docker image and container (data in `./data` is kept) |

- Go tests open a port on localhost (`httptest`). In a sandbox that forbids binding a local port they panic with `bind: operation not permitted`; that is the sandbox, not the code. Run them outside it.
- A sandbox may also block the Go build cache: set `GOCACHE` to a writable directory.
- `go build ./...` works without the UI built: `internal/web/dist` holds only `.gitkeep` until `make web`.

## Layout

```
cmd/docket          config, wiring, graceful shutdown, housekeeping (daily backup, call-log cleanup)
internal/store      SQLite, migrations (migrations/*.sql), all SQL
internal/service    domain rules, validation, activity log, events, MCP settings
internal/httpapi    REST, SSE /api/events, host/origin guard, bearer auth for /mcp, MCP self-check
internal/mcpserver  MCP tools -> service, access policy and call log
internal/auth       selfhost users, sessions, password hashing, login rate limiter
internal/selfhost   one database per profile, routing by session or MCP token
internal/events     pub/sub for SSE
internal/web        embedded SPA (dist)
skills/             agent skills for users of Docket, embedded in the binary (see below)
web/                React + TypeScript + Vite + Tailwind CSS v4; the design tokens are in web/src/styles.css
```

**Layering rule:** `httpapi` and `mcpserver` call only `service`; SQL lives only in `store`. Both channels (REST as the human, MCP as `claude`) go through the same service functions, so validation, history and live updates behave the same.

## Conventions

**Commits.** Conventional style, measured on this repo: `type(scope): lowercase subject`, with type `feat`, `fix`, `docs`, `test`, `refactor` or `chore` and scope `web`, `selfhost`, `auth` or `mcp`. Subjects are short (average 54 characters), no trailing period. One logical change per commit. Don't add attribution lines to commit messages or PR descriptions.

**Tests live next to the code** as `*_test.go` and change in the same commit as the code: in this history `selfhost/handler.go` and `auth/db.go` never changed without their test file. A bug fix gets a test that fails without it. There is no frontend test runner; `tsc` and `knip` are the frontend gate.

**Go.** `service` returns typed errors (`*service.Error` with a code such as `invalid_argument`, `not_found`, `forbidden`, `status_in_use`); `httpapi` maps the code to an HTTP status and `mcpserver` reports it as `code: message`. Validation messages say what is wrong and what is allowed. Don't log secrets or tokens.

**Web.** The UI is Russian, English and Ukrainian. The Russian source text is the key: `tr('Настройки')`, `tr('Онлайн · {v}', { v })`. English and Ukrainian go into `web/src/messages.ts`; a missing entry falls back to Russian, so add all three together. Use `locale()` for dates and numbers, never a hard-coded locale. Styling is Tailwind utilities on the tokens in `web/src/styles.css`; the existing look is shadcn-admin. The UI must not contact any outside host (fonts are bundled): no CDNs, analytics or remote images. Knip fails the build on unused exports and dependencies, so delete code you stop using.

**Data safety.** Never commit `.env`, `data/` or any database. No bundled personal data: Docket ships without a seed (`DOCKET_SEED` is optional and empty by default). The `/mcp` endpoint is behind a bearer token and the Host/Origin guard; don't weaken either.

## MCP tools

The tool catalogue is `service.MCPTools` in `internal/service/mcp.go`; `internal/mcpserver` registers exactly those and panics at startup if a name is missing. Adding a tool means: an entry in `MCPTools` (name, `read` or `write`, description), a handler registered with `add(...)` in `mcpserver.go`, the README tool table and `skills/docket-reference/SKILL.md`. Write tools are blocked in read-only mode by `IsWriteTool`. Results are compact JSON with IDs like `TRK-07`; keep them small, the assistant pays for every byte.

## Agent skills (`skills/`)

Three skills, one `SKILL.md` each, embedded by `skills/skills.go` and served at `/api/mcp/skill/{name}`:

- `docket`: the workflow (task per piece of work, plan, notes, issues, end checklist). Its body is also sent to every client as MCP `instructions`, and Claude Code truncates instructions at 2048 bytes: `TestDocketSkillIsServedAndSentAsInstructions` enforces that. Keep it compact; put detail in the other two.
- `docket-reference`: data model, every tool, errors, conventions, FAQ.
- `docket-setup`: connecting Claude Code, Claude Desktop and Codex, installing skills, the `SessionStart` and `Stop` hooks, troubleshooting.

When you change a tool, a limit or a setup step, change the matching skill in the same commit. The hook JSON exists in two places that must stay identical: `skills/docket-setup/SKILL.md` and `web/src/McpAgent.tsx`. A new skill is a folder `skills/docket-<topic>/SKILL.md` with `name` equal to the folder and a `description` that starts with "Use when" and says only when to use it. The MCP page lists skills from `SKILLS` in `McpAgent.tsx`.

## If the maintainer uses Docket

The maintainer plans their work in Docket. When their Docket MCP server is connected, follow the `docket` skill: a task per piece of work, the plan as a checklist, noticed problems filed as `area: issue` tasks, and a reconcile step before the final message.
