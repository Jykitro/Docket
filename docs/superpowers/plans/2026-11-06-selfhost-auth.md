# Self-host auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `DOCKET_MODE=selfhost` puts the whole app behind a login; every profile gets its own SQLite file and MCP token.

**Architecture:** A new `internal/auth` package owns `auth.db` (users, sessions, invites, password hashing, login rate limit). A new `internal/selfhost` package opens one `Service` + `httpapi.Router` per user (`Workspaces`) and a dispatcher that checks the session cookie (or the MCP bearer token) and hands the request to that user's existing `Router`. Handlers in `httpapi` stay untouched. The SPA gets a `Gate` that shows login / first-run / invite screens.

**Tech Stack:** Go 1.27 stdlib (`crypto/pbkdf2`, `crypto/rand`), chi, modernc sqlite, React 19 + TanStack Query, Tailwind.

**Spec:** `docs/superpowers/specs/2026-11-06-selfhost-auth-design.md`

## Global Constraints

- Mode switch: `DOCKET_MODE` is `local` (default, unchanged behavior) or `selfhost`; any other value fails startup.
- Files: `auth.db` and `users/<id>.db` live in the directory of `DOCKET_DB` (`/data/auth.db`, `/data/users/<id>.db`).
- Passwords: PBKDF2-HMAC-SHA256, 600000 iterations, 16-byte salt, stored as `pbkdf2-sha256$<iter>$<b64 salt>$<b64 hash>`; length 10–128 characters.
- Usernames: trimmed, lowercased, must match `^[a-z0-9][a-z0-9_.-]{1,31}$`; unique.
- Session: random 32-byte token, only its SHA-256 stored, lifetime 30 days, cookie `docket_session` (HttpOnly, SameSite=Lax, Path=/, `Secure` when `r.TLS != nil` or `X-Forwarded-Proto: https`).
- Invite: random token (hash stored), valid 7 days, single use, link `/invite/<token>`.
- Login limit: 5 failures per 10 minutes, key `<remote host>|<username>`.
- Setup code: random, logged at startup while `users` is empty; `POST /api/auth/setup` works only while `users` is empty, then 404.
- Error body everywhere: `{"error":{"code","message"}}`. Codes used: `unauthorized` 401, `forbidden` 403, `not_found` 404, `validation` 400, `rate_limited` 429, `invalid_credentials` 401.
- In `selfhost`: `DOCKET_SEED` and `DOCKET_MCP_TOKEN` are ignored (a shared token would be ambiguous).
- The working tree already holds unrelated uncommitted edits (including `internal/httpapi/httpapi.go`, `web/src/*`). Stage with `git add -p` / explicit paths and only this plan's hunks; do the work on a new branch `feature/selfhost-auth` (create it before Task 1), not on `main`.
- Tests: `go test ./...` for Go; the web has no test runner, verify with `npm run typecheck` and `npm run knip` in `web/`.

## Review Focus

1. Username `" Denis "` after `denis` exists: rejected as taken (case/whitespace) — Task 2.
2. Expired or already-used invite, expired session: rejected like unknown ones — Tasks 2, 3.
3. Admin deletes a user: the user's cookie stops working at once, their DB file is gone, an admin cannot delete themselves — Task 7.
4. Unknown username vs wrong password: identical status, code and message (no username enumeration) — Tasks 2, 6.
5. Five failures for `a` block `a` but not `b` from the same host; the key uses the remote host because behind a proxy every client shares it — Tasks 4, 6.

---

### Task 1: Password hashing and tokens

**Files:**
- Create: `internal/auth/crypto.go`
- Test: `internal/auth/crypto_test.go`

**Interfaces:**
- Produces: `func HashPassword(pw string) (string, error)`, `func CheckPassword(hash, pw string) bool`, `func newToken() (token, hash string, err error)` (token = base64url of 32 random bytes, hash = hex SHA-256 of it), `func hashToken(token string) string`, `var pbkdf2Iter = 600_000`.

