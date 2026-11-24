# Selfhost pilot verification

The pilot allows up to 50 personal workspaces on one Plandillo process. Applications authenticate owners with a session or a workspace MCP token. The host operator and the dedicated PostgreSQL role can access stored data. Checked on 24 November 2026 with synthetic accounts and disposable storage.

| Surface | Evidence |
|---|---|
| REST and search | Two accounts had identical project keys, task codes and page IDs, with different contents. Each account saw its own projects, tasks, pages, search results and JSON export. Deleting one owner's task preserved the other copy. |
| Browser | Chromium covered signup, mobile layout, password recovery and switching users in the same browser. The owner saw a private HTML wiki report inside an opaque-origin sandbox. Existing HTML and Markdown tests covered task and wiki previews. |
| Input handling | Reviewed SQL construction for task filters, sorting, search and page queries. Values are parameters. Dynamic columns and ordering come from server-selected expressions. Workspace paths and schema identifiers use numeric owner IDs. Markdown passes through DOMPurify. Interactive HTML runs in sandboxed frames with a restrictive content policy. |
| Sessions and live events | Verified guest denial, origin and host checks, cookie invalidation, separation of SSE events by owner, and cancellation of active SSE and MCP requests when an account is blocked. Test fixtures use loopback. The deployment operator supplies HTTPS, preserves Host, overwrites forwarding headers and restricts the backend port. |
| MCP | Two owners called Code Mode with distinct tokens. The test container used a read-only root filesystem, UID 65532, no network, 256 MiB of memory and one CPU. A callback obeyed read-only mode. Stopping the worker made Code Mode unavailable while regular MCP reads and REST writes continued. |
| Registration | Verified first-admin setup, all three modes, a paused invite surviving reopening, a queued signup refused after closure, and two concurrent signups contesting a single invite or the final slot. A temporary provisioning failure could be retried by signing in. |
| Recovery and restore | Verified single-use recovery, session invalidation, MCP token rotation and continued suspension of blocked accounts. Restored a stopped SQLite data directory and a full PostgreSQL dump. Both allowed the original owner to log in and read the original project. Both refused startup when only workspace storage was restored. |

The SQLite upgrade below was exercised on a stopped copy created at Git revision `749fab5` using setup and an invitation. The new binary refused both unbound profiles. After explicit pairing, both owners could sign in, read their original private projects and use their original MCP bearers. The incident-restore procedure was then tested on the same copy. Both old sessions, bearers and recovery codes returned 401. The passkey fixture rows and invites were cleared, and both owners could use newly issued credentials.

The release checks covered `make check`, `make test-postgres`, `go vet ./...`, the race detector and 19 Chromium tests. `govulncheck` reported no known reachable vulnerabilities. Bun's audit reported no high or critical advisories for the 163 packages checked. The full Go linter still reports pre-existing diagnostics, tracked separately in DCK-30.

## Capacity sample

The opt-in Go test reopened 50 initialized workspaces and measured 50 sequential, authenticated `list_projects` requests through the selfhost HTTP handler. It captures one local run with small workspaces. Heap is the Go test process's live heap after collection. Reopening excludes binary startup, network and proxy latency.

| Storage | Reopen all | MCP p50 | MCP p95 | Go heap | Storage | PostgreSQL connections |
|---|---:|---:|---:|---:|---:|---:|
| SQLite | 0.50 s | 3.7 ms | 5.9 ms | 13.0 MiB | 11.6 MiB | — |
| PostgreSQL | 1.17 s | 48.1 ms | 94.2 ms | 17.2 MiB | 51.1 MiB | 51 |

Use `DOCKET_TEST_PILOT=1 go test ./internal/selfhost -run '^TestPilot$' -count=1 -v` to repeat. Set `DOCKET_TEST_POSTGRES_DSN` to an isolated test server for PostgreSQL. Budget connections for maintenance and backups in addition to the 51 measured. These historical timings predate the in-memory MCP token index.

## Pilot operation

1. Start the isolated worker with the supplied Compose configuration and restore-test the auth catalog with its workspace data. The worker availability probe checks its socket. The operator enforces CPU, memory, networking and mount restrictions.
2. Start with invitations. Observe `workspace signup completed` and `workspace provisioning failed` in server logs, Code Mode errors in the owner's MCP call log, free disk and PostgreSQL connections. Invite tokens, recovery codes and passwords should never appear in proxy access logs.
3. Enable Open in Settings after these checks. Switch to Closed to stop accepting new accounts immediately. Block a specific account to revoke its active access. Closing signup preserves existing owners' access.

