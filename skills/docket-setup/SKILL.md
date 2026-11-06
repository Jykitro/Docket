---
name: docket-setup
description: Use when connecting an assistant to Docket (Claude Code, Claude Desktop or Codex), installing the Docket skills, enabling the Docket hooks, or when Docket tools are missing or fail with 401, 403, forbidden or tool_disabled.
---

# Docket setup

Four steps: run Docket, connect the client, install the skills, optionally turn on the hooks. The MCP tab in Docket (`/mcp` in the browser) shows the user's exact address and token and has a "Проверить соединение" self-check.

## 1. Docket is running

- Docker: `make rebuild` in the Docket repo (creates `.env`, builds `docket:local`, starts the container). Logs: `make logs`. Data stays in `./data`.
- The UI and MCP share one address, `http://localhost:7777`; MCP is `http://localhost:7777/mcp` (Streamable HTTP). It listens on `127.0.0.1` only.
- The token looks like `dkt_…`. It is printed to the log once on first start, and is shown (masked, with a Show button) on the MCP tab. `DOCKET_MCP_TOKEN` in `.env` overrides it and disables rotation.
- Selfhost mode (`DOCKET_MODE=selfhost`): every user has their own token on their MCP tab; the same `/mcp` address serves everyone and the token picks the profile.

## 2. Connect the client

**Claude Code** (add `-s user` to make it available in every project; the default scope is this project only):

```bash
claude mcp add --transport http docket http://localhost:7777/mcp \
  --header "Authorization: Bearer <TOKEN>"
claude mcp list
```

**Codex** (the token is read from an environment variable and stays out of `config.toml`; start Codex from a shell where the variable is set):

```bash
export DOCKET_MCP_TOKEN=<TOKEN>
codex mcp add docket --url http://localhost:7777/mcp --bearer-token-env-var DOCKET_MCP_TOKEN
codex mcp list
```

The same entry by hand in `~/.codex/config.toml`:

```toml
[mcp_servers.docket]
url = "http://localhost:7777/mcp"
bearer_token_env_var = "DOCKET_MCP_TOKEN"
```

**Claude Desktop** (needs Node for `npx`). The file is `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS and `%APPDATA%\Claude\claude_desktop_config.json` on Windows; restart Desktop after editing it:

```json
{
  "mcpServers": {
    "docket": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://localhost:7777/mcp",
               "--header", "Authorization:${AUTH}", "--allow-http"],
      "env": { "AUTH": "Bearer <TOKEN>" }
    }
  }
}
```

Check it: ask the assistant to call `list_projects`. Twelve tools should be listed (five read, seven write); fewer means some are switched off on the MCP tab.

**Docket on a server or in selfhost mode:** use its public address instead of `localhost:7777`, for example `https://docket.example.com/mcp`, and the token from your own MCP tab. For Claude Desktop with an `https` address drop `--allow-http` (it is only for plain `http`). Put the host name (no scheme, no port) in `DOCKET_ALLOWED_HOSTS` in `.env` and restart the container, or every call gets `403`. TLS is the reverse proxy's job; the README's Self-host section explains it. Re-adding a server after a token change needs a remove first: `claude mcp remove docket` or `codex mcp remove docket`.

## 3. Install the skills

The server already sends the `docket` workflow to every connected client as MCP instructions. Installing the skills makes it trigger from the first message and adds the two detailed ones. Folder per skill, one `SKILL.md` each:

| Client | Skills folder |
|---|---|
| Claude Code | `~/.claude/skills/<name>/SKILL.md` (or `.claude/skills/` in a project) |
| Codex | `~/.agents/skills/<name>/SKILL.md` |
| Claude Desktop | no folder: paste the text into the project instructions |

Fastest on macOS and Linux: the MCP tab's "Install in one command" button copies one line. It installs all three skills and registers the server in Claude Code and Codex, whichever are in PATH. Codex still needs `DOCKET_MCP_TOKEN` in the shell profile; the script prints the line to add. The line is `curl -fsSL http://localhost:7777/api/mcp/install.sh | DOCKET_URL=http://localhost:7777/mcp DOCKET_TOKEN=<token> sh`; the script holds no secrets, so it is public even in selfhost mode. By hand:

