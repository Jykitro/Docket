---
name: plandillo-setup
description: Use when installing Plandillo skills or a client plugin, connecting a workspace to Claude Code, Claude Desktop or Codex, setting up optional hooks, or diagnosing missing tools and connection errors.
---

# Connect an agent to Plandillo

A new installation needs two things: the planner instructions and a connection to the user's workspace. A working MCP connection already supplies the core workflow. If only the skills were installed, continue with **Connect the workspace** below.

## Choose an installation

| Starting point | What to do |
|---|---|
| A running board, macOS or Linux | In **Integrations → Agent setup**, select the CLI and **Skills + MCP for this board**. Copy the command into a terminal. It installs all three skills and registers MCP for that client. Follow the printed login instructions in the browser. Older Docket installations keep their names. |
| A team using skills.sh | In the target project run `npx skills add huntly-digital/plandillo --skill plandillo plandillo-reference plandillo-setup --agent codex claude-code`. Select just one agent if appropriate. Add `-g` for a personal install. |
| A team using native plugins | Add the repository marketplace and install its Plandillo plugin using the commands below. The plugin includes all three skills and the endpoint `https://app.plandillo.dev/mcp`, the maintainer's private board with sign-up closed. For your own board or Code Mode, use the board installer or skills.sh and register your URL. |
| Manual install | Download each SKILL.md from **Integrations** into its own named folder under `~/.claude/skills/` or `~/.agents/skills/` (Codex). The signed-in UI works on self-hosted workspaces. For Claude Desktop projects, copy the workflow into project instructions if needed. |

Codex:

```sh
codex plugin marketplace add huntly-digital/plandillo
codex plugin add plandillo@huntly-digital
```

Claude Code:

```sh
claude plugin marketplace add huntly-digital/plandillo
claude plugin install plandillo@huntly-digital
```

Use one distribution method per client to avoid loading duplicate instructions. In a local checkout, replace the repository slug after `marketplace add` or `skills add` with the absolute path to the checkout. For an embedded installer matching your server version, use its own **Integrations** tab.

## Connect the workspace

1. Open the intended workspace and **Integrations**. Select **MCP** for individual tools at `/mcp`, or **Code Mode** for one JavaScript tool at `/mcp/code`. Use the URL shown there. A hosted workspace usually uses HTTPS. The local default is `http://localhost:7777`.
2. Select your client, choose **MCP only** and use **Copy config**. Register the URL. Codex can open sign-in automatically. If needed, run `codex mcp login plandillo`. For Claude Code use `claude mcp login plandillo`. For a plugin connection select its server in the client's MCP settings. Claude Code also offers authentication in `/mcp`.
3. In the browser, sign in if prompted, select the workspace and approve the requested permissions. A local single-user board requires a loopback address. The client handles token exchange and renewal. For Claude Desktop, the generated config runs `mcp-remote` via `npx`; Desktop needs access to Node. An OAuth-capable remote connector can use a reachable hosted HTTPS URL directly.
4. In a fresh session, ask for `list_projects({})`. On Code Mode send `{"code":"return await codemode.list_projects({})"}` to `code`. Confirm the expected projects and the successful read in the board's call log. An empty list is valid for a fresh workspace. Check the actor label before relying on attribution.

To switch endpoints or workspaces, authorize the new connection. Revoke it in the workspace’s **Integrations → Connected apps**. For scripts and bearer-only clients, store the manual workspace token in the client’s secret settings. Keep credentials out of chats and shared config.

## Diagnose a connection

| Observation | Next check |
|---|---|
| Skills are visible but no planner tools | Check that the selected workspace has a registered HTTP MCP connection and restart the client. On Code Mode, seeing only `code` is expected. |
| Connection refused | Check that the configured host and port are reachable from the client. For a local installation check whether the Plandillo process or container is running. |
| HTTP 401 | Use the client’s MCP login again for this endpoint after expiry or revocation. For a manually configured bearer, check the workspace and the client’s secret after rotation. |
| HTTP 403 | Check the failing host/origin against the operator's permitted hosts. Use the public URL shown in Integrations for a hosted board. |
| Read succeeds, write is missing or returns `forbidden` | Check both the approved OAuth permissions and the workspace’s access mode. Authorize again to request additional access. |
| A specific tool is missing or returns `tool_disabled` | Check its switch in Integrations. Fresh installations disable `delete_tasks`. |
| Code Mode returns `unavailable` | In selfhost, the operator checks the isolated worker. Ordinary MCP uses its own endpoint and the same workspace policy. |
| The cause is unclear | Run **Check connection** in Integrations. It checks the server, MCP protocol, token, allowed hosts and database. Compare with the client's error and the call log. |

For starting a new server, administrator bootstrap and worker configuration, see the [hosting guide](https://github.com/huntly-digital/plandillo/blob/main/docs/hosting.md).

## Optional session reminders

Install hooks only when the user wants reminders for tracked work. Merge these entries into the existing `hooks` object. Claude Code accepts project-level `.claude/settings.json`; Codex uses `~/.codex/hooks.json`, which applies across projects. The Integrations tab also provides a copy button.

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{
      "type": "command",
      "command": "echo 'Plandillo: for tracked work, follow the Plandillo workflow. Answer board questions with reads and respect a request to skip tracking.'"
    }] }],
    "Stop": [{ "hooks": [{
      "type": "command",
      "command": "grep -Eq '\"stop_hook_active\": ?true' && exit 0; echo '{\"decision\":\"block\",\"reason\":\"Plandillo end checklist: tick finished steps, add_note the result, move_tasks to done only if checks passed, record findings with report_issue and list their IDs, check get_board doing. For read-only requests or untracked work, finish without planner writes.\"}'"
    }] }]
  }
}
```

`SessionStart` supplies a reminder. `Stop` requests a final check once per turn and passes on the second stop. To keep only the startup reminder, remove the Plandillo Stop entry.
