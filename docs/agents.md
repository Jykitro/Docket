# Connecting your AI agent

Connect Codex, Claude Code or another OAuth-capable Streamable HTTP client using just the server URL.

1. In **Integrations**, select your client under **Agent setup**. For a standalone connection choose **MCP only**, select **MCP** or **Code Mode** and copy the configuration. The local MCP URL is `http://localhost:7777/mcp`. On a hosted installation use its HTTPS URL.
2. Codex can open the sign-in page when you add the server. Use `codex mcp login plandillo` if needed. For Claude Code run `claude mcp login plandillo`. Claude Code also offers authentication through `/mcp`. For a plugin-managed connection, select its server in the client's MCP settings.
3. Sign in to Plandillo if prompted, select the workspace and approve the requested access. Clear the write checkbox to allow reads only. The client receives and refreshes its tokens automatically.
4. Ask “What’s on my board today?” and check the workspace call log. To disconnect one client, open **Integrations → Connected apps** in that workspace.

For a local board, authorize using a loopback address on the machine running Plandillo. A remote client must be able to reach the HTTPS installation. For a script or a client that only supports static credentials, copy the workspace token from **Integrations** into its secret settings and send `Authorization: Bearer <TOKEN>`. Rotating this token also requires OAuth clients for that workspace to authorize again.

<p align="center">
  <img src="images/mcp-dark.png" alt="Integrations showing the server address, masked token and access mode" width="820">
</p>

## Try a shared task

Open a project in the web UI and connect a client using **Integrations**. Try these requests with your connected agent. For the last two, enable write access in **Integrations**.

- “Find the release checklist in my project. If it exists, show me the outstanding steps.” The agent searches and reads the task before making a change.
- “Add a task to review mobile navigation tomorrow, with a checklist for the menu and keyboard focus.” Open the new card to see its planned date and description.
- “Add the test results to that task. Mark it done if the checks passed.” The note and status appear in the same card, along with the name of the agent that changed it.

## Code Mode

