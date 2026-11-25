# Landing on Vercel

Vercel hosts only the public site at `plandillo.dev`, the static Astro project in [`landing/`](../landing/README.md). The planner is not deployed on Vercel. Run it like any other self-hosted installation, as described in the [hosting guide](hosting.md), and point `app.plandillo.dev` at that server ([Hosted planner](#hosted-planner)).

## Project settings

Import the repository as one Vercel project and set its **Root Directory** to `landing`. Vercel then sees a plain Astro project, and the repository holds no Vercel configuration:

| Setting | Value |
| --- | --- |
| Root Directory | `landing` |
| Framework preset | Astro. Vercel detects `landing/bun.lock`, installs with Bun and runs `astro build`. |
| Output directory | `dist` (the preset default) |
| Include files outside the root directory | Off. The landing uses nothing outside `landing/`. |
| Skip deployments when there are no changes to the root directory | On |
| Environment variable | `ASTRO_TELEMETRY_DISABLED=1`, the same as `make -C landing build` sets locally |

Attach `plandillo.dev` as the production domain and redirect `www.plandillo.dev` to it. Do not attach `app.plandillo.dev` to this project: it would answer the planner's address with the landing. Remove any environment variables left over from the old app deployment.

## When it builds

With **Skip deployments** on, Vercel builds only when a push changes `landing/`. GitHub Actions works the other way round: [`ci.yml`](../.github/workflows/ci.yml) ignores pushes that touch only `landing/`, so a landing-only change runs no app tests and publishes no Docker image.

## Preview locally

```sh
make -C landing serve
```

Open <http://127.0.0.1:4174/>. `make -C landing build` writes the same static site that Vercel builds to `landing/dist/`. The [landing README](../landing/README.md) covers editing.

## Hosted planner

The planner at `app.plandillo.dev` runs on a separate server with PostgreSQL storage. Follow [Self-host](hosting.md#self-host) and [PostgreSQL storage](hosting.md#postgresql-storage) in the hosting guide, behind an HTTPS reverse proxy for `app.plandillo.dev`.

When moving the existing hosted installation, keep what its accounts depend on:

| Variable | Value |
| --- | --- |
| `DOCKET_POSTGRES_DSN` | The production connection to the existing Neon database **`plandillo`**, with its TLS options. Check the database name in the connection string. |
| `DOCKET_ALLOWED_HOSTS` | `app.plandillo.dev` |
| `DOCKET_PASSKEY_ORIGIN` | `https://app.plandillo.dev`. Changing it invalidates registered passkeys. |
| `PLANDILLO_TOTP_KEY` | The existing key, if authenticator sign-in was enabled. Keep it with the database recovery material. |

Run only one backend against the live database. Back up PostgreSQL through Neon or `pg_dump`: Plandillo makes no PostgreSQL backups itself.

Check the switch from outside. `https://app.plandillo.dev/healthz` answers `ok`, `/.well-known/oauth-authorization-server` reports the issuer `https://app.plandillo.dev`, and a signed-out `GET /api/auth/me` returns 401. A setup prompt instead means the server is connected to an empty database. Then sign in, open a workspace, and have an agent list projects over MCP.
