# Offline reading for the Plandillo PWA

Date: 2026-11-26. Status: draft for approval.

## Intent

When the connection to the Plandillo server drops (no internet, or the server is down), the installed app or open tab keeps working like a real PWA: the interface opens, everything the user has already seen is shown, and a clear status says that the connection is lost and the data may be out of date. Errors must be honest: a change the user tries to make while offline is rejected at once and the user is told it was not saved. When the connection returns, the app says so, refreshes the data and the status disappears.

Today a lost connection replaces the whole app with a full-screen "server is not responding" page (`ConnectionGate` in `web/src/Pwa.tsx`), nothing is kept on the device except `offline.html`, and data lives only in TanStack Query memory.

## Goals

- The app shell (index.html, every JS/CSS/font chunk) opens without the server, including pages not visited in this session.
- Data the user has loaded before (Today, boards, tasks, wiki pages, search results, etc.) is kept on the device and shown offline, also after a reload or a cold start of the installed app.
- A non-blocking status bar while offline: no internet vs. server not responding (the distinction `ConnectionGate` already makes), and the time the data was last confirmed by the server.
- Writes while offline fail immediately, roll back any optimistic update, and show a visible "not saved: no connection" message.
- On reconnect: "connection restored, updating…" while the refetch runs, then the bar goes away.
- Cached data never leaks to another account on the same device.

## Non-goals

- Making changes offline, a write queue, background sync, conflict resolution (a possible later project).
- Downloading pages the user never opened (only what was loaded is available).
- Push notifications, offline search over data not loaded.
- Any server change. Baseline: the gzip, woff2-only and ETag changes from earlier the same day (`internal/web/web.go`, `web/vite.config.ts`), under which `sw.js` is served with an ETag and `no-cache`, land first as their own commits.

## Anti-goals (a failure even if it technically works)

