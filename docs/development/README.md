# Development

Build the planner from the repository root. For the public website, follow the [landing page guide](../../landing/README.md).

Requirements: Go (see [go.mod](../../go.mod)), [Bun](https://bun.sh) and `make`.

```bash
make install   # frontend dependencies (bun install --frozen-lockfile)
make check     # typecheck + Knip (unused code and dependencies) + Go tests
```

Go tests open a port on localhost. Run `make help` for every target:

| Target | What it does |
|---|---|
| `make web` | Frontend build: `tsc --noEmit` and `vite build` into `internal/web/dist` |
| `make build` | Builds `bin/plandillo` with the UI embedded |
| `make test` | Backend tests (`go test ./...`) |
| `make test-postgres` | Same domain, auth, REST and MCP tests against PostgreSQL; requires `PLANDILLO_TEST_POSTGRES_DSN` |
| `make typecheck`, `make knip` | Frontend checks |
| `make dev-api` | Backend on `:7777` with a separate `./data/dev.db` |
| `make dev-web` | Vite with hot reload (proxies `/api` to `:7777`) |
| `make image`, `make rebuild`, `make stop`, `make logs` | Docker image and container |
| `make clean` | Removes `bin/` and the built frontend |

To run the PostgreSQL integration tests locally, point `PLANDILLO_TEST_POSTGRES_DSN` at a disposable PostgreSQL server,
using a `postgres://` URL and a test role with `CREATEDB`. Each test creates and drops a randomly named database;
the database in the URL is used only as the administrative connection. Do not use a production server. For example:

```bash
docker run -d --name plandillo-pg-tests -e POSTGRES_USER=plandillo -e POSTGRES_PASSWORD=plandillo_test_only \
  -e POSTGRES_DB=plandillo_test -p 127.0.0.1:55483:5432 postgres:19.0-alpine
# Wait until: docker exec plandillo-pg-tests pg_isready -U plandillo -d plandillo_test
PLANDILLO_TEST_POSTGRES_DSN='postgres://plandillo:plandillo_test_only@localhost:55483/plandillo_test?sslmode=disable' make test-postgres
docker rm -f plandillo-pg-tests
```

CI runs both the default SQLite checks and the PostgreSQL suite.

### Board query benchmark

Run `go test ./internal/store -run '^$' -bench BenchmarkBoardCards -benchmem` to measure the first and 51st pages of a 6,000-task project with subtasks. Each page contains 100 summary cards. The default run uses a temporary SQLite file. Set `PLANDILLO_TEST_POSTGRES_DSN` to repeat it in a disposable PostgreSQL database. This benchmark times the store transaction. Measure the REST endpoint separately to include authentication, routing and JSON encoding.

For frontend work with hot reload, run `make dev-api` in one terminal and `make dev-web` in another.

The [repository instructions](../../AGENTS.md) describe the conventions for working on Plandillo with an AI agent.

## Project layout

```
landing/            public Plandillo website, built separately from the planner
cmd/plandillo       config, wiring, graceful shutdown, housekeeping (daily backup and call-log cleanup)
internal/database   SQLite/PostgreSQL connections, SQL dialect boundary and schema locks
internal/store      task queries, SQLite/PostgreSQL migrations and SQLite backups
internal/service    domain rules, validation, activity, events, MCP settings
internal/httpapi    REST, SSE /api/events, host/origin guard, bearer auth for /mcp, MCP self-check
internal/mcpserver  MCP tools → service, access policy and call log; Code Mode (/mcp/code, codemode.go)
internal/events     pub/sub for SSE
internal/push       Web Push (PLANDILLO_PUSH=on): VAPID key, device subscriptions, agent-finished and morning notices
internal/web        embedded SPA (dist)
web/                React + TypeScript + Vite + Tailwind CSS v4 (shadcn-admin look; tokens in web/src/styles.css)
```

Layering rule: `httpapi` and `mcpserver` call only `service`; task SQL lives in `store`, auth SQL in `auth`, push subscription SQL in `push`, and connection/dialect code in `database`.
How the integrations (MCP, Code Mode) work and how to add one: [`docs/integrations.md`](../integrations.md).

See the [REST API reference](rest-api.md) for the HTTP endpoints. When updating the base image in the Dockerfile, verify its pinned manifest supports both Linux architectures.
