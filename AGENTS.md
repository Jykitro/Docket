# AGENTS.md

Plandillo is a personal task planner: one Go process, one SQLite database by default, optional shared PostgreSQL storage for selfhost, a React UI embedded in the binary, and an MCP server so an assistant can read and keep the tasks. This file is for agents that work on the Plandillo code. Start with the [README](README.md) for usage and the [development guide](docs/development/README.md) for build targets and the REST API.

## Commands

Requirements: Go (version in `go.mod`), [Bun](https://bun.sh), `make`, Docker for the image.

| Command | What it does |
|---|---|
| `make install` | Frontend dependencies (`bun install --frozen-lockfile`) |
| `make check` | Typecheck + Knip (unused code and dependencies) + Go tests. Run it before saying a change is done |
| `make test` | `go test ./...` |
| `make test-postgres` | Same tests with `PLANDILLO_TEST_POSTGRES_DSN` pointing to a disposable PostgreSQL server; test role needs `CREATEDB` |
| `make typecheck`, `make knip` | Frontend checks alone |
| `make dev-api` | Backend on `:7777` with a separate `./data/dev.db` |
| `make dev-web` | Vite with hot reload; proxies `/api` to `:7777` |
| `make web`, `make build` | Frontend build into `internal/web/dist`; `bin/plandillo` with the UI embedded (also available as `bin/docket`) |
| `make rebuild`, `make logs`, `make stop` | Docker image and container (data in `./data` is kept) |

- Go tests open a port on localhost (`httptest`). In a sandbox that forbids binding a local port they panic with `bind: operation not permitted`; that is the sandbox, not the code. Run them outside it.
- A sandbox may also block the Go build cache: set `GOCACHE` to a writable directory.
- `go build ./...` works without the UI built: `internal/web/dist` holds only `.gitkeep` until `make web`.

## Layout

```
landing/            public Plandillo site (its own build and README)
cmd/docket          config, wiring, graceful shutdown, housekeeping (daily backup, call-log cleanup)
internal/database   SQLite/PostgreSQL connections, SQL dialect boundary and schema locks
internal/store      task queries, SQLite/PostgreSQL migrations and SQLite backups
internal/service    domain rules, validation, activity log, events, MCP settings
internal/httpapi    REST, SSE /api/events, host/origin guard, bearer auth for /mcp, MCP self-check
internal/mcpserver  MCP tools -> service, access policy and call log; Code Mode at /mcp/code (codemode.go)
internal/auth       selfhost users, sessions, password hashing, login rate limiter
internal/selfhost   private workspace routing by account session or workspace MCP token
internal/events     pub/sub for SSE
internal/web        embedded SPA (dist)
skills/             agent skills for users of Plandillo, embedded in the binary (see below)
web/                React + TypeScript + Vite + Tailwind CSS v4; the design tokens are in web/src/styles.css
```

**Layering rule:** `httpapi` and `mcpserver` call only `service`; task SQL lives in `store`, auth SQL in `auth`, and connection/dialect code in `database`. Both channels (REST as the human, MCP as the calling agent) go through the same service functions, so validation, history and live updates behave the same.

## Conventions

**Commits.** Conventional style, measured on this repo: `type(scope): lowercase subject`, with type `feat`, `fix`, `docs`, `test`, `refactor` or `chore` and scope `web`, `selfhost`, `auth` or `mcp`. Subjects are short (average 54 characters), no trailing period. One logical change per commit. Don't add attribution lines to commit messages or PR descriptions.

**Tests live next to the code** as `*_test.go` and change in the same commit as the code: in this history `selfhost/handler.go` and `auth/db.go` never changed without their test file. A bug fix gets a test that fails without it. There is no frontend test runner; `tsc` and `knip` are the frontend gate.

**Go.** `service` returns typed errors (`*service.Error` with a code such as `invalid_argument`, `not_found`, `forbidden`, `status_in_use`); `httpapi` maps the code to an HTTP status and `mcpserver` reports it as `code: message`. Validation messages say what is wrong and what is allowed. Don't log secrets or tokens.

**Web.** The UI is Russian, English and Ukrainian. The Russian source text is the key: `tr('Настройки')`, `tr('Онлайн · {v}', { v })`. English and Ukrainian go into `web/src/messages.ts`; a missing entry falls back to Russian, so add all three together. Use `locale()` for dates and numbers, never a hard-coded locale. Styling is Tailwind utilities on the tokens in `web/src/styles.css`; the existing look is shadcn-admin. The UI must not contact any outside host (fonts are bundled): no CDNs, analytics or remote images. Knip fails the build on unused exports and dependencies, so delete code you stop using.

**Data safety.** Never commit `.env`, `data/` or any database. No bundled personal data: Plandillo ships without a seed (`PLANDILLO_SEED` is optional and empty by default). The `/mcp` endpoint is behind a bearer token and the Host/Origin guard; don't weaken either.

PostgreSQL is opt-in via `PLANDILLO_POSTGRES_DSN` and requires `PLANDILLO_MODE=selfhost`. Use a dedicated database:
The shared `plandillo_auth` schema stores accounts and workspace-scoped domain tables. SQLite uses one shared file. Fresh-install baselines are `internal/store/shared_sqlite.sql` and `shared_postgres.sql`, with the account catalog in `internal/database/catalog.go`. Preserve the old storage for rollback when switching to a new installation. Test both backends. PostgreSQL backups are external (`pg_dump`/PITR). Never claim a file backup ran. Docker and native installations run one instance per database. Vercel hosts only the landing; the hosted planner is an ordinary selfhost installation ([the Vercel guide](docs/vercel.md)). There is no automatic SQLite import. See the [hosting guide](docs/hosting.md#postgresql-storage) and `compose.postgres.yml`.

## MCP tools

The shared tool catalogue is `MCPTools` in `internal/toolcatalog/catalog.go` (exposed as `service.MCPTools`); `internal/mcpserver` registers exactly those and panics at startup if a name is missing. Adding a tool means: an entry in `MCPTools` (name, `read` or `write`, description), a handler registered with `add(...)` in `mcpserver.go`, the [MCP tool table](docs/agents.md#tools) and `skills/plandillo-reference/SKILL.md`. Write tools are blocked in read-only mode by `IsWriteTool`. Results are compact JSON with IDs like `TRK-07`; keep them small, the assistant pays for every byte.

Code Mode (`/mcp/code`) exposes the same tools as `codemode.<tool>()` inside one `code` tool that runs JavaScript in goja; it derives everything from `MCPTools` and the registered schemas, so a new tool needs no change there. How integrations work and how to add one: `docs/integrations.md`.

## Agent skills (`skills/`)

Three skills, one `SKILL.md` each, embedded by `skills/skills.go` and served at `/api/mcp/skill/{name}`:

- `plandillo`: the workflow (task per piece of work, plan, notes, issues, end checklist). Its body is also sent to every client as MCP `instructions`, and Claude Code truncates instructions at 2048 bytes: `TestPlandilloSkillIsServedAndSentAsInstructions` enforces that. Keep it compact; put detail in the other two.
- `plandillo-reference`: data model, every tool, errors, conventions, FAQ.
- `plandillo-setup`: connecting Claude Code, Claude Desktop and Codex, installing skills, the `SessionStart` and `Stop` hooks, troubleshooting.

Both native marketplaces point to `./skills` as the plugin root. Edit the public skill files there so the two plugins, skills.sh and the Go embed consume the same instructions. Keep the versions in the two plugin manifests in sync when releasing skill changes. Mark repository-only skills with `metadata.internal: "true"` to keep them out of default skills.sh discovery.

When you change a tool, a limit or a setup step, change the matching skill in the same commit. The hook JSON exists in two places that must stay identical: `skills/plandillo-setup/SKILL.md` and `web/src/McpAgent.tsx`. A new skill is a folder `skills/plandillo-<topic>/SKILL.md` with `name` equal to the folder and a `description` that starts with "Use when" and says only when to use it. The MCP page lists skills from `SKILLS` in `McpAgent.tsx`.

## If the maintainer uses Plandillo

The maintainer plans their work in Plandillo. When their Plandillo MCP server is connected, follow the `plandillo` skill: a task per piece of work, the plan as a checklist, noticed problems recorded immediately with `report_issue`, and a reconcile step before the final message.
