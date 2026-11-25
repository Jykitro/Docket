# Run the self-hosted planner on Vercel

Import this repository as one Vercel project with its **Root Directory set to the repository root**. The landing and planner share a deployment. Attach `plandillo.dev` and `app.plandillo.dev` to the project. Redirect `www.plandillo.dev` to the apex. Host rules for the marketing site precede the planner path rules; Vercel aliases and local development still serve the planner. See the [service configuration](../vercel.json).

| Service | Public paths | Internal calls |
| --- | --- | --- |
| `landing` | All paths on `plandillo.dev` | None |
| `web` | `/`, planner pages and static assets | The browser uses same-origin REST and SSE |
| `app` | `/api`, `/mcp`, their descendants, and `/healthz` | Sends Code Mode jobs to `worker` through `PLANDILLO_RUNNER_URL` |
| `worker` | Internal only | Retrieves each authorized tool result through `PLANDILLO_REPLIES_URL` |
| `replies` | Internal only | Reads a single job result from PostgreSQL using its short-lived capability |

Vercel requires an acyclic service graph. The runtime calls are `app → worker → replies`. Vercel supplies both binding URLs at runtime. Keep them out of the project's environment settings. The worker's HTTP supervisor starts a fresh WebAssembly instance for each evaluation. The guest receives protocol frames over stdin and stdout, with a 128 MiB memory limit and a 30-second deadline. Only the app can execute MCP tools against a workspace. [Vercel service bindings](https://vercel.com/docs/services/bindings)

## Project settings

Use an exact public HTTPS hostname for each environment. Give previews and development their own dedicated databases and secrets. All app replicas of one installation share the same database and secrets. Configure the following project environment variables before the first deployment.

| Variable | Value |
| --- | --- |
| `PLANDILLO_DEMO_STORAGE_MB` | Optional positive integer in decimal MB. Set to `5` for the hosted demo. Applies across all workspaces of each non-administrator account on Vercel. |
| `PORT` | Set to `8080` for the cloud deployment so the non-root containers listen on an unprivileged port. |
| `DOCKET_MODE` | `selfhost` |
| `DOCKET_POSTGRES_DSN` | A dedicated PostgreSQL database URL with TLS. The role must own the database or have permission to create and manage its schemas. Use a direct or session-pooled connection that preserves the connection's `search_path`. With Vercel's Neon integration, leave this unset so Plandillo reads the integration-managed `DATABASE_URL_UNPOOLED` directly. |
| `DOCKET_ALLOWED_HOSTS` | Comma-separated exact hostnames of the planner, without scheme or port |
| `DOCKET_PASSKEY_ORIGIN` | Optional exact HTTPS origin for WebAuthn, such as `https://planner.example.com` |
| `PLANDILLO_TOTP_KEY` | Shared base64-encoded 32-byte key for authenticator enrollment. Generate once with `openssl rand -base64 32` and preserve for restores. |
| `PLANDILLO_SETUP_CODE` | Private first-admin setup code, at least 32 characters on a fresh database. Remove it from settings after setup and redeploy. |
| `PLANDILLO_RUNNER_SECRET` | Separate random secret of at least 32 characters for app-to-worker authentication. Keep the same value across replicas in the same deployment. |

Generate each secret separately, e.g. with `openssl rand -hex 32` on a trusted terminal. Open the planner at `/` and enter the setup code on the first-admin screen. Registration starts in invite-only mode. The existing administrator and workspace data survive restarts. Enable open signup in the administrator interface if desired. The readiness check there calls the isolated worker.

Choose a Vercel function region near PostgreSQL. Allow at least 60 seconds of function duration. The API and replies images pin their respective commands in their Docker `ENTRYPOINT`. Each container reads the platform's `PORT`. The platform default without a project override is port 80. The normal Docker and Compose entrypoints continue to use their existing local settings. [Vercel container runtime](https://vercel.com/docs/functions/runtimes/container)

## Replicas and storage

Run only the Vercel app command against this deployment's database. It coordinates account transitions, token changes, passkey challenges, login limits and short-lived Code Mode jobs in PostgreSQL. Each profile write and its live event commit in the same transaction. Browser streams recheck the session before sending each batch, reconnect periodically, and refetch the board. Plan PostgreSQL connection capacity for up to eight shared account and workspace connections and up to 16 coordination connections per app replica. Vercel shares project environment variables between services. The trusted worker supervisor uses no database client and exposes none of its environment to the guest.

Back up the **whole dedicated database**, including the shared `plandillo_auth` schema, with your provider's backup and PITR tools. Restore a snapshot to a separate database and verify it before switching environments. The local SQLite installation and existing Compose deployment remain separate installations. [Storage and backup guide](hosting.md#postgresql-storage)

The container ingress accepts request bodies up to Vercel's function limit. Large HTML publication or MCP requests can hit that limit before reaching Plandillo. Vercel also limits buffered responses, including large exports. Code Mode tool replies and events stream back. Monitor API 5xx errors, PostgreSQL connection usage, Code Mode latency and the rate of completed tool calls after rollout. [Function limits](https://vercel.com/docs/functions/limitations) · [Service usage and billing](https://vercel.com/docs/services/pricing)

## Verify locally

Install the Vercel CLI, Bun and Docker. Export the project variables above in your shell using a **disposable development database**. For Docker Desktop on macOS, a database listening on the host uses `host.docker.internal` in the DSN. Use `localhost,127.0.0.1` for the allowed hosts and `http://localhost:3000` as the development passkey origin. Then run this from the repository root.

```sh
vercel dev --local --listen 127.0.0.1:3000
```

Open `http://localhost:3000/` and complete setup. Check `/healthz`, sign in and reload `/integrations` directly. Connect an MCP client with the profile token. Run `list_projects` on `/mcp`, then run `return await codemode.list_projects({})` on `/mcp/code`. Matching results exercise both internal bindings. In development Docker containers reach the host-published ports through the Docker host gateway.

Run `make check`. Set `DOCKET_TEST_POSTGRES_DSN` to a disposable PostgreSQL server where the test role has `CREATEDB`, then run `make test-postgres`. The suite creates separate test databases and exercises cross-replica session revocation, token rotation, live events, passkey challenge consumption and isolated execution.

The demo quota covers the uncompressed workspace records, including icons, task activity, HTML revisions and trash. Emptying the trash frees space. Database indexes, cleanup metadata and operational MCP logs are outside the meter. The server checks each content write before commit. Administrators retain the normal self-host limits. Check Settings for current usage.
