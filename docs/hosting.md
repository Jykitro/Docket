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
`PLANDILLO_DB` and `PLANDILLO_BACKUP_DIR` at local paths, since they default to `/data/...` (see
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
docker build -t plandillo:local .
mkdir -p -m 700 data
docker run -d --name plandillo --user "$(id -u):$(id -g)" --env-file .env -p 127.0.0.1:7777:7777 \
  -v "$PWD/data:/data" --restart unless-stopped plandillo:local
```

Open <http://localhost:7777>. Existing local installations can keep using their configured hostname. Health check: `curl localhost:7777/healthz`.

On the first run with an empty database Plandillo asks for your name and a first project, then drops you into the app.

### What to know

- The port is published on `127.0.0.1` only.
- Requests with a foreign `Host` get `403`, and write requests to `/api` with a foreign `Origin` also get `403`
  (protection against DNS rebinding and CSRF).
- Data lives in `./data/plandillo.db` (installations from the Docket days keep `./data/docket.db`). A backup (`VACUUM INTO`) is written at startup and once a day to `./data/backups`;
  the last 14 are kept. In selfhost mode an administrator can also make a copy on demand and download any copy in
  **Settings → System → Backups**.
- Every start applies pending schema migrations on its own; the log names each one. Before migrating SQLite, the
  server writes `plandillo-before-migration-<time>.db` to the backup directory. Those copies are not rotated: delete
  them once the new version works. To roll back, stop the server and start the previous version on that copy.
- Seed data (optional): if `PLANDILLO_SEED` points to a JSON file and the database has no projects, it is imported at
  startup (`{"projects": […], "tasks": […]}`: projects by `slug`, tasks by `external_id`, author `claude`). Running it
  again does not create duplicates. No seed file ships with Plandillo.

SQLite creates new database directories with mode 0700 and database, WAL and backup files with owner-only permissions on Unix. Restrict older backup files and their parent directories before moving or restoring them. On Windows, store these files in a directory whose ACL grants access only to the Plandillo account and trusted administrators.

## Configuration

Settings are read from `.env` (see [the example settings](../.env.example)). Installations from the Docket days may still use `DOCKET_*` names: each setting falls back to its `DOCKET_` name when the `PLANDILLO_` one is not set, so an existing `.env` keeps working. Rename them when convenient.

| Variable | Default | Description |
|---|---|---|
| `PLANDILLO_MODE` | `local` | `local`: one user, no login. `selfhost`: several users behind a login, see [Self-host](#self-host) |
| `PLANDILLO_ADDR` | `127.0.0.1:7777` (native), `:7777` (image) | HTTP listen address |
| `PLANDILLO_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. See [Logs](#logs) |
| `PLANDILLO_LOG_FORMAT` | `text` | `text` for reading in `docker logs`, `json` for log collectors such as Loki or ELK |
| `PLANDILLO_TLS_CERT`, `PLANDILLO_TLS_KEY` | empty | Optional certificate and key files: serve HTTPS with HTTP/2 directly, without a proxy. Set both or neither. See [HTTP/2 and HTTP/3](#http2-and-http3) |
| `PLANDILLO_TRUSTED_PROXIES` | empty | Selfhost proxy IPs or CIDRs trusted to append the original client address to X-Forwarded-For |
| `PLANDILLO_DB` | `/data/plandillo.db`, or `/data/docket.db` if only that exists | SQLite database file (ignored with PostgreSQL) |
| `PLANDILLO_CODE_SOCKET` | `/run/plandillo-code/runner.sock` | Private Unix socket shared by the Plandillo server and the Code Mode worker |
| `PLANDILLO_POSTGRES_DSN` | empty | Optional PostgreSQL connection string, requires `PLANDILLO_MODE=selfhost`; empty keeps SQLite |
| `PLANDILLO_POSTGRES_MAX_CONNS` | `10` | PostgreSQL connections each instance keeps open and reuses between requests (1–500). See [PostgreSQL storage](#postgresql-storage) |
| `PLANDILLO_BACKUP_DIR` | `/data/backups` | Where backups are written |
| `PLANDILLO_MCP_TOKEN` | empty | Optional. If set, it takes precedence over the token stored in the database and rotating it in the UI is disabled |
| `PLANDILLO_ALLOWED_HOSTS` | `localhost,127.0.0.1,plandillo.localhost` | Allowed `Host` and `Origin` values |
| `PLANDILLO_SEED` | empty | Optional initial data file |
| `PLANDILLO_PUSH` | `off` | `on` enables push notifications: each device turns them on in **Settings → App**. See [Push notifications](#push-notifications) |
| `TZ` | `Asia/Ho_Chi_Minh` | Time zone used to decide what "today" is |

### Logs

Plandillo writes structured logs to stderr: read them with `docker logs plandillo` (or `make logs`, or your platform's log view). At the default `info` level you get:

- one `Plandillo listening` line at start with the address, mode, storage and log level;
- one `http request` line per API call (`/api/*`, `/mcp`, `/oauth/*`, `/.well-known/*`) with method, path, status, response size, duration in `ms`, protocol, peer address, `forwarded_for` from the proxy and the user agent. Server errors (5xx) log at `error`. The web app's own files and `/healthz` log at `debug`. The live-update stream logs once, when it closes, with how long it stayed open;
- one `mcp call` line per MCP tool call with the tool, agent, duration and error code (`info` on success, `warn` for a refused or invalid call, `error` for an internal failure);
- the errors the server meets while handling a request.

Every line logged while serving a request carries the same `request_id`, which is also returned to the client in the `X-Request-ID` header (an id set by your proxy in that header is reused when it is plain text up to 64 characters). To trace a failing request, find its `request_id` in the browser's network panel and search the log for it. Request and response bodies, query strings, cookies, `Authorization` headers and invitation tokens are never logged; MCP call arguments stay in the workspace's own call log on the Integrations page.

### HTTP/2 and HTTP/3

The server speaks HTTP/1.1 and HTTP/2. Browsers use HTTP/2 only over HTTPS, and with it every request and the live-update stream share one connection, so many open tabs no longer hit the six-connections-per-site limit of HTTP/1.1.

- **Behind a proxy (recommended).** Let the proxy terminate TLS and serve HTTP/2 and HTTP/3 to browsers. Caddy does both by default and can reach Plandillo over cleartext HTTP/2 (h2c):

  ```caddyfile
  planner.example.com {
  	reverse_proxy h2c://127.0.0.1:7777
  }
  ```

  nginx serves HTTP/2 and HTTP/3 to browsers (`listen 443 ssl; listen 443 quic; http2 on; http3 on;`) and forwards to Plandillo over HTTP/1.1 with `proxy_http_version 1.1;` and `proxy_set_header Connection "";`. Live updates need no extra buffering settings: Plandillo sends `X-Accel-Buffering: no` on its event stream.

  Traefik v3 enables HTTP/3 per entry point, and reaches Plandillo over h2c when the server URL uses that scheme. Publish port 443 for both TCP and UDP, since HTTP/3 runs over UDP. In the static configuration:

  ```yaml
  entryPoints:
    websecure:
      address: ":443"
      http3: {}
  ```

  And in a dynamic configuration file, with `plandillo` being the container name on the network Traefik shares with it:

  ```yaml
  http:
    routers:
      plandillo:
        rule: Host(`planner.example.com`)
        entryPoints: [websecure]
        service: plandillo
        tls:
          certResolver: letsencrypt
    services:
      plandillo:
        loadBalancer:
          servers:
            - url: h2c://plandillo:7777
          passHostHeader: true
  ```

  With Docker labels, the same service is `traefik.http.services.plandillo.loadbalancer.server.port=7777` plus `traefik.http.services.plandillo.loadbalancer.server.scheme=h2c`. Set `PLANDILLO_TRUSTED_PROXIES` to the subnet of that shared Docker network (`docker network inspect <network>`, field `Subnet`), or to the Traefik container's address. h2c applies only to this service, and HTTP/3 only changes how browsers reach Traefik, so routes to other containers keep working as before. Deploy a Plandillo build with HTTP/2 before switching the scheme to `h2c://`: an older build answers it with 502 errors. Dokploy generates this file for each application: edit it in the application's Traefik settings rather than on disk.
- **Without a proxy.** Set `PLANDILLO_TLS_CERT` and `PLANDILLO_TLS_KEY` to the certificate chain and key (for example from Let's Encrypt) and publish the port as 443 (for the container, `-p 443:7777`). Browsers then get HTTP/2 straight from Plandillo. The Go server has no HTTP/3, so use a proxy if you want it. Restart Plandillo after renewing the certificate.

## Self-host

`PLANDILLO_MODE=selfhost` turns Plandillo into a server for several people: everyone logs in with a username and password,
and every profile has its own isolated storage and MCP token. SQLite is the default; PostgreSQL is optional (see below).
`local` mode (the default) works as before.

1. In `.env`, set `PLANDILLO_MODE=selfhost` and put your real domain in `PLANDILLO_ALLOWED_HOSTS` (for example
   `planner.example.com`).
2. Put an HTTPS reverse proxy in front of the container and do not publish port `7777` without it. The session cookie
   is marked `Secure` only over HTTPS (or with `X-Forwarded-Proto: https`), so logging in without a proxy is not safe.
3. For SQLite, create the private `./data` directory and export the numeric `PLANDILLO_UID` and `PLANDILLO_GID` of its non-root owner as described under [Code Mode worker](#code-mode-worker). For SQLite with Code Mode, run `docker compose -f compose.selfhost.yml up -d --build` with a fresh private data directory mounted at `./data`. The PostgreSQL Compose file also includes the worker. Existing selfhost installations using a standalone binary or container must connect the isolated worker to use Code Mode after upgrading. Ordinary MCP runs in the main server.
4. Open the site and create the first account: it becomes the administrator. Do this right after the first start, since until then anyone who reaches the server can claim it.
5. Save the one-time recovery code. In **Settings → Registration**, choose **Invitation only** (the default on installation and upgrade), **Open** or **Closed**. Open signup uses `/register` and requires an available worker when enabling it. Closing signup pauses unused invitations until reopening or their original expiry. Existing owners can continue signing in. The pilot supports 50 accounts including the administrator and blocked accounts. The [historical pilot report](selfhost-security.md) describes the previous storage layout.
6. Administrators manage signup, invitations and account suspension. Blocking ends sessions and active requests. Unblocking requires a new login and a new MCP token. Owners manage their own data and can delete their own account after confirming their password. The last administrator must remain active. The operator of the host or database has physical access to stored data.

**Passkey sign-in.** In **Settings → Passkeys**, choose **Add a passkey**, name the key and confirm your current password. Follow your device’s prompt. On your next visit, select **Sign in with a passkey** to open the same workspace without typing your username. You can register up to 32 keys per account and revoke a lost key in Settings. Password sign-in remains available. Recovering your account with a recovery code revokes all enrolled passkeys.

Open Plandillo on an HTTPS hostname in `PLANDILLO_ALLOWED_HOSTS`. For local testing use `http://localhost:<port>`. An IP address cannot serve as the passkey domain. For a custom public port or to select one canonical address, set `PLANDILLO_PASSKEY_ORIGIN` to the exact browser origin, for example `https://planner.example.com:8443`. Behind a proxy, preserve the Host (including a nonstandard port) and overwrite `X-Forwarded-Proto` with the actual external scheme. After moving to a new hostname, sign in with your password and enroll a key on that hostname.

**Accounts and workspaces.** An account can own several private workspaces. Use the sidebar selector to switch and the workspace settings to create, rename, archive or restore one. Archiving revokes its generated MCP token. Permanent deletion requires an archived workspace, its exact name and, in selfhost mode, the account password. The last active workspace must stay available. Each workspace has a separate Integrations page, bearer and tool policy. The local environment token, when supplied, applies to the original workspace only. Selfhost ignores `PLANDILLO_MCP_TOKEN` and `PLANDILLO_SEED`.

**Account security.** Settings → Security lists signed-in devices and lets the owner revoke individual sessions, add passkeys or enable an authenticator. Password sign-in then requires a six-digit TOTP code. Passkeys can sign in independently. Changing the password ends every session, including copies of the current cookie, and gives the browser that made the change a new session. Recovery invalidates previous sessions, passkeys, TOTP enrollment and generated workspace tokens.

**Storage and restore.** For a fresh installation, use a new data directory or a dedicated empty PostgreSQL database. An installation with the earlier per-profile layout needs its original binary and storage. The server checks the layout before opening it for application writes and pins each new installation to its chosen mode.

An existing shared SQLite database at format version 2 upgrades to version 3 on first start. The upgrade adds columns for compressed HTML revisions and preserves existing revisions as plain text. Before upgrading, keep a verified SQLite snapshot for use with the previous binary. A rollback to that snapshot restores the data as of the snapshot time. PostgreSQL retains the shared format version 2.

SQLite stores new HTML document revisions of at least 4 KiB as gzip when this saves at least one eighth of the original size. Reading a revision or exporting it returns the original HTML. The workspace publication quota counts the original UTF-8 bytes. Wiki Markdown and inline HTML page bodies remain text. PostgreSQL stores all these fields as `text` and manages large values using [TOAST](https://www.postgresql.org/docs/18/storage-toast.html).

SQLite stores accounts, credentials and every workspace in `PLANDILLO_DB`. Automatic snapshots in `PLANDILLO_BACKUP_DIR` contain the entire database. For SQLite selfhost, back up the private `totp.key` beside the database separately, or preserve the environment key if supplied. To test a restore, copy a snapshot to a new private directory as `PLANDILLO_DB`, supply the original key, start in the original mode on loopback and check sign-in and projects in two workspaces. Keep restored installations private because snapshots contain credentials valid at the time of the backup.

On PostgreSQL, provide all replicas with the same `PLANDILLO_TOTP_KEY` to enable enrollment. Generate it once with `openssl rand -base64 32` and store it in your secret manager alongside the database recovery procedure. Losing this key prevents decryption of enrolled factors.

Limits:

- Login allows five attempts per client and normalized username pair per ten minutes, with additional limits of 20 per client and 20 per username. Password confirmations have a separate limit per owner. At most eight credential checks run concurrently.
- Signup and recovery each allow 20 requests per client per ten minutes. Recovery also limits attempts against the same normalized username. Set a per-client limit at the trusted HTTPS proxy for public installations. Forward the original Host, overwrite `X-Forwarded-Proto`, and configure the proxy to overwrite or correctly append `X-Forwarded-For`. Set `PLANDILLO_TRUSTED_PROXIES` to its immediate trusted addresses or networks. Confirm two real clients have distinct limits. Exclude invite URLs and credentials from proxy access logs.
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

### Push notifications

With `PLANDILLO_PUSH=on`, **Settings → App → Notifications** lets each device ask for two notifications: when an agent moves a task to done (or records a finished task), and a morning reminder with the number of open tasks planned for today, at a time set on that device and in its time zone. They arrive even when Plandillo is closed. On iPhone and iPad they work only in Plandillo added to the Home Screen (iOS 16.4 or later).

Web Push goes through the push service of each browser, so the server sends outbound HTTPS requests to `fcm.googleapis.com` (Chrome, Android, Edge), `*.push.apple.com` (Safari, iOS), `updates.push.services.mozilla.com` (Firefox) and `*.notify.windows.com`. It refuses every other address, does not follow redirects, and the content is end-to-end encrypted for the device (RFC 8291): the push service sees only the size and timing. The server creates its VAPID key on first start with push enabled and keeps it in the database. A reminder missed while the server was down is sent up to three hours late, then skipped for that day. Signing out stops notifications on that device. Off by default, push makes no outbound requests.

### Project icons and installed app

Upload a PNG, JPEG, WebP or SVG (up to 2 MiB) when creating a project or in its general settings. A transparent 128 × 128 PNG travels with the project in backups and JSON exports. Select any built-in glyph to remove the upload.

The project create and patch endpoints accept an optional `icon_png` containing a base64-encoded 128 × 128 PNG (up to 128 KiB decoded). Project responses include an `icon_url` for custom icons. Read the PNG with `GET /api/projects/{id}/icon` in the same profile. Patching only `icon` switches to the built-in glyph.

Open Plandillo over HTTPS or on localhost, then choose **Install Plandillo** in settings when the browser offers installation. On iPhone or iPad choose **Share → Add to Home Screen**; in Safari on macOS use **File → Add to Dock**. Start the installed app to open Today in its own window, in the workspace you used last. Long-press the icon on Android, or right-click it in the dock or taskbar on a computer, for two shortcuts: **New task** opens the new-task form and **Today** opens the day plan, whatever start page you chose. On Android the installed app also appears in the system **Share** menu: sharing a page or text to Plandillo opens the new-task form with the title filled in and the text and link in the description (iPhone and iPad do not offer this). Browsers read the shortcuts and the share entry at install time, so an app installed earlier shows them after it updates its manifest or is reinstalled. **Import Markdown** in the sidebar, or opening a `.md` file with the installed app where the system offers it (Chrome and Edge on a computer), shows the file with a choice of project: save it as a wiki page or create tasks from the checklist items you tick. Without a connection the app opens pages you visited before; see [Offline](planner.md).

### PostgreSQL storage

Set both variables to use an existing, **dedicated** PostgreSQL database:

```dotenv
PLANDILLO_MODE=selfhost
PLANDILLO_POSTGRES_DSN=postgres://plandillo:password@db.example.com:5432/plandillo?sslmode=verify-full
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
docker compose -f compose.postgres.yml up -d --build --remove-orphans
```

[`compose.postgres.yml`](../compose.postgres.yml) builds the current source and starts PostgreSQL 18.7 with a persistent
volume; PostgreSQL has no published host port. Plandillo binds to `127.0.0.1:7777`; use the same HTTPS reverse-proxy and
allowed-host setup described above for remote access. `sslmode=disable` in this example applies only to the private
Compose network. The password in this example must be hex/alphanumeric so it is valid in the connection URL.

The bootstrap account initializes the cluster. The application receives a separate login and database, both named `plandillo`, with permission to create schemas in that database. A volume created before the rename has them as `docket`: set `PLANDILLO_POSTGRES_ROLE=docket` in `.env` to keep using it. For a fresh installation, use an empty dedicated volume. Restore only snapshots created with the same application storage layout. Keep the previous volume and its matching binary available for rollback.

Operational details:

- `PLANDILLO_DB` and `PLANDILLO_BACKUP_DIR` are ignored by PostgreSQL storage; no local SQLite files are created.
- Each instance keeps up to `PLANDILLO_POSTGRES_MAX_CONNS` connections (default 10) open and hands them from one request to the next, so requests do not reconnect. An idle connection closes after 5 minutes and every connection is replaced after 30 minutes. Keep the sum over all instances below the server's `max_connections` (100 by default), leaving room for other clients. Do not put PgBouncer in transaction mode in front: Plandillo sets `search_path` per session.
- The bundled Alpine image applies the security updates for libxml2 and OpenSSL and uses Alpine’s su-exec to start PostgreSQL as its service user.
- On a **fresh volume**, set `PLANDILLO_POSTGRES_IMAGE=postgres:19.0-alpine` to use the major version exercised in CI.
  Existing Compose volumes default to 18.7. To upgrade from 18 to 19, stop the Plandillo writer, take a final
  `pg_dump -Fc`, start version 19 with a separate empty volume, then restore with `pg_restore --exit-on-error`
  into the dedicated database using the same owner. Verify login, projects and tasks before switching traffic.
  Keep the old volume and image for rollback before allowing new writes. An image-tag change alone does not migrate data.
- **Back up PostgreSQL separately** with `pg_dump` or your provider's backups/PITR. Plandillo does not run `VACUUM INTO`
  or claim to have created a PostgreSQL backup, including before the schema migrations a start applies: take a dump
  before upgrading. MCP call-log retention still runs. For the Compose example, create
  a full logical backup (including accounts and every profile) with:

  ```bash
  (
    set -eu
    umask 077
    mkdir -p -m 700 "$HOME/plandillo-backups"
    chmod 700 "$HOME/plandillo-backups"
    dump=$(mktemp "$HOME/plandillo-backups/plandillo.XXXXXXXX")
    if docker compose -f compose.postgres.yml exec -T postgres pg_dump -U "${PLANDILLO_POSTGRES_ROLE:-plandillo}" -d "${PLANDILLO_POSTGRES_ROLE:-plandillo}" -Fc > "$dump"; then
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
  To return to the previous SQLite installation, remove `PLANDILLO_POSTGRES_DSN` and restart with the original data volume.
  Changes made in PostgreSQL do not appear in SQLite. There is no automatic conversion in either direction.
- Back up and restore the entire database and its TOTP key together.
- Native and Compose installations use one Plandillo instance per database. Account and workspace operations share a pool of up to eight PostgreSQL connections.
- The database self-check verifies connectivity and reports storage size. It does not perform a full
  PostgreSQL integrity check or verify external backups. Full-text search uses PostgreSQL's `simple` dictionary with
  prefix matching for Russian, Ukrainian and English; ranking/snippets can differ from SQLite FTS5.

## Code Mode worker

The image runs as UID/GID 65532. For a bind-mounted SQLite directory, use the non-root UID and GID that own the data. For the SQLite Compose files, prepare the directory and set the numeric identities in the shell:
```bash
mkdir -p -m 700 data
export PLANDILLO_UID="$(id -u)" PLANDILLO_GID="$(id -g)"
```
Before upgrading root-owned volumes, stop the writer and verify a restorable snapshot, then grant the intended non-root identity ownership of the private Plandillo data and backup directories. Check that a disposable copy opens and a backup completes before switching the live volume.

For single-user Docker installations, run `docker compose -f compose.local.yml up -d --build --remove-orphans` from the source checkout to start Plandillo together with the isolated worker. For selfhost, use either supplied selfhost Compose file. A standalone binary or single-container launch can serve classic MCP directly. Code Mode requires a worker sharing the configured Unix socket, and returns `unavailable` if that worker is offline.

## Troubleshooting

- **`/mcp` returns 401.** On first connection this starts OAuth discovery. Use the client’s MCP login, select the workspace and approve access. Forward `/.well-known/oauth-*`, `/oauth/*` and `/api/*` under the same public HTTPS origin when using a reverse proxy. Local single-user approval requires localhost or a loopback IP. For a bearer-only client, set `Authorization: Bearer <TOKEN>` using the manual token in Integrations. The **MCP**
  page itself opens in a browser without a token: requests with `Accept: text/html` get the UI.
- **403 `forbidden_host`.** Add the host name to `PLANDILLO_ALLOWED_HOSTS` and restart the container.
- **The self-check is red.** Each row on the Integrations tab explains what to do. For the Docker quick start, run `docker logs plandillo`. For a source installation started with `make rebuild`, run `make logs`.
- **Start with an empty local board.** Stop your existing container and start Plandillo with a new, empty data directory. Keep the original directory if you want to return to that board.
