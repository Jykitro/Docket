# Integrations

For people who work on Plandillo's code. User-facing setup lives in the [agent setup guide](agents.md) and in
[`skills/plandillo-setup`](../skills/plandillo-setup/SKILL.md).

An **integration** is a way for an AI agent to read and change tasks
in Plandillo. A person uses the web UI over the REST API. An agent connects through one of two endpoints on the **Integrations** tab (`/integrations`). Code Mode also needs the [isolated worker](hosting.md#code-mode-worker).

| Integration | Address | What the assistant sees | Code |
|---|---|---|---|
| MCP | `/mcp` | 19 tools, one per action (`get_today`, `create_tasks`, …) | [`internal/mcpserver/mcpserver.go`](../internal/mcpserver/mcpserver.go) |
| Code Mode | `/mcp/code` | One tool, `code`, that runs JavaScript calling the same 19 tools | [`internal/mcpserver/codemode.go`](../internal/mcpserver/codemode.go) |

Both speak MCP over Streamable HTTP, so any MCP client connects to either. A client uses one endpoint at a time. New installations register it as `plandillo`. The installer keeps the existing `docket` registration on upgrades, so switching the endpoint updates that connection.

## How a call flows

```
assistant ──HTTP──▶ Host/Origin guard ─▶ bearer token ─▶ mcpserver.Handler
                     (httpapi.HostGuard)  (httpapi.bearer,   ├─ /mcp       classic server ─┐
                                          selfhost: token     └─ /mcp/code  code server     │ in-memory MCP session
                                          picks the profile)        goja runs the code ─────┘ per codemode.* call
                                                                                            ▼
                                                         policy middleware: access mode, tool switches, call log
                                                                                            ▼
                                                         tool handler ─▶ service ─▶ store (SQLite / PostgreSQL)
```

What every integration gets for free, because it all sits in front of or under the classic server:

- **Auth.** One token per workspace (`service.MCPToken`), checked on every request so a rotated token works at once.
  In selfhost mode the token also chooses the profile (`selfhost/handler.go`, `h.mcp`).
- **Policy.** `policy()` in `mcpserver.go` hides switched-off tools from `tools/list`, refuses them with
  `tool_disabled`, and refuses write tools with `forbidden` in read-only mode. The list of tools and their group
  (`read` or `write`) is `service.MCPTools`.
- **Call log.** Every `tools/call` is written by `service.RecordMCPCall` (tool, short arguments, duration, error code)
  and shown live on the Integrations tab; it is kept 7 days.
- **Same rules as the UI.** Tool handlers call `service` with the calling agent's ID, so validation, history, events and
  live updates in the browser are the same as for a human edit. Integrations never touch `store` directly.
- **Instructions.** The body of the `plandillo` skill goes to every client as MCP `instructions` (`skills.Instructions()`).

The optional `?agent=` URL parameter supplies a per-client attribution label on either endpoint. Otherwise
the middleware reads MCP client info on the tool call or a recognized HTTP User-Agent. Anonymous calls use
`mcp`. Code Mode passes the resolved label to its in-memory client. Names are normalized to short lowercase
identifiers. The reserved profile-owner ID `denis` is unavailable over MCP. The label grants no permissions.

## MCP

`Handler` builds one `mcp.Server`, registers each tool with `add(...)`, and serves it stateless (`JSONResponse`, no
sessions). `add` turns a typed Go handler into an MCP tool: the input schema comes from the input struct (`json` and
`jsonschema` tags), the result is one compact JSON text block, an error is reported as `code: message` with the code
from `service.ErrorCode`. IDs in results look like `TRK-07`; keep results small, the assistant pays for every byte.

## Code Mode

The idea comes from [Cloudflare's Code Mode](https://developers.cloudflare.com/agents/tools/codemode/): models are
better at writing code than at chaining many tool calls, and a script can loop, filter and join results without
sending each intermediate result through the model's context. Cloudflare runs the code in a Worker sandbox; Plandillo
runs it in [goja](https://github.com/dop251/goja), a JavaScript engine in pure Go, so it works offline and needs
nothing installed.

**What the assistant sees.** `tools/list` on `/mcp/code` returns one tool, `code`, with input `{ code: string }`. Its
description is a short preamble plus TypeScript declarations generated from the tools the classic server lists right
now, so switched-off tools and, in read-only mode, write tools are not declared:

```ts
declare const codemode: {
  /** Full task by ID such as TRK-07: description, subtasks, notes, custom fields and the last 20 history entries. */
  get_task(args: {
    /** task ID such as TRK-07 */
    id: string;
  }): Promise<any>;
  // … one method per tool
};
```

**What the assistant sends.** An async arrow function, or just the statements of its body:

```js
async () => {
  const today = await codemode.get_today({})
  const late = today.overdue.filter((t) => t.priority === 'P0')
  await codemode.move_tasks({ ids: late.map((t) => t.id), planned_for: today.date })
  return late.map((t) => t.id)
}
```

**What comes back.** `{"result": <the return value>, "logs": ["console.log lines"]}`. A failed tool call inside the
code throws `Error("not_found: …")`; the code may catch it. If it is not caught, the `code` call fails with that same
`code: message`, so the call log keeps a stable error code.

**How it runs** (`runCode`):

1. A fresh goja runtime per call. Globals: `codemode` (one function per name in `service.MCPTools`) and `console`.
   No `fetch`, timers, `require`, files or environment.
2. `codemode.x(args)` serializes `args` to JSON and calls tool `x` on the classic server over an in-memory MCP
   session (`connect`). The call goes through `policy()` like any other, so it is checked and logged under its own
   name; the outer `code` call is logged as `code`.
3. Limits: 30 seconds (`codeTimeout`, enforced with `vm.Interrupt`), 100 tool calls, a 50 KB result, 50 log lines.
   Errors: `timeout`, `invalid_argument` (syntax error, a thrown `TypeError`, too many calls, result too large) or the
   tool's own code.

**Execution.** Plandillo sends each invocation to a disposable evaluator in the Code Mode container. The application admits one execution per workspace and two globally. Excess calls return `rate_limited`. The worker has a 256 MiB memory limit, one CPU, a read-only root filesystem and no network. The Unix socket carries the program and its tool calls. Only the application has workspace storage and validates each callback against the initiating owner's current tool policy. A disconnected or stopped worker fails its request with `unavailable`. Single-user installations use the same worker. The local Compose example provisions it, and standalone installations set `DOCKET_CODE_SOCKET` to their worker socket.

## Recipes

**Add a tool.** It appears in both integrations at once.

1. An entry in `service.MCPTools` (name, `read` or `write`, description). The group decides read-only behaviour.
2. A handler and `add(s, "name", t.handler)` in `mcpserver.go`. `Handler` panics at startup if a registered name is
   missing from `MCPTools`.
3. A test next to the code, the [MCP tool table](agents.md#tools), and `skills/plandillo-reference/SKILL.md`.

Code Mode needs nothing: its declarations and `codemode` methods come from the same lists.

**Add an integration.** Keep it a thin adapter over the classic server or `service`, so auth, policy and the call log
stay in one place:

1. Code in `internal/mcpserver` (if it speaks MCP) or a new package that calls only `service`.
2. A route in both routers: `internal/httpapi/httpapi.go` (behind `bearer`) and `internal/selfhost/handler.go`
   (behind `h.mcp`, which picks the profile by token). Never weaken the Host/Origin guard or the bearer check.
3. A switch entry in `INTEGRATIONS` in `web/src/Mcp.tsx` (name, path, one-line description; translations in
   `web/src/messages.ts`).
4. A test that drives it over HTTP, a row in the table above, and the matching setup step in `skills/plandillo-setup`.

## Tests

- `internal/mcpserver/mcpserver_test.go`: the classic tools over JSON-RPC (wiki tools, error codes, call log).
- `internal/mcpserver/codemode_test.go`: Code Mode end to end: declarations follow the policy, calls reach the tools
  and the log, errors keep their code, read-only and switched-off tools are refused, runaway code times out.
- `internal/httpapi`: the self-check and the bearer guard. `make check` runs them all.
