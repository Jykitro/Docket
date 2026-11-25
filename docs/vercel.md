# Landing on Vercel

Vercel hosts only the public site at `plandillo.dev`, the static Astro project in [`landing/`](../landing/README.md). The planner is not deployed on Vercel. Run it like any other self-hosted installation, as described in the [hosting guide](hosting.md), and point `app.plandillo.dev` at that server ([Hosted planner](#hosted-planner)).

## Project settings

Import the repository as one Vercel project with its **Root Directory set to the repository root**. [`vercel.json`](../vercel.json) holds the build settings, so the dashboard needs no overrides:

| Setting | Value |
| --- | --- |
| Framework preset | None (`"framework": null`) |
| Build command | `make -C landing build`: installs the dependencies from `landing/bun.lock` with Bun, then runs `astro build` |
| Output directory | `landing/dist` |

Attach `plandillo.dev` as the production domain and redirect `www.plandillo.dev` to it. Do not attach `app.plandillo.dev` to this project: it would answer the planner's address with the landing. The landing needs no environment variables, so remove any left over from the old app deployment.

## When it builds

The `ignoreCommand` in `vercel.json` skips a deployment unless `landing/`, `vercel.json` or `.vercelignore` changed since the last successful deployment (`VERCEL_GIT_PREVIOUS_SHA`, or the previous commit when there is none). The paths start with `:/`, so they count from the repository root whatever directory the command runs in. If Git cannot compare the two commits, the command fails and Vercel builds anyway.

GitHub Actions works the other way round: [`ci.yml`](../.github/workflows/ci.yml) ignores pushes that touch only those files, so a landing-only change runs no app tests and publishes no Docker image.

## Preview locally

```sh
make -C landing serve
```

Open <http://127.0.0.1:4174/>. `make -C landing build` writes the same static site that Vercel deploys to `landing/dist/`. The [landing README](../landing/README.md) covers editing.

## Hosted planner

The planner at `app.plandillo.dev` runs on a separate server with PostgreSQL storage. Follow [Self-host](hosting.md#self-host) and [PostgreSQL storage](hosting.md#postgresql-storage) in the hosting guide, behind an HTTPS reverse proxy for `app.plandillo.dev`. The landing's **Sign in** and **Try the demo** buttons open `https://app.plandillo.dev/` and `https://app.plandillo.dev/register`.

When moving the existing hosted installation, keep what its accounts depend on:

| Variable | Value |
| --- | --- |
| `DOCKET_POSTGRES_DSN` | The production connection to the existing Neon database **`plandillo`**, with its TLS options. Check the database name in the connection string. |
| `DOCKET_ALLOWED_HOSTS` | `app.plandillo.dev` |
| `DOCKET_PASSKEY_ORIGIN` | `https://app.plandillo.dev`. Changing it invalidates registered passkeys. |
| `PLANDILLO_TOTP_KEY` | The existing key, if authenticator sign-in was enabled. Keep it with the database recovery material. |
| `PLANDILLO_DEMO_STORAGE_MB` | The demo quota for non-administrator accounts, for example `5`. |

Run only one backend against the live database. Back up PostgreSQL through Neon or `pg_dump`: Plandillo makes no PostgreSQL backups itself.

Check the switch from outside. `https://app.plandillo.dev/healthz` answers `ok`, `/.well-known/oauth-authorization-server` reports the issuer `https://app.plandillo.dev`, and a signed-out `GET /api/auth/me` returns 401. A setup prompt instead means the server is connected to an empty database. Then sign in, open a workspace, and have an agent list projects over MCP.
