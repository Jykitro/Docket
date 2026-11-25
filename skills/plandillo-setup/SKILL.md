---
name: plandillo-setup
description: Use when connecting an assistant to Plandillo through MCP, installing the Plandillo skills, enabling the Plandillo hooks, or when Plandillo tools are missing or fail with 401, 403, forbidden or tool_disabled.
---

# Plandillo setup

Four steps: run Plandillo, connect the client, install the skills, optionally turn on the hooks. The Integrations tab in Plandillo (`/integrations` in the browser) shows the user's exact address and token and has a "Проверить соединение" self-check.

## 1. Plandillo is running

- Docker: `make rebuild` in the Plandillo repo (creates `.env`, builds the local image and starts the container). Logs: `make logs`. Data stays in `./data`.
- The UI and MCP share one address, `http://localhost:7777`; MCP is `http://localhost:7777/mcp` (Streamable HTTP). It listens on `127.0.0.1` only.
- The token looks like `dkt_…`. Use Show or Copy token on the Integrations tab. `DOCKET_MCP_TOKEN` in `.env` overrides it and disables rotation.
- Selfhost Code Mode requires the isolated worker in `compose.selfhost.yml` or `compose.postgres.yml`. The operator shares `DOCKET_CODE_SOCKET` with Plandillo. When the worker is offline, ordinary `/mcp` is still available.
- Selfhost mode (`DOCKET_MODE=selfhost`): the first administrator obtains the bootstrap code with `plandillo setup-code` (Docker: `docker compose -f compose.selfhost.yml exec docket /app/plandillo setup-code`; substitute `compose.postgres.yml` when using the PostgreSQL stack). Choose the workspace first and copy its token from Integrations. The same `/mcp` address serves every workspace. Archiving a workspace revokes its generated token. Storage defaults to SQLite; operators can set `DOCKET_POSTGRES_DSN` for a dedicated PostgreSQL database (see the [hosting guide](https://github.com/huntly-digital/plandillo/blob/main/docs/hosting.md#self-host)). This does not import existing SQLite data; PostgreSQL backups are managed by the operator.

## 2. Connect the client

The Integrations tab supplies a client-specific URL for both MCP and Code Mode. For a Streamable HTTP client, copy the endpoint and configure the header `Authorization: Bearer <TOKEN>` using the token from Integrations. To name the agent, choose **Other client** and enter its name, or append `?agent=my-agent` to the endpoint. If an existing connection shows up as “MCP agent”, update its URL in the client. The token still selects the workspace.

**Claude Code and Codex:** select the client on the Integrations tab and copy its config. The terminal command prompts for the token with echo disabled and exports a variable specific to the selected endpoint. Start the client in that shell, or have a credential manager set that variable at launch. Claude Code stores a literal environment-variable reference in its HTTP header. Codex uses `--bearer-token-env-var`. After rotating the token, repeat the hidden prompt. When switching servers or between MCP and Code Mode, use the new endpoint's command.

**Claude Desktop** (needs Node for `npx`). The file is `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS and `%APPDATA%\Claude\claude_desktop_config.json` on Windows; restart Desktop after editing it:

```json
{
  "mcpServers": {
    "plandillo": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://localhost:7777/mcp?agent=claude",
               "--header", "Authorization:${AUTH}", "--allow-http"],
      "env": { "AUTH": "Bearer <TOKEN>" }
    }
  }
}
```

Check it: ask the assistant to call `list_projects`. The catalogue has 19 tools. A fresh install lists 18 because `delete_tasks` is switched off by default. The Integrations tab shows which tools are enabled.

**Code Mode** (optional): the same commands with `/mcp/code` instead of `/mcp`. The client then sees one tool, `code`, and writes JavaScript that calls the tools as `codemode.list_projects({})`; fewer round trips on big jobs. Same token, switches and log. Keep one of the two registered as `plandillo`; the Integrations tab switches the address and the install command.

**Plandillo on a server or in selfhost mode:** use its public address instead of `localhost:7777`, for example `https://planner.example.com/mcp`, and the token from your own Integrations tab. For Claude Desktop with an `https` address drop `--allow-http` (it is only for plain `http`). Put the host name (no scheme, no port) in `DOCKET_ALLOWED_HOSTS` in `.env` and restart the container, or every call gets `403`. TLS is the reverse proxy's job; the hosting guide covers the proxy setup. Re-adding a server after a token change needs a remove first: `claude mcp remove plandillo` or `codex mcp remove plandillo`.

## 3. Install the skills

The server already sends the `plandillo` workflow to every connected client as MCP instructions. Installing the skills makes it trigger from the first message and adds the two detailed ones. Folder per skill, one `SKILL.md` each:

