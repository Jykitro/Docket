<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="web/public/plandillo-inverse.svg">
    <img src="web/public/plandillo.svg" alt="Plandillo" width="290">
  </picture>
</p>

<h3 align="center">A task board for you and your AI agent.</h3>

<p align="center">
  Plan in the browser. Claude Code, Codex or any MCP client picks up a task, ticks its checklist<br>
  and leaves a note with what it verified, and you watch it happen live on the same board.
</p>

<p align="center">
  <a href="https://github.com/huntly-digital/plandillo/actions/workflows/ci.yml"><img src="https://github.com/huntly-digital/plandillo/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/huntly-digital/plandillo/releases"><img src="https://img.shields.io/github/v/release/huntly-digital/plandillo" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/huntly-digital/plandillo" alt="License: MIT"></a>
</p>

<p align="center">
  <a href="https://plandillo.dev">Website</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="docs/agents.md">Agent setup</a> ·
  <a href="docs/hosting.md">Self-hosting</a>
</p>

<p align="center">
  <img src="docs/images/board-light.png" alt="A Plandillo project board with tasks grouped by status" width="820">
</p>

## Why Plandillo

- **One board for both of you.** You plan in the browser; your agent reads and updates the same tasks through MCP or Code Mode. Its changes appear on your screen as it makes them.
- **Done means checked.** Each task carries the checklist you will check the work against. The agent ticks what it verified and records how before it marks the task done.
- **A wiki the agent keeps.** Every project has a wiki next to its board, so the next task starts with what the agent already learned instead of from scratch.
- **Your data, your machine.** One Docker container, SQLite in a folder you choose, no account. Self-host it for a team with private workspaces and PostgreSQL.
- **Light to run.** Written in Go with the React UI embedded in one binary. Fits on a small VPS or a home server.

## How it works

1. **Run it.** One `docker run` command, then open <http://localhost:7777>.
2. **Connect your agent.** Copy the setup command from **Integrations → Agent setup** for Claude Code or Codex, or add the MCP endpoint to any Streamable HTTP client.
3. **Hand over the work.** Ask *“Take the next task in my project”*. The agent plans, works, reports issues it finds and closes the task with notes you can read.

<table>
  <tr>
    <td width="50%"><img src="docs/images/task-light.png" alt="A task with an acceptance checklist and a note left by Claude"></td>
    <td width="50%"><img src="docs/images/mcp-dark.png" alt="The Integrations page with the MCP self-check and the call log of Claude and Codex"></td>
  </tr>
  <tr>
    <td align="center">A task with its acceptance checklist and the agent's note</td>
    <td align="center">MCP self-check and a live log of every agent call</td>
  </tr>
</table>

## Quick start

With Docker installed, run these commands from the directory where you want to keep your tasks.

```sh
mkdir -p -m 700 data
docker run -d --name plandillo --user "$(id -u):$(id -g)" --pull=always \
  -p 127.0.0.1:7777:7777 -v "$PWD/data:/data" --restart unless-stopped \
  ghcr.io/huntly-digital/plandillo:main
```

Open <http://localhost:7777>, enter your name and create your first project. The default setup stores your board in SQLite under `./data`. The interface is available in English, Russian and Ukrainian. Native binaries and other options are in the [installation guide](docs/hosting.md).

## Connecting agents (MCP)

### Install skills and connect your board

Open **Integrations → Agent setup**, select **Codex** or **Claude Code**, and choose **Skills + MCP for this board**. Copy the command into a terminal on macOS or Linux. It installs the three skills and registers the selected endpoint in that client. Choose your workspace when the browser opens and approve access. If sign-in does not start automatically, run the login command printed by the installer. The client refreshes its own credentials.

For the default local board, the Codex command is:

```sh
curl -fsSL http://localhost:7777/api/mcp/install.sh | PLANDILLO_CLIENT=codex PLANDILLO_URL=http://localhost:7777/mcp sh
```

Use `PLANDILLO_CLIENT=claude-code` for Claude Code. For self-hosting or Code Mode, copy the command from your running board so it uses the right URL. **MCP only** shows the configuration for clients that already have the skills, including Claude Desktop and other Streamable HTTP clients.

<details>
<summary><strong>Install a plugin from GitHub</strong></summary>

The native plugin bundles the three skills and an MCP connection to a local board at `http://localhost:7777/mcp`. For a self-hosted board, start Claude Code with `PLANDILLO_URL=https://your-host/mcp`; in Codex, use the board installer above, since Codex keeps the plugin's address fixed. Details are in [Board address for the plugins](docs/agents.md#board-address-for-the-plugins). The public GitHub repository serves as the marketplace for personal and team installations. Choose the commands for your client.

**Codex**

```sh
codex plugin marketplace add huntly-digital/plandillo
codex plugin add plandillo@huntly-digital
```

**Claude Code**

```sh
claude plugin marketplace add huntly-digital/plandillo
claude plugin install plandillo@huntly-digital
```

Start a new client session, select the Plandillo connection in the MCP settings and approve access in your browser. Ask *“Which projects are in my workspace?”* to verify the connection.

</details>

<details>
<summary><strong>Install just the skills</strong></summary>

Run this in the project directory with Node.js installed. It downloads the workflow, tool reference and setup guide directly from the public GitHub repository. You can specify either agent or both.

```sh
npx skills add huntly-digital/plandillo --skill plandillo plandillo-reference plandillo-setup --agent codex claude-code
```

You can also download individual files from **Integrations → Download individual skills**. To give the agent access to your tasks, choose **MCP only** in the same screen.

</details>

More examples and the full tool list: [Agent setup](docs/agents.md).

## Documentation

- [Using the planner](docs/planner.md) — plan your day, organize a project board, and keep notes and wiki pages alongside tasks.
- [Installation and hosting](docs/hosting.md) — Docker, native binaries, configuration, updates and troubleshooting.
- [Agent setup](docs/agents.md) — MCP and Code Mode, skills, plugins and the tool reference.

### Self-host

For a shared installation, each account can own several private workspaces, each with its own MCP token. The [self-hosting guide](docs/hosting.md#self-host) covers invitations, HTTPS, SQLite or PostgreSQL, and backups. The landing at plandillo.dev is deployed on Vercel; see the [Vercel guide](docs/vercel.md).

## Development

The planner and its public website share this repository. Use the [development guide](docs/development/README.md) to build the app, run tests and find your way around the code. The [REST API reference](docs/development/rest-api.md) lists the HTTP endpoints. The [landing page guide](landing/README.md) covers the site for `plandillo.dev`.

Plandillo is maintained by [Huntly Digital](https://github.com/huntly-digital) and released under the [MIT License](LICENSE). To take part, read the [contributing guide](CONTRIBUTING.md) and [Code of Conduct](CODE_OF_CONDUCT.md). To report a vulnerability, follow the [security policy](SECURITY.md).
