# Installation and hosting

## Quick start

Install [Docker](https://docs.docker.com/get-docker/) and run the following command from the directory where you want to keep your data.

Start the newest tested build of `main` on Intel/AMD or ARM64:

```bash
mkdir -p -m 700 data
docker run -d --name plandillo --user "$(id -u):$(id -g)" --pull=always -p 127.0.0.1:7777:7777 -v "$PWD/data:/data" --restart unless-stopped ghcr.io/huntly-digital/plandillo:main
```

Open <http://localhost:7777>. The image includes the web UI, API and SQLite. GitHub Actions updates
`:main` after the SQLite and PostgreSQL checks pass. Use `:sha-<full-commit-sha>` to pin a particular main build,
or `:latest` to follow tagged stable releases.

Before updating an older root-owned installation, stop the writer and verify a restorable copy of the complete data directory. Ensure the intended non-root service UID can open the database and create a backup on a disposable copy. Apply the verified ownership changes to the private Plandillo volume before switching images.

To update a container started with the command above, use the **same directory** and run:

```bash
docker pull ghcr.io/huntly-digital/plandillo:main && docker stop plandillo && docker rm plandillo && docker run -d --name plandillo --user "$(id -u):$(id -g)" -p 127.0.0.1:7777:7777 -v "$PWD/data:/data" --restart unless-stopped ghcr.io/huntly-digital/plandillo:main
```

Or download a binary for Linux, macOS or Windows from the [releases page](https://github.com/huntly-digital/plandillo/releases)
and run it. The native binary listens on `127.0.0.1:7777` by default. Point
`DOCKET_DB` and `DOCKET_BACKUP_DIR` at local paths, since they default to `/data/...` (see
[Configuration](#configuration)).

To build from a source checkout:

```bash
make rebuild   # creates .env from .env.example, builds the plandillo:local image and (re)starts the container
```

The same command rebuilds and restarts the container after you change the code; your data in `./data` is kept.
Other targets: `make logs`, `make stop`, `make help`.

Without `make`, run the equivalent commands:

```bash
umask 077
[ -f .env ] || (set -C; cat .env.example > .env)
chmod 600 .env
docker build -t plandillo:local -t docket:local .
mkdir -p -m 700 data
docker run -d --name docket --user "$(id -u):$(id -g)" --env-file .env -p 127.0.0.1:7777:7777 \
  -v "$PWD/data:/data" --restart unless-stopped plandillo:local
```

Open <http://localhost:7777>. Existing local installations can keep using their configured hostname. Health check: `curl localhost:7777/healthz`.

On the first run with an empty database Plandillo asks for your name and a first project, then drops you into the app.

### What to know

- The port is published on `127.0.0.1` only.
- Requests with a foreign `Host` get `403`, and write requests to `/api` with a foreign `Origin` also get `403`
  (protection against DNS rebinding and CSRF).
- Data lives in `./data/docket.db`. A backup (`VACUUM INTO`) is written at startup and once a day to `./data/backups`;
  the last 14 are kept.
- Seed data (optional): if `DOCKET_SEED` points to a JSON file and the database has no projects, it is imported at
  startup (`{"projects": […], "tasks": […]}`: projects by `slug`, tasks by `external_id`, author `claude`). Running it
  again does not create duplicates. No seed file ships with Plandillo.

SQLite creates new database directories with mode 0700 and database, WAL and backup files with owner-only permissions on Unix. Restrict older backup files and their parent directories before moving or restoring them. On Windows, store these files in a directory whose ACL grants access only to the Plandillo account and trusted administrators.

## Configuration

Settings are read from `.env` (see [the example settings](../.env.example)):

| Variable | Default | Description |
|---|---|---|
| `DOCKET_MODE` | `local` | `local`: one user, no login. `selfhost`: several users behind a login, see [Self-host](#self-host) |
| `DOCKET_ADDR` | `127.0.0.1:7777` (native), `:7777` (image) | HTTP listen address |
| `DOCKET_TRUSTED_PROXIES` | empty | Selfhost proxy IPs or CIDRs trusted to append the original client address to X-Forwarded-For |
| `DOCKET_DB` | `/data/docket.db` | SQLite database file (ignored with PostgreSQL) |
| `DOCKET_CODE_SOCKET` | `/run/docket-code/runner.sock` | Private Unix socket shared by the Plandillo server and the Code Mode worker |
| `DOCKET_POSTGRES_DSN` | empty | Optional PostgreSQL connection string, requires `DOCKET_MODE=selfhost`; empty keeps SQLite |
| `DOCKET_BACKUP_DIR` | `/data/backups` | Where backups are written |
| `DOCKET_MCP_TOKEN` | empty | Optional. If set, it takes precedence over the token stored in the database and rotating it in the UI is disabled |
| `DOCKET_ALLOWED_HOSTS` | `localhost,127.0.0.1,docket.localhost` | Allowed `Host` and `Origin` values |
| `DOCKET_SEED` | empty | Optional initial data file |
| `TZ` | `Asia/Ho_Chi_Minh` | Time zone used to decide what "today" is |

## Self-host

`DOCKET_MODE=selfhost` turns Plandillo into a server for several people: everyone logs in with a username and password,
and every profile has its own isolated storage and MCP token. SQLite is the default; PostgreSQL is optional (see below).
`local` mode (the default) works as before.

1. In `.env`, set `DOCKET_MODE=selfhost` and put your real domain in `DOCKET_ALLOWED_HOSTS` (for example
   `planner.example.com`).
2. Put an HTTPS reverse proxy in front of the container and do not publish port `7777` without it. The session cookie
   is marked `Secure` only over HTTPS (or with `X-Forwarded-Proto: https`), so logging in without a proxy is not safe.
3. For SQLite, create the private `./data` directory and export the numeric `DOCKET_UID` and `DOCKET_GID` of its non-root owner as described under [Code Mode worker](#code-mode-worker). For SQLite with Code Mode, run `docker compose -f compose.selfhost.yml up -d --build` with a fresh private data directory mounted at `./data`. The PostgreSQL Compose file also includes the worker. Existing selfhost installations using a standalone binary or container must connect the isolated worker to use Code Mode after upgrading. Ordinary MCP runs in the main server.
4. On the server, retrieve the first administrator's setup code with `docker compose -f compose.selfhost.yml exec docket /app/plandillo setup-code` (use `compose.postgres.yml` for PostgreSQL). For a native installation run `plandillo setup-code` with the server's `DOCKET_DB` setting. Enter it on the first-run screen. Restarting before setup replaces the code. Completing setup removes the private file from the data directory.
5. Save the one-time recovery code. In **Settings → Registration**, choose **Invitation only** (the default on installation and upgrade), **Open** or **Closed**. Open signup uses `/register` and requires an available worker when enabling it. Closing signup pauses unused invitations until reopening or their original expiry. Existing owners can continue signing in. The pilot supports 50 accounts including the administrator and blocked accounts. The [historical pilot report](selfhost-security.md) describes the previous storage layout.
6. Administrators manage signup, invitations and account suspension. Blocking ends sessions and active requests. Unblocking requires a new login and a new MCP token. Owners manage their own data and can delete their own account after confirming their password. The last administrator must remain active. The operator of the host or database has physical access to stored data.

**Passkey sign-in.** In **Settings → Passkeys**, choose **Add a passkey**, name the key and confirm your current password. Follow your device’s prompt. On your next visit, select **Sign in with a passkey** to open the same workspace without typing your username. You can register up to 32 keys per account and revoke a lost key in Settings. Password sign-in remains available. Recovering your account with a recovery code revokes all enrolled passkeys.

Open Plandillo on an HTTPS hostname in `DOCKET_ALLOWED_HOSTS`. For local testing use `http://localhost:<port>`. An IP address cannot serve as the passkey domain. For a custom public port or to select one canonical address, set `DOCKET_PASSKEY_ORIGIN` to the exact browser origin, for example `https://planner.example.com:8443`. Behind a proxy, preserve the Host (including a nonstandard port) and overwrite `X-Forwarded-Proto` with the actual external scheme. After moving to a new hostname, sign in with your password and enroll a key on that hostname.

**Accounts and workspaces.** An account can own several private workspaces. Use the sidebar selector to switch and the workspace settings to create, rename, archive or restore one. Archiving revokes its generated MCP token. Permanent deletion requires an archived workspace, its exact name and, in selfhost mode, the account password. The last active workspace must stay available. Each workspace has a separate Integrations page, bearer and tool policy. The local environment token, when supplied, applies to the original workspace only. Selfhost ignores `DOCKET_MCP_TOKEN` and `DOCKET_SEED`.

**Account security.** Settings → Security lists signed-in devices and lets the owner revoke individual sessions, add passkeys or enable an authenticator. Password sign-in then requires a six-digit TOTP code. Passkeys can sign in independently. Recovery invalidates previous sessions, passkeys, TOTP enrollment and generated workspace tokens.

**Storage and restore.** For a fresh installation, use a new data directory or a dedicated empty PostgreSQL database. An installation with the earlier per-profile layout needs its original binary and storage. The server checks the layout before opening it for application writes and pins each new installation to its chosen mode.

An existing shared SQLite database at format version 2 upgrades to version 3 on first start. The upgrade adds columns for compressed HTML revisions and preserves existing revisions as plain text. Before upgrading, keep a verified SQLite snapshot for use with the previous binary. A rollback to that snapshot restores the data as of the snapshot time. PostgreSQL retains the shared format version 2.

SQLite stores new HTML document revisions of at least 4 KiB as gzip when this saves at least one eighth of the original size. Reading a revision or exporting it returns the original HTML. The workspace publication quota counts the original UTF-8 bytes. Wiki Markdown and inline HTML page bodies remain text. PostgreSQL stores all these fields as `text` and manages large values using [TOAST](https://www.postgresql.org/docs/18/storage-toast.html).

SQLite stores accounts, credentials and every workspace in `DOCKET_DB`. Automatic snapshots in `DOCKET_BACKUP_DIR` contain the entire database. For SQLite selfhost, back up the private `totp.key` beside the database separately, or preserve the environment key if supplied. To test a restore, copy a snapshot to a new private directory as `DOCKET_DB`, supply the original key, start in the original mode on loopback and check sign-in and projects in two workspaces. Keep restored installations private because snapshots contain credentials valid at the time of the backup.

On PostgreSQL, provide all replicas with the same `PLANDILLO_TOTP_KEY` to enable enrollment. Generate it once with `openssl rand -base64 32` and store it in your secret manager alongside the database recovery procedure. Losing this key prevents decryption of enrolled factors.

Limits:

- Login allows five attempts per client and normalized username pair per ten minutes, with additional limits of 20 per client and 20 per username. Password confirmations have a separate limit per owner. At most eight credential checks run concurrently.
- Signup and recovery each allow 20 requests per client per ten minutes. Recovery also limits attempts against the same normalized username. Set a per-client limit at the trusted HTTPS proxy for public installations. Forward the original Host, overwrite `X-Forwarded-Proto`, and configure the proxy to overwrite or correctly append `X-Forwarded-For`. Set `DOCKET_TRUSTED_PROXIES` to its immediate trusted addresses or networks. Confirm two real clients have distinct limits. Exclude invite URLs and credentials from proxy access logs.
- Recover at `/recover` with the username, single-use code and a new password. Save the replacement code and reconnect MCP with its new token. Existing users can generate a code in Settings after confirming their password. A blocked account remains blocked. Keep the code in a password manager because there is no email reset.
- Account deletion removes the live workspace. Separately retained copies follow the operator's backup retention policy.

| Selfhost endpoint | Purpose |
|---|---|
| `GET /api/auth/registration`, `PUT /api/admin/registration` | Read the mode or set `{"mode":"invite"}` as administrator. Allowed values are `open`, `invite` and `closed` |
| `POST /api/auth/register`, `POST /api/auth/invite/{token}` | Create a private workspace with username and password. Returns the user, a session cookie and a one-time `recovery_code` |
| `POST /api/auth/passkeys/register/begin`, `POST /api/auth/passkeys/register/finish` | Enroll a discoverable key from a signed-in browser. Begin with `name` and `password`, finish with the browser credential response |
| `POST /api/auth/passkeys/login/begin`, `POST /api/auth/passkeys/login/finish` | Request sign-in options, then submit the signed response to open the owner's session. Both ceremonies use the same browser, exact origin and a five-minute single-use challenge |
| `GET /api/auth/passkeys`, `DELETE /api/auth/passkeys/{id}` | List your key names and IDs or revoke one with your current `password` |
| `POST /api/auth/recovery-code`, `POST /api/auth/recover` | Issue a code with the signed-in owner's password, or reset using username, code and new password (`password`). Both return `recovery_code` |
| `PUT /api/admin/users/{id}/blocked` | Set `{"blocked":true}` or `false` as administrator |
| `DELETE /api/auth/account` | Delete the signed-in owner's account with `{"password":"…"}` |

### Project icons and installed app

Upload a PNG, JPEG, WebP or SVG (up to 2 MiB) when creating a project or in its general settings. A transparent 128 × 128 PNG travels with the project in backups and JSON exports. Select any built-in glyph to remove the upload.

The project create and patch endpoints accept an optional `icon_png` containing a base64-encoded 128 × 128 PNG (up to 128 KiB decoded). Project responses include an `icon_url` for custom icons. Read the PNG with `GET /api/projects/{id}/icon` in the same profile. Patching only `icon` switches to the built-in glyph.

Open Plandillo over HTTPS or on localhost, then choose **Install Plandillo** in settings when the browser offers installation. On iPhone or iPad choose **Share → Add to Home Screen**; in Safari on macOS use **File → Add to Dock**. Start the installed app to open Today in its own window. Tasks require a connection. If the connection drops, Plandillo offers a retry and reopens the same URL when the server returns. The service worker stores only the generic offline page.

### PostgreSQL storage

Set both variables to use an existing, **dedicated** PostgreSQL database:

```dotenv
DOCKET_MODE=selfhost
DOCKET_POSTGRES_DSN=postgres://docket:password@db.example.com:5432/docket?sslmode=verify-full
```

The database must exist and the Plandillo role must own it (or have `CONNECT` and `CREATE` on it). Superuser and
`CREATEDB` permissions are not needed by the application. Use a private network or verified TLS for remote connections,
and URL-encode special characters in credentials. The connection string is not exposed by the API or logged.

Plandillo creates the shared schema `plandillo_auth` for accounts, sessions, workspaces and planner content. Each workspace belongs to one account. Queries select the workspace and composite foreign keys keep related content together. Account deletion cascades to its workspaces. Restrict the application's database credential to its dedicated database.

For a new installation with Docker Compose:

```bash
umask 077
[ -f .env ] || (set -C; cat .env.example > .env)
chmod 600 .env
# For a NEW installation, append two independent random passwords without printing them.
# Skip these two lines if the file already defines the passwords.
(printf 'POSTGRES_ADMIN_PASSWORD='; openssl rand -hex 32) >> .env
(printf 'POSTGRES_PASSWORD='; openssl rand -hex 32) >> .env
docker compose -f compose.postgres.yml up -d --build
docker compose -f compose.postgres.yml exec docket /app/plandillo setup-code
```

[`compose.postgres.yml`](../compose.postgres.yml) builds the current source and starts PostgreSQL 18.7 with a persistent
volume; PostgreSQL has no published host port. Plandillo binds to `127.0.0.1:7777`; use the same HTTPS reverse-proxy and
allowed-host setup described above for remote access. `sslmode=disable` in this example applies only to the private
Compose network. The password in this example must be hex/alphanumeric so it is valid in the connection URL.

The bootstrap account initializes the cluster. The application receives a separate login (`docket`) with permission to create schemas in its dedicated database. For a fresh installation, use an empty dedicated volume. Restore only snapshots created with the same application storage layout. Keep the previous volume and its matching binary available for rollback.

Operational details:

- `DOCKET_DB` and `DOCKET_BACKUP_DIR` are ignored by PostgreSQL storage; no local SQLite files are created.
- The bundled Alpine image applies the security updates for libxml2 and OpenSSL and uses Alpine’s su-exec to start PostgreSQL as its service user.
- On a **fresh volume**, set `DOCKET_POSTGRES_IMAGE=postgres:19.0-alpine` to use the major version exercised in CI.
  Existing Compose volumes default to 18.7. To upgrade from 18 to 19, stop the Plandillo writer, take a final
  `pg_dump -Fc`, start version 19 with a separate empty volume, then restore with `pg_restore --exit-on-error`
  into the dedicated database using the same owner. Verify login, projects and tasks before switching traffic.
  Keep the old volume and image for rollback before allowing new writes. An image-tag change alone does not migrate data.
- **Back up PostgreSQL separately** with `pg_dump` or your provider's backups/PITR. Plandillo does not run `VACUUM INTO`
  or claim to have created a PostgreSQL backup. MCP call-log retention still runs. For the Compose example, create
  a full logical backup (including accounts and every profile) with:

  ```bash
  (
    set -eu
    umask 077
    mkdir -p -m 700 "$HOME/docket-backups"
    chmod 700 "$HOME/docket-backups"
    dump=$(mktemp "$HOME/docket-backups/docket.XXXXXXXX")
    if docker compose -f compose.postgres.yml exec -T postgres pg_dump -U docket -d docket -Fc > "$dump"; then
      mv "$dump" "$dump.dump"
    else
      rm -f "$dump"
      exit 1
    fi
  )
  ```

  Check older dumps and environment files for group or public access and restrict them to the owner. Keep the backup directory on a private, access-controlled filesystem. Store dumps securely outside the database volume and test restoration into a separate empty database. Do not run
  `docker compose down -v` on an installation you intend to keep: it deletes the PostgreSQL volume.
- Existing SQLite accounts and tasks are **not imported**. Selecting PostgreSQL starts a separate installation.
  To return to the previous SQLite installation, remove `DOCKET_POSTGRES_DSN` and restart with the original data volume.
  Changes made in PostgreSQL do not appear in SQLite. There is no automatic conversion in either direction.
- Back up and restore the entire database and its TOTP key together.
- Native and Compose installations use one Plandillo instance per database. Account and workspace operations share a pool of up to eight PostgreSQL connections. The [Vercel deployment](vercel.md) coordinates multiple replicas.
- The database self-check verifies connectivity and reports storage size. It does not perform a full
  PostgreSQL integrity check or verify external backups. Full-text search uses PostgreSQL's `simple` dictionary with
  prefix matching for Russian, Ukrainian and English; ranking/snippets can differ from SQLite FTS5.

## Code Mode worker

The image runs as UID/GID 65532. For a bind-mounted SQLite directory, use the non-root UID and GID that own the data. For the SQLite Compose files, prepare the directory and set the numeric identities in the shell:
```bash
mkdir -p -m 700 data
export DOCKET_UID="$(id -u)" DOCKET_GID="$(id -g)"
```
Before upgrading root-owned volumes, stop the writer and verify a restorable snapshot, then grant the intended non-root identity ownership of the private Plandillo data and backup directories. Check that a disposable copy opens and a backup completes before switching the live volume.

For single-user Docker installations, run `docker compose -f compose.local.yml up -d --build` from the source checkout to start Plandillo together with the isolated worker. For selfhost, use either supplied selfhost Compose file. A standalone binary or single-container launch can serve classic MCP directly. Code Mode requires a worker sharing the configured Unix socket, and returns `unavailable` if that worker is offline.

## Troubleshooting

- **`/mcp` returns 401.** On first connection this starts OAuth discovery. Use the client’s MCP login, select the workspace and approve access. Forward `/.well-known/oauth-*`, `/oauth/*` and `/api/*` under the same public HTTPS origin when using a reverse proxy. Local single-user approval requires localhost or a loopback IP. For a bearer-only client, set `Authorization: Bearer <TOKEN>` using the manual token in Integrations. The **MCP**
  page itself opens in a browser without a token: requests with `Accept: text/html` get the UI.
- **403 `forbidden_host`.** Add the host name to `DOCKET_ALLOWED_HOSTS` and restart the container.
- **The self-check is red.** Each row on the Integrations tab explains what to do. For the Docker quick start, run `docker logs plandillo`. For a source installation started with `make rebuild`, run `make logs`.
- **Start with an empty local board.** Stop your existing container and start Plandillo with a new, empty data directory. Keep the original directory if you want to return to that board.