| Client | Skills folder |
|---|---|
| Claude Code | `~/.claude/skills/<name>/SKILL.md` (or `.claude/skills/` in a project) |
| Codex | `~/.agents/skills/<name>/SKILL.md` |
| Claude Desktop | no folder: paste the text into the project instructions |

Fastest on macOS and Linux: the Integrations tab's "Install in one command" button copies one line. It installs all three skills and registers the server in Claude Code and Codex, whichever are in PATH. Run the printed export command in the shell that will launch the client. Copy the token separately from Integrations and paste it at the hidden prompt. The installation command is `curl -fsSL http://localhost:7777/api/mcp/install.sh | PLANDILLO_URL=http://localhost:7777/mcp sh`. An existing installation with Docket skill folders keeps its names. To update a manually registered `docket` connection under its original name, use `DOCKET_URL` in the install command.

By hand:

```bash
for s in plandillo plandillo-reference plandillo-setup; do
  mkdir -p ~/.claude/skills/$s     # Codex: ~/.agents/skills/$s
  curl -fsS http://localhost:7777/api/mcp/skill/$s -o ~/.claude/skills/$s/SKILL.md
done
```

In selfhost mode the API needs a login, so use the Download buttons on the Integrations tab, or copy the folders from the repo's `skills/` directory. Skills are `plandillo` (the workflow), `plandillo-reference` (tools, fields, errors, conventions), `plandillo-setup` (this one).

## 4. Hooks (optional, strict mode)

Hooks make the workflow mandatory instead of advised. One JSON works for both clients:

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{
      "type": "command",
      "command": "echo 'Plandillo: every piece of work needs a task. Search for it or create it with the plan, status doing. Follow the plandillo skill.'"
    }] }],
    "Stop": [{ "hooks": [{
      "type": "command",
      "command": "grep -Eq '\"stop_hook_active\": ?true' && exit 0; echo '{\"decision\":\"block\",\"reason\":\"Plandillo end checklist: tick finished steps, add_note the result, move_tasks to done only if checks passed, record findings with report_issue and list their IDs, check get_board doing. Not tracked in Plandillo: say so and finish.\"}'"
    }] }]
  }
}
```

- `SessionStart`: its output becomes context at the start of every session.
- `Stop`: blocks the first stop of a turn with the checklist; the second stop passes (`stop_hook_active`), so it cannot loop. In a repo that doesn't use Plandillo the agent replies "not tracked" and finishes.
- Claude Code: save as `.claude/settings.json` in the project (merge into an existing `hooks` object), or `~/.claude/settings.json` for everywhere. `/hooks` lists what is active.
- Codex: save as `~/.codex/hooks.json`. The file is global, so it runs in every repo. The `hooks` feature is on by default in current versions (`codex features list` shows it). An older Codex needs `[features]` `hooks = true` (earlier name `codex_hooks = true`) in `~/.codex/config.toml`.
- To switch it off, delete the `Stop` entry (keep `SessionStart` if you only want the reminder).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `401` on every call | Wrong or rotated token, or a missing `Bearer ` prefix. Copy the current token from the Integrations tab, remove the server (`claude mcp remove plandillo`, `codex mcp remove plandillo`) and add it again; for the CLIs repeat the endpoint-specific hidden token prompt first; for Desktop edit `AUTH`. Rotating invalidates the old token. If `.env` sets `DOCKET_MCP_TOKEN`, that value is the token and rotation is off |
| `403` | `Host` or `Origin` is not allowed. Add the host name (no scheme, no port) to `DOCKET_ALLOWED_HOSTS` in `.env` (default `localhost,127.0.0.1,docket.localhost`) and restart the container |
| Connection refused | Container is down: `docker ps`, `make logs`, `make rebuild`. Check the port `7777` |
| Codex says the token variable is not set | Run the export command for this endpoint from Integrations, then start Codex from the same shell |
| Tools missing or `tool_disabled` | Switched off on the Integrations tab. `delete_tasks` is off by default on purpose |
| `forbidden` on writes | Access mode is "Только чтение" on the Integrations tab. Reads still work; the user switches to "Чтение и запись" if they want writes. Tell them, don't work around it |
| Desktop shows no server | `npx` not found or the JSON is invalid. Restart Desktop after editing the file |
| Hooks do nothing | Claude Code: check `/hooks` and that the JSON is valid and merged. Codex: the file must be exactly `~/.codex/hooks.json` |

Still failing: press "Проверить соединение" on the Integrations tab. It runs five checks (server, MCP protocol, token, host and origin, database) and prints what to do for the one that fails. Don't work around a refusal: tell the user what you saw.