## First upgrade of a legacy installation

New accounts receive a random workspace generation in the auth catalog. The first creation of the workspace stores that same generation alongside its owner in the workspace migration transaction. Startup checks both values for every existing workspace before replaying any pending deletion. A pending deletion also requires the random confirmation recorded in that workspace when the owner requested deletion. A workspace restored from before the deletion request requires operator reconciliation.

Older installations have no shared generation. Perform their first upgrade on an isolated, stopped copy of a **verified, complete backup set**. Keep the original copy for rollback. Verify provenance from the backup manifest or a consistent full dump taken at the same time. Numeric IDs, usernames and editable profile names alone cannot establish provenance. If the catalog and workspaces cannot be traced to one set, obtain a matching backup before continuing.

1. Disable all network access to the copy. Run the new binary once to create the catalog tables. It stops when it encounters a legacy account or an orphan. Stop the process before opening storage tools.
2. Enumerate the catalog's `users(id,username)`, `pending_deletions`, and every workspace file or schema. Verify one unique storage location for each provisioned account. Reconcile missing workspaces and historical pending deletions against independent records. Old pending deletions have no workspace confirmation. Recover a complete snapshot predating the deletion and ask the verified owner to repeat it, or preserve the copy for manual reconciliation.
3. For each **individually verified live account** insert one row in `workspace_bindings(user_id,generation)` in the auth catalog. Generate a unique random value per account, for example `lower(hex(randomblob(24)))` in SQLite or `gen_random_uuid()::text` in PostgreSQL. For an interrupted upgrade, reuse the existing generation after checking its provenance. Preserve any existing generation and owner marker. The auth schema on PostgreSQL is `plandillo_auth` (`docket_auth` on an existing Docket installation).
4. In the independently matched workspace, set the missing `settings(key,value)` row `selfhost.owner` to that catalog username and the missing `selfhost.generation` to the catalog generation. Use `INSERT … ON CONFLICT (key) DO NOTHING` and reread both rows. Any disagreement requires the matching backup. Each PostgreSQL workspace uses its own `plandillo_user_<id>` schema (`docket_user_<id>` on Docket installations). For SQLite, attach only the verified owner file for that step. Keep all network access disabled throughout.
5. Restart the isolated copy and verify all owners and representative private records. Take a fresh complete backup after the upgrade. Production rollout uses the corresponding matched data and the same verification before opening access.

## Restore following a suspected credential leak

Keep the restored instance disconnected from all client networks, including existing proxy routes, until the credentials below have been revoked. A snapshot restores the credential state that existed when it was taken. Perform revocation on a separate writable copy and preserve the original evidence.

1. With Plandillo stopped, check ownership and outstanding deletions against the complete backup set. In the auth catalog, clear restored session/invitation tokens, recovery codes and passkey registrations, and close registration. Run the following in a transaction in the SQLite auth database or with the PostgreSQL search path set to the installation’s auth schema. Enable foreign key enforcement on the SQLite connection before beginning.

   ```sql
   BEGIN;
   UPDATE registration SET mode='closed' WHERE id=1;
   DELETE FROM sessions;
   DELETE FROM invites;
   UPDATE account_access SET recovery_hash='';
   DELETE FROM passkeys;
   DELETE FROM passkey_owners;
   COMMIT;
   ```

2. In **every** verified live workspace, including blocked accounts, delete the stored bearer while the service is stopped. Run `DELETE FROM settings WHERE key IN ('mcp_token','mcp_token_last_used_at')` in the individual SQLite database or PostgreSQL workspace schema. Startup provisions a fresh random bearer. In local mode an environment override takes precedence, so replace any configured `DOCKET_MCP_TOKEN` separately.
3. Reconcile account suspension, administrator access and password changes made since the snapshot with records kept outside the restored database. Keep affected accounts blocked until identity and password replacement are verified through a private channel. Clear any reverse-proxy caches and revoke upstream credentials that were part of the incident.
4. Start on the isolated network, check that a previously valid session and bearer are refused, and confirm the counts of restored passkeys, recovery hashes and outstanding invites are zero before reenrollment. Verify a sample project for each affected owner, distribute new credentials through verified private channels, then reopen the proxy. Keep registration closed until the operator chooses a mode.
