# Plandillo

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="web/public/plandillo-inverse.svg">
  <img src="web/public/plandillo.svg" alt="Plandillo" width="290">
</picture>

[![CI](https://github.com/huntly-digital/plandillo/actions/workflows/ci.yml/badge.svg)](https://github.com/huntly-digital/plandillo/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/huntly-digital/plandillo)](https://github.com/huntly-digital/plandillo/releases)
[![License: MIT](https://img.shields.io/github/license/huntly-digital/plandillo)](LICENSE)

Plandillo is an open-source task planner for you and your AI agent. Plan your day in the browser, then ask your agent to pick up a task, update its checklist or leave a note with what it verified. You see its changes live on the same board.

<p align="center">
  <img src="docs/images/board-light.png" alt="A Plandillo project board with tasks grouped by status" width="820">
</p>

## Quick start

With Docker installed, run these commands from the directory where you want to keep your tasks.

```sh
mkdir -p -m 700 data
docker run -d --name plandillo --user "$(id -u):$(id -g)" --pull=always \
  -p 127.0.0.1:7777:7777 -v "$PWD/data:/data" --restart unless-stopped \
  ghcr.io/huntly-digital/plandillo:main
```

Open <http://localhost:7777>, enter your name and create your first project. The default setup stores your board in SQLite under `./data`. The interface is available in English, Russian and Ukrainian.

## Connecting agents (MCP)

### Install skills and connect your board

Open **Integrations → Agent setup**, select **Codex** or **Claude Code**, and choose **Skills + MCP for this board**. Copy the command into a terminal on macOS or Linux. It installs the three skills and registers the selected endpoint in that client. Choose your workspace when the browser opens and approve access. If sign-in does not start automatically, run the login command printed by the installer. The client refreshes its own credentials.

For the default local board, the Codex command is:

```sh
curl -fsSL http://localhost:7777/api/mcp/install.sh | PLANDILLO_CLIENT=codex PLANDILLO_URL=http://localhost:7777/mcp sh
```

Use `PLANDILLO_CLIENT=claude-code` for Claude Code. For self-hosting or Code Mode, copy the command from your running board so it uses the right URL. **MCP only** shows the configuration for clients that already have the skills, including Claude Desktop and other Streamable HTTP clients.

### Install a plugin from GitHub

For the hosted board at `https://app.plandillo.dev/mcp`, the native plugin bundles the three skills and the MCP connection. The public GitHub repository serves as the marketplace for personal and team installations. Choose the commands for your client.

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

### Install just the skills

Run this in the project directory with Node.js installed. It downloads the workflow, tool reference and setup guide directly from the public GitHub repository. You can specify either agent or both.

```sh
npx skills add huntly-digital/plandillo --skill plandillo plandillo-reference plandillo-setup --agent codex claude-code
```

You can also download individual files from **Integrations → Download individual skills**. To give the agent access to your tasks, choose **MCP only** in the same screen. [Agent setup and examples](docs/agents.md)

## Documentation

- [Using the planner](docs/planner.md) — plan your day, organize a project board, and keep notes and wiki pages alongside tasks.
- [Installation and hosting](docs/hosting.md) — Docker, native binaries, configuration, updates and troubleshooting.

### Self-host

For a shared installation, each account can own several private workspaces, each with its own MCP token. The [self-hosting guide](docs/hosting.md#self-host) covers invitations, HTTPS, SQLite or PostgreSQL, and backups. For the full planner on Vercel, see the [Vercel deployment guide](docs/vercel.md).

## Development

The planner and its public website share this repository. Use the [development guide](docs/development/README.md) to build the app, run tests and find your way around the code. The [REST API reference](docs/development/rest-api.md) lists the HTTP endpoints. The [landing page guide](landing/README.md) covers the site for `plandillo.dev`.

Plandillo is maintained by [Huntly Digital](https://github.com/huntly-digital) and released under the [MIT License](LICENSE). To take part, read the [contributing guide](CONTRIBUTING.md) and [Code of Conduct](CODE_OF_CONDUCT.md). To report a vulnerability, follow the [security policy](SECURITY.md).
