# Plandillo

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="web/public/plandillo-inverse.svg">
  <img src="web/public/plandillo.svg" alt="Plandillo" width="290">
</picture>

[![CI](https://github.com/huntly-digital/plandillo/actions/workflows/ci.yml/badge.svg)](https://github.com/huntly-digital/plandillo/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/huntly-digital/plandillo)](https://github.com/huntly-digital/plandillo/releases)
[![License: MIT](https://img.shields.io/github/license/huntly-digital/plandillo)](LICENSE)

Plandillo is an open-source task planner from [Huntly Digital](https://github.com/huntly-digital) for people who work with AI agents. Keep your projects, daily plan and notes in one place. You can move a task on the board, ask Claude Code to add its implementation checklist, then have Codex record what passed. Both clients work on the same task through [MCP](https://modelcontextprotocol.io), and their changes appear live in your browser.

Run the planner on your machine with SQLite, or host it for several accounts with SQLite or PostgreSQL. The public site for `plandillo.dev` lives in [landing/](landing/README.md).

No external services, and in the default local mode no users and no login. The UI never talks to an outside host
(fonts are bundled).

<p align="center">
  <img src="docs/images/today-dark.png" alt="The Today screen: day plan, progress bar, tasks grouped by project and the activity feed" width="820">
</p>

> The interface supports English, Russian and Ukrainian. The screenshots show a fictional release project.

## Contents

- [Features](#features)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Self-host](#self-host)
- [Using the UI](#using-the-ui)
- [Connecting agents (MCP)](#connecting-agents-mcp)
- [Development](#development)
- [REST API](#rest-api)
- [Troubleshooting](#troubleshooting)
- [Contributing and license](#contributing-and-license)

## Features

- **Today view.** Day plan, overdue tasks, progress, recent activity and MCP status in one place.
- **Kanban board and list.** Per-project statuses, drag and drop, filters, grouping, sorting and saved views.
- **Rich tasks.** Markdown description with clickable checkboxes, subtasks, notes and a full change history.
- **Agent integrations.** 19 MCP tools, or all of them as one Code Mode tool that runs JavaScript, a read-only mode,
  per-tool switches and an audit log of every call.
- **Live updates.** Changes made over MCP show up in the browser immediately (server-sent events).
- **Safe by default.** Bound to `127.0.0.1`, `Host` and `Origin` checks, bearer token for `/mcp`.
- **Easy to keep.** A single SQLite file, automatic daily backups, a trash bin and a full JSON export.

<table>
  <tr>
    <td width="50%"><img src="docs/images/board-light.png" alt="Project board with columns per status"><br><sub><b>Board.</b> Columns per status, filters, grouping and saved views.</sub></td>
    <td width="50%"><img src="docs/images/task-light.png" alt="Task panel with properties and a markdown description"><br><sub><b>Task panel.</b> Properties, markdown with checkboxes, subtasks, notes and history.</sub></td>
  </tr>
</table>

## Quick start

Install [Docker](https://docs.docker.com/get-docker/) and run the following command from the directory where you want to keep your data.

Start the newest tested build of `main` on Intel/AMD or ARM64:

```bash
mkdir -p -m 700 data
docker run -d --name docket --user "$(id -u):$(id -g)" --pull=always -p 127.0.0.1:7777:7777 -v "$PWD/data:/data" --restart unless-stopped ghcr.io/huntly-digital/plandillo:main
```

Open <http://localhost:7777>. The image includes the web UI, API and SQLite. GitHub Actions updates
`:main` after the SQLite and PostgreSQL checks pass. Use `:sha-<full-commit-sha>` to pin a particular main build,
or `:latest` to follow tagged stable releases.

Before updating an older root-owned installation, stop the writer and verify a restorable copy of the complete data directory. Ensure the intended non-root service UID can open the database and create a backup on a disposable copy. Apply the verified ownership changes to the private Plandillo volume before switching images.

To update a container started with the command above, use the **same directory** and run:

```bash
docker pull ghcr.io/huntly-digital/plandillo:main && docker stop docket && docker rm docket && docker run -d --name docket --user "$(id -u):$(id -g)" -p 127.0.0.1:7777:7777 -v "$PWD/data:/data" --restart unless-stopped ghcr.io/huntly-digital/plandillo:main
```

Or download a binary for Linux, macOS or Windows from the [releases page](https://github.com/huntly-digital/plandillo/releases)
and run it. The native binary listens on `127.0.0.1:7777` by default. Point
`DOCKET_DB` and `DOCKET_BACKUP_DIR` at local paths, since they default to `/data/...` (see
[Configuration](#configuration)). To build from source:

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

### Upgrading from Docket

Plandillo is the new name for Docket. Use your existing service, data directory and hostname. The Compose service `docket`, the `DOCKET_*` settings, database and backup filenames, PostgreSQL schemas, browser preferences and the PWA installation ID keep their existing values. Installed clients can keep their MCP URL, bearer token and registration name. New installations use `plandillo` for the skills and client registration. The installer updates previously installed Docket skills under their existing names; for a manually registered `docket` connection, use `DOCKET_URL` in the installer command to keep that registration.

`make build` provides `bin/plandillo` and the existing `bin/docket` command. `make image` tags the same local image as `plandillo:local` and `docket:local`. Images built from this checkout provide both `/app/plandillo` and `/app/docket`. Previously published binaries and images use the older executable name. New images are published at `ghcr.io/huntly-digital/plandillo`. Existing installations using `ghcr.io/jykitro/docket` can switch the image reference on their next update and reuse the same data mount.

## Configuration

Settings are read from `.env` (see `.env.example`):

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
3. For SQLite, create the private `./data` directory and export the numeric `DOCKET_UID` and `DOCKET_GID` of its non-root owner as described under [Code Mode](#code-mode). For SQLite with Code Mode, run `docker compose -f compose.selfhost.yml up -d --build` with the existing selfhost data mounted at `./data`. The PostgreSQL Compose file also includes the worker. Existing selfhost installations using a standalone binary or container must connect the isolated worker to use Code Mode after upgrading. Ordinary MCP runs in the main server.
4. On the server, retrieve the first administrator's setup code with `docker compose -f compose.selfhost.yml exec docket /app/plandillo setup-code` (use `compose.postgres.yml` for PostgreSQL). For a native installation run `plandillo setup-code` with the server's `DOCKET_DB` setting. Enter it on the first-run screen. Restarting before setup replaces the code. Completing setup removes the private file from the data directory.
5. Save the one-time recovery code. In **Settings → Registration**, choose **Invitation only** (the default on installation and upgrade), **Open** or **Closed**. Open signup uses `/register` and requires an available worker when enabling it. Closing signup pauses unused invitations until reopening or their original expiry. Existing owners can continue signing in. The pilot supports 50 accounts including the administrator and blocked accounts. The [pilot verification report](docs/selfhost-security.md) records the security checks, capacity sample and rollout procedure.
6. Administrators manage signup, invitations and account suspension. Blocking ends sessions and active requests. Unblocking requires a new login and a new MCP token. Owners manage their own data and can delete their own account after confirming their password. The last administrator must remain active. The operator of the host or database has physical access to stored data.

**Passkey sign-in.** In **Settings → Passkeys**, choose **Add a passkey**, name the key and confirm your current password. Follow your device’s prompt. On your next visit, select **Sign in with a passkey** to open the same workspace without typing your username. You can register up to 32 keys per account and revoke a lost key in Settings. Password sign-in remains available. Recovering your account with a recovery code revokes all enrolled passkeys.

Open Plandillo on an HTTPS hostname in `DOCKET_ALLOWED_HOSTS`. For local testing use `http://localhost:<port>`. An IP address cannot serve as the passkey domain. For a custom public port or to select one canonical address, set `DOCKET_PASSKEY_ORIGIN` to the exact browser origin, for example `https://planner.example.com:8443`. Behind a proxy, preserve the Host (including a nonstandard port) and overwrite `X-Forwarded-Proto` with the actual external scheme. After moving to a new hostname, sign in with your password and enroll a key on that hostname.

With SQLite, `auth.db` (users and sessions) and `users/<id>.db` (one database per profile) sit next to `DOCKET_DB`; SQLite profile
backups are written to `DOCKET_BACKUP_DIR/<id>`, and a profile created later gets its first backup within an hour.
Every profile has its own MCP token, shown on the **Integrations** tab. Each agent connects to the same `/mcp` address and the
profile is picked by the token. `DOCKET_SEED` and `DOCKET_MCP_TOKEN` are ignored in this mode (a warning is logged if
`DOCKET_MCP_TOKEN` is set).

Switching an existing install to `selfhost` does not import `DOCKET_DB` (`docket.db`): the file stays untouched and
selfhost profiles start empty. Switching back to `local` brings the old data back.

Back up the auth catalog and workspace data together. For a consistent SQLite snapshot, stop Plandillo and copy the entire data directory, including any WAL files, before restarting. Automatic per-profile SQLite backups alone cannot restore logins. For PostgreSQL, back up the entire dedicated database with `pg_dump` or PITR. Startup verifies each workspace against its account's stable owner identifier. For the first upgrade of a legacy installation, verify the snapshot's provenance and follow the offline binding procedure in [the selfhost runbook](docs/selfhost-security.md). A mismatched or unbound workspace stops startup.

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

Plandillo creates `docket_auth` for accounts, sessions and invitations, and `docket_user_<id>` for each profile's projects,
tasks, wiki, history, settings and MCP call log. Migrations run automatically at startup/profile creation in a transaction.
Every connection uses only its assigned schema; the public schema is not a fallback. Profile deletion drops only that
profile's schema. These are application isolation boundaries, not separate database roles: the server's database
credential can access every profile and must stay private.

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

[`compose.postgres.yml`](compose.postgres.yml) builds the current source and starts PostgreSQL 18.7 with a persistent
volume; PostgreSQL has no published host port. Plandillo binds to `127.0.0.1:7777`; use the same HTTPS reverse-proxy and
allowed-host setup described above for remote access. `sslmode=disable` in this example applies only to the private
Compose network. The password in this example must be hex/alphanumeric so it is valid in the connection URL.

The bootstrap account initializes the cluster. The application receives a separate login (`docket`) with permission to create schemas in its dedicated database. Before upgrading a volume originally initialized with `POSTGRES_USER=docket`, stop writers, take and verify a complete backup, then initialize a **separate empty volume** with the new configuration. Restore the application schemas into it as `docket` using `pg_restore --no-owner --no-privileges --exit-on-error`, verify data and sign-in, then switch traffic. The Compose health check refuses the old elevated login. Keep the original volume for rollback.

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
- Never reset `docket_auth` while retaining profile schemas: reused user IDs could attach new accounts to old data.
  Back up and restore the whole database together.
- Run **one Plandillo instance per database**: live events, login rate limiting and MCP routing are still kept in process.
  PostgreSQL does not enable horizontal scaling. Each loaded profile and the auth store use at most one connection;
  allow for that in PostgreSQL's connection budget (idle connections close after five minutes).
- The database self-check checks connectivity and reports the profile's table/index size; it does not perform a full
  PostgreSQL integrity check or verify external backups. Full-text search uses PostgreSQL's `simple` dictionary with
  prefix matching for Russian, Ukrainian and English; ranking/snippets can differ from SQLite FTS5.

## Using the UI

| Screen | Address | What it does |
|---|---|---|
| Today | `/` | Day plan, overdue tasks, progress, activity and MCP status; the status circle cycles todo → doing → done |
| Needs attention | `/attention` | Review agent findings across projects, open the source task, take an issue, snooze it or close it with a reason |
| Project board | `/p/:slug` | Columns per project status, drag and drop, filters, grouping, sorting, a list tab and saved views |
| Project wiki | `/p/:slug/wiki` | Right-click a page or open its ⋯ menu to pin it or change its place among siblings. You can also drag pages and tick Markdown checklists while reading. |
| Task panel | `?task=TRK-07`, `/t/TRK-07` | Properties, markdown description with clickable checkboxes, subtasks, notes and history |
| Project settings | `/p/:slug/settings` | Icon, colour, key, statuses, labels, custom fields, views, export, archive and delete |
| Integrations | `/integrations` | MCP or Code Mode: status and self-check, address and token, access mode, tool switches and the call log |
| Trash | `/trash` | Restore tasks and projects, or empty the trash |

**Keyboard:** `C` creates a task, `⌘K` / `Ctrl+K` opens search, `Esc` closes the panel, search and dialogs.

**Layout and theme:** the top bar collapses the sidebar to an icon rail (a drawer on narrow screens) and switches the
theme between light, dark and system.

**Needs attention** brings open issues from every project together, including existing tasks with area `issue`. An agent
calls `report_issue` as soon as it notices a problem. Supply the project, a short title, the observation and, when known,
the source task. A matching open title in that project receives the observation. Supply `existing_issue` to target a
known duplicate under another title. Exact retries from the same agent and source add one observation. Blockers and
decisions appear first. Start work, snooze until a date in the server time zone, or close with a reason.

**Wiki page formats.** Choose Markdown or HTML before saving a new page. For an HTML page, paste source or
load a self-contained HTML file (up to 2 MiB). The page displays the interactive document directly.
In a Markdown wiki page or task description, use **Insert HTML** to insert an `html-preview` fenced block at the cursor.
An ordinary `html` code fence displays source code. Both standalone pages and embedded blocks use the isolated HTML viewer.

A workspace retains up to 64 MiB and 4,096 HTML revisions. Existing history remains readable when the quota is reached.

**Interactive HTML documents** can be attached to tasks and wiki pages. Paste source or choose an HTML file and
publish it. The interactive content appears inline with the Markdown. Expand the document title below the preview
to choose a version, edit the source or delete the document. Each publication records the author and
date. The JSON export includes all versions. Agents can publish through `publish_html` and read a revision through
`read_html`. Bundle the data, styles and scripts in the document (up to 2 MiB). The viewer runs inline JavaScript
inside sandboxed frames with an opaque origin and a restrictive content security policy. Reports receive no Plandillo API
credentials. A report's in-memory controls reset when you leave the page or change its version.

**Task IDs** look like `TRK-07`: the project key plus a number. Numbers are never reused. You can change a project key in
its settings and every ID is renumbered, but old links that use the previous key stop working.

<a id="connecting-claude-mcp"></a>

Paged project, task, trash and Today reads accept `limit` and `offset` (up to 100 rows per page). Use task summaries for lists. Large detail pages may require a smaller page size. Export streams the full snapshot.

## Connecting agents (MCP)

On the first start Plandillo creates a `dkt_…` token and stores it in the database. It
is shown (masked, with a "Show" button) on the **Integrations** tab, where you can also rotate it and copy a ready-made config.

<p align="center">
  <img src="docs/images/mcp-dark.png" alt="The Integrations tab: self-check, server address, token and access mode" width="820">
</p>

Open **Integrations**, choose the client and the intended endpoint, and copy its registration command. The CLI configuration refers to an environment variable derived from that endpoint. In the shell that starts Claude Code or Codex, run the displayed hidden-input command and paste the token at the prompt. Desktop clients can obtain the same credential from a private environment or credential manager. When switching hosts, register the new endpoint and use its own credential.

For another client, choose **Other client** on the Integrations tab and enter an agent name. The resulting URL
includes `?agent=my-agent`, which sets the author on tasks, notes, history and MCP calls. Existing URLs also
work. Plandillo uses the client name supplied with the call or a recognizable Codex or Claude user agent. An
unidentified client appears as **MCP agent**. Older history keeps the author recorded at the time.

### Try a shared task

Open a project in the web UI and connect a client using **Integrations**. Try these requests with Claude Code or Codex in a workspace that allows writes:

- “Find the release checklist in my project. If it exists, show me the outstanding steps.” The agent searches and reads the task before making a change.
- “Add a task to review mobile navigation tomorrow, with a checklist for the menu and keyboard focus.” Open the new card to see its planned date and description.
- “Add the test results to that task. Mark it done if the checks passed.” The note and status appear in the same card, including which agent changed it.

### Code Mode

The image runs as UID/GID 65532. For a bind-mounted SQLite directory, use the non-root UID and GID that own the data. For the SQLite Compose files, prepare the directory and set the numeric identities in the shell:
```bash
mkdir -p -m 700 data
export DOCKET_UID="$(id -u)" DOCKET_GID="$(id -g)"
```
Before upgrading root-owned volumes, stop the writer and verify a restorable snapshot, then grant the intended non-root identity ownership of the private Plandillo data and backup directories. Check that a disposable copy opens and a backup completes before switching the live volume. Update the pinned runtime manifest in the Dockerfile when refreshing the base image, verifying both Linux architectures.

For single-user Docker installations, run `docker compose -f compose.local.yml up -d --build` from the source checkout to start Plandillo together with the isolated worker. For selfhost, use either supplied selfhost Compose file. A standalone binary or single-container launch can serve classic MCP directly. Code Mode requires a worker sharing the configured Unix socket, and returns `unavailable` if that worker is offline.

The same server is also available at `/mcp/code` (same token, same commands with that address). There the assistant
sees one tool, `code`, after [Cloudflare's Code Mode](https://developers.cloudflare.com/agents/tools/codemode/): it
writes JavaScript that calls the 19 tools as `await codemode.get_today({})`, loops and filters inside Plandillo, and gets
back only the result. That saves round trips and context on big jobs (triage, bulk moves, reports). The code runs in
an embedded sandbox with no network or files, for at most 30 seconds and 100 tool calls; access mode, tool switches
and the call log apply to every call it makes. Pick MCP or Code Mode on the **Integrations** tab: the address, the
configs and the install command follow. How it works inside: [`docs/integrations.md`](docs/integrations.md).

### Agent skill: Plandillo as a personal planner

Three skills in [`skills/`](skills), one `SKILL.md` each:

| Skill | What it covers |
|---|---|
| [`plandillo`](skills/plandillo/SKILL.md) | Find or create a task, record the plan as a checklist, add notes, call `report_issue` when a problem appears, and reconcile the board before the final message. |
| [`plandillo-reference`](skills/plandillo-reference/SKILL.md) | The answers an agent needs: data model, every tool and its quirks, error codes, conventions for plans, notes and issues, a FAQ |
| [`plandillo-setup`](skills/plandillo-setup/SKILL.md) | Connecting Claude Code, Claude Desktop and Codex, installing the skills, the hooks, troubleshooting |

- Plandillo sends the body of `plandillo` to every client as the MCP server instructions, so a connected agent has the workflow already. A test keeps it under the 2048 bytes Claude Code accepts, which is why the detail lives in the other two skills.
- To have the skills trigger from the first message, install them. The **Integrations** tab has an **Install in one command** button: it
  copies one line to paste into a terminal (macOS, Linux). The script writes all three skills for Claude Code (`~/.claude/skills`)
  and Codex (`~/.agents/skills`) and registers the MCP server in whichever of the two it finds. It is served at
  `/api/mcp/install.sh` and takes the address from `PLANDILLO_URL`. It prints the credential setup command after registration:

  ```bash
  curl -fsSL http://localhost:7777/api/mcp/install.sh | PLANDILLO_URL=http://localhost:7777/mcp sh
  ```

  By hand, the same tab has Copy and Download buttons, and the server serves each skill at `/api/mcp/skill/<name>`.
  Claude Code reads `~/.claude/skills/<name>/SKILL.md`, Codex `~/.agents/skills/<name>/SKILL.md`:

  ```bash
  for s in plandillo plandillo-reference plandillo-setup; do
    mkdir -p ~/.claude/skills/$s    # Codex: ~/.agents/skills/$s
    curl -fsS http://localhost:7777/api/mcp/skill/$s -o ~/.claude/skills/$s/SKILL.md
  done
  ```

  In selfhost mode the API needs a login: use the Download buttons instead.
- Optional strict mode, the same JSON for Claude Code and Codex (the snippet is on the **Integrations** tab): a `SessionStart`
  hook reminds the agent to find or create the task, and a `Stop` hook makes it run the reconcile step before it
  finishes. It blocks only the first stop of a turn, so it cannot loop. Claude Code: the project's
  `.claude/settings.json`. Codex: `~/.codex/hooks.json` (hooks are on by default). Codex's file is global, so in a repo
  without Plandillo the agent answers "not tracked" and finishes.

### Tools

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

## Development

Requirements: Go (see `go.mod`), [Bun](https://bun.sh) and `make`.

```bash
make install   # frontend dependencies (bun install --frozen-lockfile)
make check     # typecheck + Knip (unused code and dependencies) + Go tests
```

Go tests open a port on localhost. Run `make help` for every target:

| Target | What it does |
|---|---|
| `make web` | Frontend build: `tsc --noEmit` and `vite build` into `internal/web/dist` |
| `make build` | Builds `bin/plandillo` with the UI embedded |
| `make test` | Backend tests (`go test ./...`) |
| `make test-postgres` | Same domain, auth, REST and MCP tests against PostgreSQL; requires `DOCKET_TEST_POSTGRES_DSN` |
| `make typecheck`, `make knip` | Frontend checks |
| `make dev-api` | Backend on `:7777` with a separate `./data/dev.db` |
| `make dev-web` | Vite with hot reload (proxies `/api` to `:7777`) |
| `make image`, `make rebuild`, `make stop`, `make logs` | Docker image and container |
| `make clean` | Removes `bin/` and the built frontend |

To run the PostgreSQL integration tests locally, point `DOCKET_TEST_POSTGRES_DSN` at a disposable PostgreSQL server,
using a `postgres://` URL and a test role with `CREATEDB`. Each test creates and drops a randomly named database;
the database in the URL is used only as the administrative connection. Do not use a production server. For example:

```bash
docker run -d --name docket-pg-tests -e POSTGRES_USER=docket -e POSTGRES_PASSWORD=docket_test_only \
  -e POSTGRES_DB=docket_test -p 127.0.0.1:55483:5432 postgres:19.0-alpine
# Wait until: docker exec docket-pg-tests pg_isready -U docket -d docket_test
DOCKET_TEST_POSTGRES_DSN='postgres://docket:docket_test_only@localhost:55483/docket_test?sslmode=disable' make test-postgres
docker rm -f docket-pg-tests
```

CI runs both the default SQLite checks and the PostgreSQL suite.

For frontend work with hot reload, run `make dev-api` in one terminal and `make dev-web` in another.

Working on Plandillo with an assistant: [`AGENTS.md`](AGENTS.md) (read by Codex and most agents) and [`CLAUDE.md`](CLAUDE.md)
(Claude Code, imports `AGENTS.md`) describe the commands, layout, conventions and gotchas.

### Project layout

```
landing/            public Plandillo website, built separately from the planner
cmd/docket          config, wiring, graceful shutdown, housekeeping (daily backup and call-log cleanup)
internal/database   SQLite/PostgreSQL connections, SQL dialect boundary and schema locks
internal/store      task queries, SQLite/PostgreSQL migrations and SQLite backups
internal/service    domain rules, validation, activity, events, MCP settings
internal/httpapi    REST, SSE /api/events, host/origin guard, bearer auth for /mcp, MCP self-check
internal/mcpserver  MCP tools → service, access policy and call log; Code Mode (/mcp/code, codemode.go)
internal/events     pub/sub for SSE
internal/web        embedded SPA (dist)
web/                React + TypeScript + Vite + Tailwind CSS v4 (shadcn-admin look; tokens in web/src/styles.css)
```

Layering rule: `httpapi` and `mcpserver` call only `service`; task SQL lives in `store`, auth SQL in `auth`, and connection/dialect code in `database`.
How the integrations (MCP, Code Mode) work and how to add one: [`docs/integrations.md`](docs/integrations.md).

## REST API

All requests are performed on behalf of the human actor (stored as `denis`; the UI shows the profile name). Errors come as
`{"error": {"code", "message"}}` (`validation` 400, `not_found` 404, `status_in_use` 409, `conflict` 409, `forbidden` 403). A task in the
path can be given by code (`TRK-07`) or by id.

| Method and path | Purpose |
|---|---|
| `GET/POST /api/projects`, `PATCH/DELETE /api/projects/:id` | Projects; `DELETE` moves a project to the trash |
| `POST /api/projects/:id/archive`, `POST /api/projects/:id/restore`, `GET /api/projects/:id/export` | Archive, restore from the trash, JSON export |
| `GET/POST /api/projects/:id/statuses`, `PATCH/DELETE /api/statuses/:id`, `POST …/statuses/reorder` | Statuses; `DELETE ?move_to=<id>` moves the tasks |
| `GET/POST /api/projects/:id/labels`, `PATCH/DELETE /api/labels/:id` | Area labels |
| `GET/POST /api/projects/:id/fields`, `PATCH/DELETE /api/fields/:id` | Custom fields (`number`, `url`, `select`) |
| `GET/POST /api/views?project=`, `PATCH/DELETE /api/views/:id` | Saved views |
| `GET /api/today?date=` | Tasks for a date, overdue tasks and recent activity |
| `GET /api/tasks?project=&status=&planned_for=&area=&created_by=&q=&parent=&include_done=1` | Task list |
| `POST /api/tasks`, `GET/PATCH/DELETE /api/tasks/:id` | CRUD; `planned_for: ""` clears the date |
| `POST /api/tasks/:id/move` | `{status, before_id?, after_id?, project_id?}` (drag and drop) |
| `POST /api/tasks/:id/restore`, `POST /api/tasks/:id/notes`, `POST /api/tasks/carry-over` | Restore, add a note, move tasks from one date to another |
| `GET /api/tasks/:id/pages` | Wiki pages that mention the task (`[{id, title, project}]`) |
| `GET/POST /api/projects/:id/pages` | Wiki pages of a project (without bodies); create (`title`, `body_md?`, `parent_id?`) |
| `GET/PUT/DELETE /api/pages/:id` | A page with backlinks and links; update (`expected_updated_at` returns `conflict` 409 if the page changed); `DELETE` re-parents children |
| `GET /api/attention` | Open issues due for attention across projects |
| `POST /api/tasks/:id/triage` | Send `action` of `take`, `snooze` (with `until`) or `close` (with `reason`) |
| `POST /api/html-documents` | Publish `title` and `html` on exactly one `task` or `page_id`. Supply the current `expected_version` for updates |
| `GET /api/html-documents/:id` | JSON with source and metadata. Optional `?version=N` selects a historical revision |
| `DELETE /api/html-documents/:id?version=N` | Delete the document and its versions if N is current |
| `PUT /api/pages/:id/layout` | Send one of `pinned` (boolean), `before_id` or `after_id` (sibling page ID). Changes navigation without changing the content revision. |
| `GET /api/search?q=&scope=all\|tasks\|projects` | Tasks by title and description (with a snippet), and projects |
| `GET /api/trash`, `GET /api/trash/projects`, `DELETE /api/trash` | List trash or purge the reviewed snapshot. DELETE takes both arrays `tasks` and `projects`, each containing `{id, deleted_at}`. A changed snapshot returns 409. |
| `GET /api/mcp/status`, `POST /api/mcp/check`, `GET/PATCH /api/mcp/settings` | Status, self-check, mode and tool switches |
| `GET /api/mcp/token`, `POST /api/mcp/token/rotate`, `GET/DELETE /api/mcp/calls` | Token, rotation, call log |
| `GET/PUT /api/profile` | Display name (an empty name means first run) |
| `GET /api/events` | SSE: `{type, task_id?, project_id?, actor}` |
| `GET /api/export`, `GET /healthz` | Full dump, liveness check |

## Troubleshooting

- **`/mcp` returns 401.** You need the header `Authorization: Bearer <TOKEN>`; find the token on the Integrations tab. The **MCP**
  page itself opens in a browser without a token: requests with `Accept: text/html` get the UI.
- **403 `forbidden_host`.** Add the host name to `DOCKET_ALLOWED_HOSTS` and restart the container.
- **The self-check is red.** Each row on the Integrations tab explains what to do. Logs: `make logs` (or `docker logs docket`).
- **Start from scratch.** Stop the container and delete `./data/docket.db*`; backups are in `./data/backups`.

## Contributing and license

Issues and pull requests are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately, as
described in [SECURITY.md](SECURITY.md). Plandillo is released under the [MIT License](LICENSE).