- Stale data shown without saying it is stale.
- A user believes a change was saved when it was not.
- A change made offline is sent later without the user knowing (TanStack's default "paused mutation" behaviour does exactly this and must be off).
- Another account on the same browser sees the previous account's cached data, even for a moment.
- The service worker serving a mix of two builds (old index.html with new chunks or the reverse).

## Constraints

- No outside hosts (AGENTS.md): everything is precached from our own origin.
- Workspace isolation: one document is bound to one workspace (`workspacePath.ts`); login/logout changes the `plandillo-auth-change` stamp in localStorage and reloads (`leaveWorkspace` in `api.ts`).
- UI strings in Russian, English and Ukrainian together (`messages.ts`); Knip fails on unused code/deps; no frontend test runner, so `tsc` + Knip + manual browser checks are the gate.
- Service worker runs only in production builds over a secure context (`main.tsx`).

## Approaches considered

- **A. Persist the TanStack Query cache in IndexedDB (chosen).** All screens already read through one QueryClient. A persister saves its successful queries to IndexedDB and restores them at start. The app knows the account, workspace and age of the data, so the status bar can show a time and logout can wipe precisely.
- **B. Cache API responses in the service worker.** Less app code, but the worker does not know which account/workspace is current, risks leaving another user's data on a shared device, and cannot easily tell the UI how old the data is.
- **C. A full local replica with sync.** Needed only for offline writes; out of scope.

The user picked A.

## Design

### 1. App shell precache (service worker)

- A small Vite plugin in `web/vite.config.ts` writes the list of every emitted file under `assets/` plus `index.html` into `sw.js` after the build (placeholder replaced in `writeBundle`), together with a build id used as the cache name.
- `install`: add all listed files to the build's cache (`cache: 'reload'`), then `skipWaiting()`. `activate`: delete every older Plandillo cache (including `plandillo-offline-v1`), `clients.claim()`. One cache always equals the build the server serves, so builds never mix.
- `fetch`: `/assets/*` → cache first, network fallback. Navigations (outside `/api`, `/mcp`, `/healthz`) → network first, cached `index.html` on failure. Everything else passes through untouched (API, SSE, icons).
- `offline.html` is no longer needed once the shell is cached; remove it (builder may keep it only as a last-resort fallback if the shell is missing).

### 2. Persisted query cache (IndexedDB)

- Add `@tanstack/react-query-persist-client` (same major as `@tanstack/react-query`); wrap the app in `PersistQueryClientProvider` instead of `QueryClientProvider` in `main.tsx`.
- Own persister over IndexedDB (`persistClient` / `restoreClient` / `removeClient`, no extra library): database `plandillo-cache`, one entry per key `<sign-in stamp or "local">:<workspaceId>`.
- What is stored (my call): successful queries whose first key is on an explicit allowlist, so a new query is not stored until someone decides it should be: `me`, `profile`, `workspaces`, `projects`, `today`, `attention`, `tasks`, `task`, `task-mini`, `task-pages`, `statuses`, `labels`, `fields`, `views`, `pages`, `page`, `search`, `trash`, `trash-projects`. Never stored: `passkeys`, `devices`, `totp`, `admin/*`, `oauth-*` (the OAuth request carries the query string; the sidebar's connected apps are simply empty offline), `mcp-*` (live status is meaningless offline), `signup-mode`, `html-document` (large; already `gcTime: 0`).
- Lifetime: default query `gcTime` raised to the persister `maxAge` (7 days), as TanStack requires. `buster` (my call) is a hand-bumped data-format constant next to the persister, not the build id, so a deploy does not throw away offline data; bump it when a stored response changes shape. The build id is used only for the shell cache.
- Privacy: on start, delete entries whose stamp is not the current one. On logout / account deletion (`leaveWorkspace` paths in `api.ts`), remove all entries before the reload. A 401 or `session_changed` after reconnect follows the existing `leaveWorkspace` path and wipes too.

### 3. Online/offline state

- One source of truth: TanStack `onlineManager`. It already follows the browser `online`/`offline` events; additionally `req()`'s existing `plandillo:disconnected` event sets it offline. My call: `req()` also fires that event for 502, 503 and 504 (a reverse proxy in front of a stopped server, see `docs/hosting.md`); a 500 stays a server error so real bugs are not hidden behind "no connection". The probe treats the same statuses as down. While offline a probe hits `/healthz` every 5 s (and on the `online` event) and sets it online on success. This is the logic `ConnectionGate` has today, moved out of the full-screen page.
- Queries keep the default network mode: offline they pause and keep showing cached data instead of turning into errors (several screens render `ErrorState` whenever `isError`, which would hide the cached data).
- Mutations use `networkMode: 'always'`: offline they fail at once with the existing "no connection" error, optimistic updates roll back via their existing `onError`, and nothing is replayed later. Exception found in review: `patch` in `TaskPanel.tsx` writes the description in `onMutate` without a rollback and relies on a refetch; it gets a proper rollback, otherwise an unsaved edit would stay on screen and be persisted.
- A `MutationCache` `onError` reports offline failures to the status bar, because many mutations do not render their own error.

### 4. What the user sees

- `ConnectionGate` becomes a thin status bar at the top of the app instead of a page. It blocks with the current full-screen page only when there is nothing to show (no cached `me`, e.g. first launch offline).
- Offline text: "No connection to the server · data as of 13:05 · changes are not saved" (or "No internet · …" when the device has no network). The time (my call) is the oldest `dataUpdatedAt` among the queries active on the current screen, so a board loaded three days ago says so even if Today refreshed a minute before the drop.
- After a rejected write: the bar briefly highlights "Not saved: no connection to the server".
- Reconnect: "Connection restored, updating…" while queries refetch (`refetchOnReconnect` is on by default; `useLive` reopens SSE and refreshes), then the bar hides.
- A screen whose data was never loaded: the shared loading placeholders (`Skeleton`/`SkeletonRows` in `ui.tsx`) show "Not available offline: open this page when connected" when the query is paused offline without data, instead of spinning forever.

## Left to the builder

- Exact copy and styling of the bar (Tailwind on existing tokens, shadcn-admin look), translations.
- How the build id is produced and injected (Vite `define` or the plugin).
- Throttling of persistence writes (TanStack default is fine).
- Screens that bypass the shared placeholders and need the same offline-empty state.
- Updating user docs (`docs/planner.md` or README) with what works offline.

## Verification

`make check`. In a production build served by the Go binary: load several pages, stop the server → status bar appears, pages still render after a reload, an edit is rejected with the message, an unvisited page shows the offline-empty state; start the server → "updating…", then fresh data. Repeat with DevTools offline (no internet text). Log out and back in as another user: no old data appears.
