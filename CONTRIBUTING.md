# Contributing

Thanks for helping. Bug reports, small fixes and ideas are all welcome; for anything bigger, open an issue or a
discussion first so we agree on the direction before you write code.

## Setup

You need Go (version in `go.mod`), [Bun](https://bun.sh) and `make`.

```bash
make install   # frontend dependencies
make check     # typecheck + Knip + Go tests; run it before you open a PR
make dev-api   # backend on :7777
make dev-web   # Vite with hot reload (second terminal)
```

Go tests open a port on localhost. `make help` lists every target.

## Rules of the repo

The full list is in [AGENTS.md](AGENTS.md); the short version:

- **Layers.** `httpapi` and `mcpserver` call only `service`; SQL lives only in `store`.
- **Tests** sit next to the code (`*_test.go`) and change in the same commit. A bug fix gets a test that fails without it.
- **UI text** is Russian, English and Ukrainian: the Russian source is the key in `tr('…')`, the other two go into
  `web/src/messages.ts`. Add all three together.
- **No outside hosts** from the UI: no CDNs, analytics or remote fonts or images.
- **Security.** Do not weaken the bearer-token check on `/mcp` or the Host/Origin guard.
- **Data.** Never commit `.env`, `data/` or a database, and no personal data.
- **MCP tools.** A new tool needs a `service.MCPTools` entry, a handler, the README table and
  `skills/docket-reference/SKILL.md`.

## Commits and pull requests

Conventional style: `type(scope): lowercase subject`, with type `feat`, `fix`, `docs`, `test`, `refactor` or `chore`
and scope such as `web`, `selfhost`, `auth` or `mcp`. One logical change per commit, no trailing period.
Release notes are generated from these subjects, so make them readable.

## License

By contributing you agree that your work is released under the [MIT License](LICENSE).