[Start the isolated worker](hosting.md#code-mode-worker) to enable Code Mode.

At `/mcp/code`, the agent authorizes that endpoint separately and
sees one tool, `code`, after [Cloudflare's Code Mode](https://developers.cloudflare.com/agents/tools/codemode/): it
writes JavaScript that calls the 19 tools as `await codemode.get_today({})`, loops and filters inside Plandillo, and gets
back only the result. That saves round trips and context on big jobs (triage, bulk moves, reports). The code runs in
an embedded sandbox with no network or files, for at most 30 seconds and 100 tool calls; access mode, tool switches
and the call log apply to every call it makes. Pick MCP or Code Mode on the **Integrations** tab: the address, the
configs and the install command follow. How it works inside: [Integration internals](integrations.md).

## Install agent skills

Choose one method per client. For a new board, **Integrations → Agent setup → Skills + MCP for this board** installs all three skills and registers MCP in the selected CLI on macOS or Linux. The script runs locally. Follow the printed login instructions to authorize in the browser.

The native plugins include the three skills and an MCP connection to a local board at `http://localhost:7777/mcp`. Claude Code reads `PLANDILLO_URL` for another address, for example `PLANDILLO_URL=https://plans.example.com/mcp claude`; Codex keeps the local address. For a self-hosted board in Codex, or for Code Mode, install the skills and register the URL from that board’s **Integrations** tab. The skills.sh command installs the instructions only.

### skills.sh

Run this in the project where you want the skills. Select one or both agents. Add `-g` for a personal installation.

```sh
npx skills add huntly-digital/plandillo --skill plandillo plandillo-reference plandillo-setup --agent codex claude-code
```

The CLI reads the three named skills from the public repository. You can also download their files directly under **Integrations → Download individual skills**.

### Codex plugin

```sh
codex plugin marketplace add huntly-digital/plandillo
codex plugin add plandillo@huntly-digital
```

### Claude Code plugin

```sh
claude plugin marketplace add huntly-digital/plandillo
claude plugin install plandillo@huntly-digital
```

To test an unreleased checkout, replace `huntly-digital/plandillo` in the **add source** command with the absolute path to the checkout. Use the same plugin ID. The native setup skill is available as `$plandillo:plandillo-setup` in Codex and `/plandillo:plandillo-setup` in Claude Code. Open a new client session and ask it to list your Plandillo projects. On Code Mode it calls `code` with `return await codemode.list_projects({})`. Check the workspace and agent label in the Integrations call log.

### Board address for the plugins

Both plugins connect to `http://localhost:7777/mcp`, the address of the one-command Docker install, so a local board needs no extra setup.

**Claude Code** expands `PLANDILLO_URL` in the plugin's MCP address. To use a self-hosted board, set it to that board's `/mcp` URL before you start Claude Code:

```sh
PLANDILLO_URL=https://plans.example.com/mcp claude
```

To keep the address for every session, add `export PLANDILLO_URL=https://plans.example.com/mcp` to your shell profile. Remove the variable to return to the local board. An empty value counts as set and breaks the address. For Code Mode, use the `/mcp/code` URL instead.

**Codex** does not expand variables in a plugin's MCP address, so its plugin always uses the local board. For a self-hosted board, open that board's **Integrations → Agent setup** and run **Skills + MCP for this board** for Codex.

Plugin 1.1.0 changed the default address from `https://app.plandillo.dev/mcp` to the local board. If you relied on the old default, set `PLANDILLO_URL` to your board's address.

### What the agent loads

| Skill | When the agent uses it |
|---|---|
| [plandillo](../skills/plandillo/SKILL.md) | Read the board or track a piece of work from its checklist to verified completion. The server also sends this workflow as MCP instructions. |
| [plandillo-reference](../skills/plandillo-reference/SKILL.md) | Choose among the 19 MCP tools, construct arguments, resolve a conflict or recover from an uncertain write. |
| [plandillo-setup](../skills/plandillo-setup/SKILL.md) | Connect a client, install the skills, enable optional session reminders or diagnose missing tools. |

**Manual installation.** The Integrations tab offers Copy and Download for each SKILL.md. Put each file under its skill name in `~/.claude/skills/` or `~/.agents/skills/` (Codex). The server also serves them at `/api/mcp/skill/<name>` and its shell installer at `/api/mcp/install.sh`. For authenticated self-hosted downloads use the signed-in UI.

**Optional hooks.** The setup skill and Integrations tab provide the same SessionStart and Stop configuration. Install it separately if you want a reminder before the agent finishes a turn.

## Tools

There are 19 tools. Tasks are referred to by ID (`TRK-07`) in responses, `project` accepts a key, a slug or an id, and a
status is given by its key.

| Read | Write |
|---|---|
| `list_projects`, `get_board`, `get_today`, `get_task`, `search`, `list_pages`, `read_page`, `read_html` | `create_project`, `create_tasks` (up to 100 at a time, epics with subtasks via `ref`/`parent_ref`, idempotent by `external_id`), `update_task`, `move_tasks`, `add_note`, `report_issue`, `publish_html`, `write_page`, `append_to_page`, `carry_over`, `delete_tasks` |

- `delete_tasks` is **off** by default. It only moves tasks to the trash; the owner empties the trash in the UI.
- Wiki: `write_page` creates a page or replaces its whole body (pass the `updated_at` from `read_page` as `expected_updated_at`;
  a newer edit by the owner returns `conflict`), `append_to_page` adds to the end. There is no delete tool: the owner deletes
  pages in the UI.
- Read-only mode keeps only the read tools; writes return `forbidden`. A disabled tool is hidden from `tools/list`, and
  calling it by name returns `tool_disabled`.
- Every call is logged (tool, short arguments, duration, result). The log is kept for 7 days.
- An unknown area on a task creates a project label automatically. `create_tasks` and `update_task` accept values for custom
  fields by name (`custom`).