```bash
for s in docket docket-reference docket-setup; do
  mkdir -p ~/.claude/skills/$s     # Codex: ~/.agents/skills/$s
  curl -fsS http://localhost:7777/api/mcp/skill/$s -o ~/.claude/skills/$s/SKILL.md
done
```

In selfhost mode the API needs a login, so use the Download buttons on the MCP tab, or copy the folders from the repo's `skills/` directory. Skills are `docket` (the workflow), `docket-reference` (tools, fields, errors, conventions), `docket-setup` (this one).

## 4. Hooks (optional, strict mode)

Hooks make the workflow mandatory instead of advised. One JSON works for both clients:

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{
      "type": "command",
      "command": "echo 'Docket: every piece of work needs a task. Search for it or create it with the plan, status doing. Follow the docket skill.'"
    }] }],
    "Stop": [{ "hooks": [{
      "type": "command",
      "command": "grep -Eq '\"stop_hook_active\": ?true' && exit 0; echo '{\"decision\":\"block\",\"reason\":\"Docket end checklist: tick finished steps, add_note the result, move_tasks to done only if checks passed, file noticed problems as area issue tasks, check get_board doing. Not tracked in Docket: say so and finish.\"}'"
    }] }]
  }
}
```

- `SessionStart`: its output becomes context at the start of every session.
- `Stop`: blocks the first stop of a turn with the checklist; the second stop passes (`stop_hook_active`), so it cannot loop. In a repo that doesn't use Docket the agent replies "not tracked" and finishes.
- Claude Code: save as `.claude/settings.json` in the project (merge into an existing `hooks` object), or `~/.claude/settings.json` for everywhere. `/hooks` lists what is active.
- Codex: save as `~/.codex/hooks.json`. The file is global, so it runs in every repo. The `hooks` feature is on by default in current versions (`codex features list` shows it). An older Codex needs `[features]` `hooks = true` (earlier name `codex_hooks = true`) in `~/.codex/config.toml`.
- To switch it off, delete the `Stop` entry (keep `SessionStart` if you only want the reminder).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `401` on every call | Wrong or rotated token, or a missing `Bearer ` prefix. Copy the current token from the MCP tab, remove the server (`claude mcp remove docket`, `codex mcp remove docket`) and add it again; for Codex export the new `DOCKET_MCP_TOKEN` first; for Desktop edit `AUTH`. Rotating invalidates the old token. If `.env` sets `DOCKET_MCP_TOKEN`, that value is the token and rotation is off |
| `403` | `Host` or `Origin` is not allowed. Add the host name (no scheme, no port) to `DOCKET_ALLOWED_HOSTS` in `.env` (default `localhost,127.0.0.1,docket.localhost`) and restart the container |
| Connection refused | Container is down: `docker ps`, `make logs`, `make rebuild`. Check the port `7777` |
| Codex says the token variable is not set | `DOCKET_MCP_TOKEN` must exist in the shell that starts Codex; export it in the shell profile |
| Tools missing or `tool_disabled` | Switched off on the MCP tab. `delete_tasks` is off by default on purpose |
| `forbidden` on writes | Access mode is "Только чтение" on the MCP tab. Reads still work; the user switches to "Чтение и запись" if they want writes. Tell them, don't work around it |
| Desktop shows no server | `npx` not found or the JSON is invalid. Restart Desktop after editing the file |
| Hooks do nothing | Claude Code: check `/hooks` and that the JSON is valid and merged. Codex: the file must be exactly `~/.codex/hooks.json` |

Still failing: press "Проверить соединение" on the MCP tab. It runs five checks (server, MCP protocol, token, host and origin, database) and prints what to do for the one that fails. Don't work around a refusal: tell the user what you saw.