- [ ] **Step 1: Write the failing tests.** `TestMain` sets `pbkdf2Iter = 1000` for speed. `TestHashRoundTrip`: `CheckPassword(h, "correct horse")` true, `"wrong"` false. `TestHashSalted`: two hashes of the same password differ. `TestCheckPasswordRejectsGarbage`: `CheckPassword("nonsense", "x")` is false and does not panic. `TestNewTokenHash`: `hashToken(tok) == hash`, two tokens differ.
- [ ] **Step 2: Run `go test ./internal/auth/` — expect FAIL (undefined).**
- [ ] **Step 3: Implement the four functions in `crypto.go`.** Use `crypto/pbkdf2.Key(sha256.New, pw, salt, iter, 32)`, compare with `subtle.ConstantTimeCompare`; parse the iteration count from the stored hash so the constant can change later.
- [ ] **Step 4: Run `go test ./internal/auth/` — expect PASS.**
- [ ] **Step 5: Commit** `git add internal/auth/crypto.go internal/auth/crypto_test.go && git commit -m "feat(auth): password hashing and tokens"`

### Task 2: auth.db — users, sessions, login

**Files:**
- Create: `internal/auth/db.go`
- Test: `internal/auth/db_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `type User struct { ID int64 \`json:"id"\`; Username string \`json:"username"\`; IsAdmin bool \`json:"is_admin"\`; CreatedAt string \`json:"created_at"\` }`
  - `var ErrTaken, ErrBadCredentials, ErrNotFound, ErrSetupDone, ErrInvalidInvite error`; `type ValidationError string` implementing `error` (username/password rules).
  - `func Open(path string) (*DB, error)`, `func (d *DB) Close() error`, `func (d *DB) UserCount() (int, error)`
  - `func (d *DB) createUser(tx *sql.Tx, username, password string, admin bool) (User, error)` (shared by Task 3)
  - `func (d *DB) Authenticate(username, password string) (User, error)`
  - `func (d *DB) NewSession(userID int64) (string, error)`, `func (d *DB) UserBySession(token string) (User, error)`, `func (d *DB) DeleteSession(token string) error`
  - `DB` has an unexported `now func() time.Time` (defaults to `time.Now`) so tests can move the clock.

- [ ] **Step 1: Write the failing tests** (each opens `Open(filepath.Join(t.TempDir(), "auth.db"))`; users are made through a test helper that calls `createUser` in a tx):
  - `TestUsernameNormalized`: creating `"denis"` then `" Denis "` returns `ErrTaken`; stored username is `denis`.
  - `TestValidation`: usernames `"a"`, `"../x"`, `"has space"` and passwords of 9 and 129 chars return `ValidationError`.
  - `TestAuthenticate`: right password returns the user; wrong password and unknown user both return exactly `ErrBadCredentials`.
  - `TestSessionLifecycle`: `NewSession` → `UserBySession` returns the user; after `DeleteSession` it returns `ErrNotFound`; advancing `d.now` by 31 days makes a fresh session return `ErrNotFound`; the DB row holds a hash, not the token.
- [ ] **Step 2: Run `go test ./internal/auth/` — expect FAIL.**
- [ ] **Step 3: Implement in `db.go`.** Schema created in `Open` with `CREATE TABLE IF NOT EXISTS` (`users`, `sessions`, `invites` per the spec); `modernc.org/sqlite` with the same pragmas as `store.Open`, `SetMaxOpenConns(1)`. `Authenticate` verifies against a fixed dummy hash when the user is missing so timing is similar.
- [ ] **Step 4: Run `go test ./internal/auth/` — expect PASS.**
- [ ] **Step 5: Commit** `git add internal/auth/db.go internal/auth/db_test.go && git commit -m "feat(auth): users, sessions, login"`

### Task 3: Setup, invites, user management

**Files:**
- Modify: `internal/auth/db.go`
- Test: `internal/auth/db_test.go`

**Interfaces:**
- Consumes: Task 2.
- Produces:
  - `func (d *DB) SetupAdmin(username, password string) (User, error)` — `ErrSetupDone` if any user exists; check-and-insert in one tx.
  - `type Invite struct { ID int64; CreatedAt, ExpiresAt string }` (JSON `id`, `created_at`, `expires_at`)
  - `func (d *DB) NewInvite(createdBy int64) (token string, expires time.Time, err error)`, `func (d *DB) AcceptInvite(token, username, password string) (User, error)`, `func (d *DB) ListInvites() ([]Invite, error)` (unused and unexpired only)
  - `func (d *DB) ListUsers() ([]User, error)`, `func (d *DB) DeleteUser(id int64) error` (also deletes the user's sessions), `func (d *DB) ChangePassword(userID int64, oldPw, newPw string) error` (`ErrBadCredentials` on wrong old password).

- [ ] **Step 1: Write the failing tests:**
  - `TestSetupAdmin`: first call returns an admin; second returns `ErrSetupDone`; `UserCount` is 1.
  - `TestInviteAccept`: token accepted once creates a non-admin user; the second use returns `ErrInvalidInvite`.
  - `TestInviteExpired`: after moving `d.now` 8 days, `AcceptInvite` returns `ErrInvalidInvite`; `ListInvites` no longer lists it.
  - `TestInviteUnknownToken`: `ErrInvalidInvite`.
  - `TestAcceptInviteTaken`: a taken username returns `ErrTaken` and the invite stays unused.
  - `TestDeleteUserKillsSessions`: `DeleteUser` makes `UserBySession` return `ErrNotFound`, `ListUsers` excludes the user.
  - `TestChangePassword`: wrong old password returns `ErrBadCredentials`; after a change, the old password fails and the new one authenticates.
- [ ] **Step 2: Run `go test ./internal/auth/` — expect FAIL.**
- [ ] **Step 3: Implement the methods** reusing `createUser` inside a tx; `AcceptInvite` marks `used_at` and creates the user in the same tx.
- [ ] **Step 4: Run `go test ./internal/auth/` — expect PASS.**
- [ ] **Step 5: Commit** `git add internal/auth && git commit -m "feat(auth): setup, invites, user management"`

### Task 4: Login rate limiter

**Files:**
- Create: `internal/auth/limiter.go`
- Test: `internal/auth/limiter_test.go`

**Interfaces:**
- Produces: `func NewLimiter(max int, window time.Duration) *Limiter`, `func (l *Limiter) Blocked(key string) bool`, `func (l *Limiter) Fail(key string)`, `func (l *Limiter) Reset(key string)`; unexported `now func() time.Time`.

- [ ] **Step 1: Write the failing tests:** `TestLimiterBlocksAfterMax` (5 `Fail("1.2.3.4|a")` → `Blocked` true; `Blocked("1.2.3.4|b")` false), `TestLimiterWindowExpires` (advance `now` past the window → not blocked), `TestLimiterReset`.
- [ ] **Step 2: Run `go test ./internal/auth/ -run Limiter` — expect FAIL.**
- [ ] **Step 3: Implement** with a mutex-guarded `map[string][]time.Time`, pruning on access. `// shortcut: in-memory, resets on restart; move to auth.db if the server runs several replicas.`
- [ ] **Step 4: Run `go test ./internal/auth/` — expect PASS.**
- [ ] **Step 5: Commit** `git add internal/auth/limiter.go internal/auth/limiter_test.go && git commit -m "feat(auth): login rate limiter"`

### Task 5: Per-user workspaces

**Files:**
- Create: `internal/selfhost/workspaces.go`
- Test: `internal/selfhost/workspaces_test.go`

**Interfaces:**
- Consumes: `store.Open`, `service.New`, `events.NewBus`, `(*Service).EnsureMCPSettings`, `GetProfile`, `SetProfile`, `auth.User`.
- Produces:
  - `type Workspace struct { ID int64; Svc *service.Service; Router http.Handler }` (plus unexported `st *store.Store`)
  - `func NewWorkspaces(dir string, loc *time.Location, build func(*service.Service, *events.Bus) http.Handler) *Workspaces`
  - `func (w *Workspaces) Open(ctx context.Context, u auth.User) (*Workspace, error)` — idempotent; DB at `<dir>/users/<id>.db`; calls `EnsureMCPSettings`; sets the profile name to `u.Username` when it is empty.
  - `func (w *Workspaces) Get(id int64) (*Workspace, bool)`, `func (w *Workspaces) All() []*Workspace`, `func (w *Workspaces) Remove(id int64) error` (closes the store, deletes `<id>.db`, `-wal`, `-shm`), `func (w *Workspaces) Close()`

- [ ] **Step 1: Write the failing tests** (`build` returns `http.NotFoundHandler()`): `TestOpenCreatesFileAndProfile` (file exists, `GetProfile` name equals the username), `TestOpenIdempotent` (same pointer twice), `TestOpenKeepsExistingProfileName` (rename via `SetProfile`, reopen after `Close`, name unchanged), `TestRemoveDeletesFiles` (file gone, `Get` returns false).
- [ ] **Step 2: Run `go test ./internal/selfhost/` — expect FAIL.**
- [ ] **Step 3: Implement** with a mutex-guarded `map[int64]*Workspace`.
- [ ] **Step 4: Run `go test ./internal/selfhost/` — expect PASS.**
- [ ] **Step 5: Commit** `git add internal/selfhost && git commit -m "feat(selfhost): per-user workspaces"`

### Task 6: Dispatcher, login/logout/me, isolation

**Files:**
- Create: `internal/selfhost/handler.go`
- Modify: `internal/httpapi/httpapi.go` (export `HostGuard`, add local `GET /api/auth/me`)
- Test: `internal/selfhost/handler_test.go`, `internal/httpapi/httpapi_test.go`

**Interfaces:**
- Consumes: Tasks 2–5; `httpapi.Router`.
- Produces:
  - `httpapi.HostGuard(allowedHosts []string) func(http.Handler) http.Handler` (rename of the unexported `hostGuard`, which builds the allow map itself from the slice; `Router` calls it).
  - `type Options struct { Auth *auth.DB; WS *Workspaces; Web http.Handler; AllowedHosts []string; SetupCode string }`
  - `func Handler(o Options) http.Handler` with routes: `/healthz` (public), `GET /api/auth/me`, `POST /api/auth/login`, `POST /api/auth/logout`, any other `/api/*` (needs session → user's `Router`), `/mcp` (`GET` with `Accept: text/html` → `Web`; otherwise bearer token → the workspace whose `Svc.MCPToken` matches, compared with `subtle.ConstantTimeCompare`, else 401), everything else → `Web`.
  - Login body `{"username","password"}`; success sets the cookie and returns the user; wrong credentials return 401 `invalid_credentials` with one fixed message; blocked returns 429 `rate_limited`. Limiter is created inside `Handler` (`auth.NewLimiter(5, 10*time.Minute)`), key `host|username` where host is `net.SplitHostPort(r.RemoteAddr)`.
- Test helper `newServer(t)` in `handler_test.go`: temp dir, `auth.Open`, `NewWorkspaces` with `build` creating `httpapi.Router` (hosts `localhost,127.0.0.1`, `Web: http.NotFoundHandler()`, a real `mcpserver.Handler`), an `httptest.Server`, and `addUser(name)` that creates the user via `auth`, opens the workspace, and returns a logged-in cookie.

- [ ] **Step 1: Write the failing tests:**
  - `TestAnonymousBlocked`: `GET /api/projects`, `GET /api/events`, `GET /api/export`, `POST /mcp` without credentials all return 401; `GET /healthz` returns 200.
  - `TestMeStates`: empty DB → 200 with `setup:true`; anonymous with users → 401; with cookie → 200 with `user.username`.
  - `TestLoginLogout`: right password sets `HttpOnly` cookie with `SameSite=Lax`, then `/api/projects` returns 200; logout then returns 401 on the same cookie; `X-Forwarded-Proto: https` adds `Secure`.
  - `TestLoginErrorsIdentical`: unknown user and wrong password give the same status, code and message.
  - `TestLoginRateLimit`: 5 wrong passwords for `a`, the 6th attempt (even with the right password) returns 429; user `b` still logs in.
  - `TestIsolation`: user A creates a project via `POST /api/projects`; B's `GET /api/projects` does not contain it; A's MCP token on `/mcp` reaches A's workspace; B's cookie never sees A's data.
  - `TestMcpBearerRouting`: unknown token → 401; A's token passes (`initialize` request gets a non-401 answer).
  - httpapi: `TestLocalMe` — `GET /api/auth/me` in `Router` returns 200 `{"mode":"local"}`.
- [ ] **Step 2: Run `go test ./internal/selfhost/ ./internal/httpapi/` — expect FAIL.**
- [ ] **Step 3: Implement** `handler.go`; wrap the whole mux in `httpapi.HostGuard`. Add `/api/auth/me` to `httpapi.Router` (one route returning `map[string]string{"mode":"local"}`).
- [ ] **Step 4: Run `go test ./...` — expect PASS (existing httpapi tests unchanged).**
- [ ] **Step 5: Commit** `git add internal/selfhost && git add -p internal/httpapi && git commit -m "feat(selfhost): session dispatcher, login, MCP routing"`

### Task 7: Setup, invite accept, password, admin endpoints

**Files:**
- Modify: `internal/selfhost/handler.go`
- Test: `internal/selfhost/handler_test.go`

**Interfaces:**
- Consumes: Task 6 helpers; `auth.DB.SetupAdmin/AcceptInvite/NewInvite/ListInvites/ListUsers/DeleteUser/ChangePassword`; `Workspaces.Open/Remove`.
- Produces routes: `POST /api/auth/setup` `{code,username,password}` (wrong code → 403 `forbidden`; users exist → 404; success logs the user in and opens the workspace), `POST /api/auth/invite/{token}` `{username,password}` (bad token → 400 `validation`; success logs in), `PUT /api/auth/password` `{old,new}`, admin-only (non-admin → 403): `GET /api/admin/users`, `DELETE /api/admin/users/{id}`, `GET /api/admin/invites`, `POST /api/admin/invites` → `{"url":"/invite/<token>","expires_at":...}`.

- [ ] **Step 1: Write the failing tests:**
  - `TestSetupFlow`: wrong code 403 and no user created; right code creates an admin and a working cookie; second call returns 404.
  - `TestInviteFlow`: admin creates an invite, an anonymous client accepts it, the new user lists no projects, is not admin, and gets 403 on `/api/admin/users`.
  - `TestInviteReuseAndExpiry`: a used token and a garbage token return 400.
  - `TestAdminDeleteUser`: deleting B returns 204; B's old cookie now returns 401; `users/<id>.db` is gone; the admin deleting their own id returns 400.
  - `TestChangePasswordEndpoint`: wrong `old` returns 401 `invalid_credentials`; success lets the new password log in.
- [ ] **Step 2: Run `go test ./internal/selfhost/` — expect FAIL.**
- [ ] **Step 3: Implement the routes.** JSON decoding limited to 1 MiB like `httpapi.decode`; setup code compared with `subtle.ConstantTimeCompare`; `auth.ValidationError` maps to 400 `validation`, `ErrTaken` to 400 `validation` "username is taken".
- [ ] **Step 4: Run `go test ./...` — expect PASS.**
- [ ] **Step 5: Commit** `git add internal/selfhost && git commit -m "feat(selfhost): setup, invites, password, admin API"`

### Task 8: Config, main wiring, docs

**Files:**
- Modify: `internal/config/config.go`, `cmd/docket/main.go`, `README.md`, `docker-compose.yml`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `selfhost.Handler`, `selfhost.NewWorkspaces`, `auth.Open`, `service.Maintenance`.
- Produces: `Config.SelfHost bool`, `Config.DataDir string` (directory of `DB`).

- [ ] **Step 1: Write the failing test** `TestLoadMode`: unset and `local` → `SelfHost` false; `selfhost` → true and `DataDir` equals the dir of `DOCKET_DB`; `bogus` → error.
- [ ] **Step 2: Run `go test ./internal/config/` — expect FAIL.**
- [ ] **Step 3: Implement `Load` changes and the `selfhost` branch in `run()`:** open `auth.Open(DataDir/auth.db)`; `NewWorkspaces(DataDir, cfg.Loc, build)` where `build` returns `httpapi.Router(Deps{Svc, Bus, AllowedHosts, MCP: mcpserver.Handler(svc, loc), Web, SelfAddr: cfg.Addr, Ctx: ctx})`; open every existing user's workspace at startup (`ListUsers` + `Open`) so MCP token lookup sees all of them; when `UserCount()==0` generate a setup code (`crypto/rand`, base64url) and `slog.Info("first run: create the admin with this setup code", "code", code)`; skip `SetEnvToken` and the seed; serve `selfhost.Handler` instead of `httpapi.Router`; run `maintenance` per workspace with `BackupDir/<id>` (workspaces opened later at runtime get no scheduled backup until restart — `// shortcut: restart picks them up; move to a ticker over All() when needed`). The `local` path stays as today.
- [ ] **Step 4: Docs.** README env table: `DOCKET_MODE`; a "Self-host" section: set `DOCKET_ALLOWED_HOSTS` to the real domain, run behind an HTTPS proxy (needed for the `Secure` cookie), find the setup code in `docker logs`, invite flow, per-user MCP tokens, `DOCKET_SEED`/`DOCKET_MCP_TOKEN` ignored. `docker-compose.yml`: comment on the `127.0.0.1` binding.
- [ ] **Step 5: Run `go build ./... && go test ./...` — expect PASS.**
- [ ] **Step 6: Smoke test by hand:** `DOCKET_MODE=selfhost DOCKET_DB=$TMPDIR/d/docket.db DOCKET_BACKUP_DIR=$TMPDIR/d/b go run ./cmd/docket`; log shows the setup code; `curl -s localhost:7777/api/projects` returns 401; `curl -s localhost:7777/api/auth/me` returns `{"mode":"selfhost","setup":true}`. Then run without `DOCKET_MODE` and confirm `/api/projects` returns 200 as before.
- [ ] **Step 7: Commit** `git add internal/config cmd/docket README.md docker-compose.yml && git commit -m "feat: DOCKET_MODE=selfhost wiring and docs"` (use `git add -p` for files with unrelated edits).

### Task 9: SPA gate and auth screens

**Files:**
- Create: `web/src/Auth.tsx`
- Modify: `web/src/api.ts`, `web/src/main.tsx`

**Interfaces:**
- Consumes: Task 6–7 endpoints.
- Produces in `api.ts`: types `AuthUser = { id: number; username: string; is_admin: boolean; created_at: string }`, `Me = { mode: 'local' | 'selfhost'; setup?: boolean; user?: AuthUser }`; `api.me()`, `api.login(username, password)`, `api.logout()`, `api.setup(code, username, password)`, `api.acceptInvite(token, username, password)`, `api.changePassword(old, next)`, `api.adminUsers()`, `api.adminInvites()`, `api.createInvite()`, `api.deleteUser(id)`; hook `useMe()` (query key `['me']`, `retry: false`). In `req()`: a 401 on any URL except `/api/auth/*` dispatches `window.dispatchEvent(new Event('docket:unauthorized'))` before throwing.
- Produces in `Auth.tsx`: `export function Gate({ children }: { children: ReactNode })`.

- [ ] **Step 1: Add the api.ts types, methods and `useMe`.** `Gate` distinguishes: success + `mode==='local'` or `user` → `children`; success + `setup` → `Setup`; 401 error → `Login`.
- [ ] **Step 2: Implement `Gate`, `Login`, `Setup`, `AcceptInvite` in `Auth.tsx`.** Same card layout and classes as `Onboarding.tsx` (centered card, `cap`, `btnPri`, Russian copy). `Gate` renders `AcceptInvite` when `location.pathname` starts with `/invite/` and `mode` is not local; after success it navigates to `/` and invalidates `['me']`. It listens for `docket:unauthorized` and invalidates `['me']`. Login errors show the server message; the `rate_limited` message tells the user to wait.
- [ ] **Step 3: In `main.tsx` wrap `<App />` in `<Gate>`** (hooks in `App` must not run before login, so the gate sits outside it). Clear the query cache (`client.clear()`) on logout and on `docket:unauthorized` so the next profile never sees stale data.
- [ ] **Step 4: Run `cd web && npm run typecheck` — expect no errors.** (knip runs after Task 10: `adminUsers`, `createInvite` etc. are used there.)
- [ ] **Step 5: Commit** `git add web/src/Auth.tsx web/src/api.ts web/src/main.tsx` (use `git add -p` where files carry unrelated edits).

### Task 10: Settings: account, users, logout — and end-to-end check

**Files:**
- Create: `web/src/Users.tsx`
- Modify: `web/src/AppSettings.tsx`

**Interfaces:**
- Consumes: Task 9 `useMe`, `api.*`.
- Produces: `export function AccountSection()` in `Users.tsx`; rendered inside `AppSettings` only when `useMe().data?.mode === 'selfhost'`.

- [ ] **Step 1: Implement `AccountSection`:** shows the username, a "Выйти" button (`api.logout()` then `client.clear()` + invalidate `['me']`), a change-password form (old / new, min 10), and for admins a "Пользователи" list (name, delete button with confirm, hidden for self), a "Создать приглашение" button that shows the full link `location.origin + url` with a copy button and its expiry, and the list of pending invites.
- [ ] **Step 2: Render it in `AppSettings`** below the "Профиль" block; the existing profile-name form stays.
- [ ] **Step 3: Run `cd web && npm run typecheck && npm run knip` — expect PASS.**
- [ ] **Step 4: Rebuild and run the server:** `cd web && npm run build`, then `DOCKET_MODE=selfhost DOCKET_DB=$TMPDIR/e2e/docket.db DOCKET_BACKUP_DIR=$TMPDIR/e2e/b go run ./cmd/docket` and open it in the preview browser.
- [ ] **Step 5: Walk through and confirm each:** first-run screen accepts the logged setup code and creates the admin; onboarding skips the name step; settings shows the invite link; in a second private window the invite creates user B; B sees an empty workspace while A's tasks stay private; B's MCP token (MCP page) differs from A's; deleting B logs B out on the next request; a wrong password five times shows the rate-limit message; reload keeps the session; running without `DOCKET_MODE` shows no login at all.
- [ ] **Step 6: Commit** `git add web/src/Users.tsx web/src/AppSettings.tsx && git commit -m "feat(web): account settings, users, invites"`
