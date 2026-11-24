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

Open **Integrations** to get the MCP server URL and bearer token for your agent. Choose **Other client** for the settings you can use with any client that supports Streamable HTTP and bearer authentication.

Try asking *“Add a task to test the mobile menu tomorrow. Give it a checklist for touch and keyboard.”* Open the card to see the plan. After testing, ask your agent to record the results on that same card. [Agent setup and examples](docs/agents.md)

## Documentation

- [Using the planner](docs/planner.md) — plan your day, organize a project board, and keep notes and wiki pages alongside tasks.
- [Installation and hosting](docs/hosting.md) — Docker, native binaries, configuration, updates and troubleshooting.

### Self-host

For a shared installation, each person gets a private workspace and an MCP token. The [self-hosting guide](docs/hosting.md#self-host) covers invitations, HTTPS, SQLite or PostgreSQL, and backups. For the full planner on Vercel, see the [Vercel deployment guide](docs/vercel.md).

## Development

The planner and its public website share this repository. Use the [development guide](docs/development/README.md) to build the app, run tests and find your way around the code. The [REST API reference](docs/development/rest-api.md) lists the HTTP endpoints. The [landing page guide](landing/README.md) covers the site for `plandillo.dev`.

Plandillo is maintained by [Huntly Digital](https://github.com/huntly-digital) and released under the [MIT License](LICENSE). To take part, read the [contributing guide](CONTRIBUTING.md) and [Code of Conduct](CODE_OF_CONDUCT.md). To report a vulnerability, follow the [security policy](SECURITY.md).
