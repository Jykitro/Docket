# Offline Reading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Plandillo PWA opens and shows already loaded data without the server, says plainly that it is offline and how old the data is, rejects writes with a visible message, and refreshes on reconnect.

**Architecture:** TanStack `onlineManager` is the single online/offline signal (fed by fetch failures, proxy 502–504 and a `/healthz` probe). The query cache is persisted to IndexedDB through an own persister with an allowlist; the service worker precaches the whole build. The full-screen `ConnectionGate` becomes a status bar.

**Tech Stack:** React 19, TanStack Query 5.108.4 + `@tanstack/react-query-persist-client` (same version), IndexedDB, service worker, Vite 8 plugin, Bun.

**Spec:** `docs/superpowers/specs/2026-11-26-offline-read-design.md`

## Global Constraints

- No outside hosts: precache and data stay on our origin (AGENTS.md).
- Every new UI string: Russian key via `tr()`, English and Ukrainian in `web/src/messages.ts`; `bun run i18n` must pass.
- Knip fails on unused exports and dependencies: export only what another file uses.
- No frontend test runner: each task's gate is `make check` (typecheck, Knip, Go tests; the Go tests must run outside the sandbox) plus the browser checks it lists.
- Persisted data lifetime `maxAge` = default query `gcTime` = 7 days (`7 * 24 * 60 * 60 * 1000`).
- Persisted query allowlist (first key element): `me`, `profile`, `workspaces`, `projects`, `today`, `attention`, `tasks`, `task`, `task-mini`, `task-pages`, `statuses`, `labels`, `fields`, `views`, `pages`, `page`, `search`, `trash`, `trash-projects`. Nothing else is written to disk.
- Proxy statuses 502, 503, 504 mean "disconnected"; 500 stays a server error.
- Commits: `type(web): lowercase subject`, one logical change each, no attribution lines.
- Browser checks run against the Go binary serving the built UI: `cd web && bun run build`, then `preview_start` with `api-alt` (port 7779, `.claude/launch.json`); "server down" = `preview_stop`. Localhost is a secure context, so the service worker registers.

## Review Focus

1. IndexedDB unavailable or failing (Safari private mode, blocked storage, quota): the app must work exactly as today, without offline data and without errors that break rendering. → Task 4, Step 4.
2. Session expired while offline: on reconnect `/api/auth/me` answers 401; expect the wipe and the sign-in page, never old data under a login screen. → Task 4, Step 5.
3. Another tab signs out or switches account while this tab is offline: the next start must not restore the old account's entry. → Task 4, Step 5.
4. A deploy while a tab is open: the new worker takes over and drops the old cache; a lazily loaded old chunk then fails and `vite:preloadError` reloads onto the new shell. Expect one reload, not a broken page. → Task 5, Step 4.
5. Server restarting behind a proxy (a few seconds of 502): the bar appears and clears by itself within one probe interval, with no full-screen page. → Task 1, Step 4.

---

### Task 1: One online/offline signal

**Files:**
- Create: `web/src/connection.ts`
- Modify: `web/src/api.ts` (`req` ~191–215, `errorKind` ~93–103)
- Modify: `web/src/main.tsx:39` (QueryClient defaults)
- Modify: `web/src/live.ts` (`useLive` effect)

**Interfaces:**
- Produces: `startConnectionWatch(): void` (called once in `main.tsx`); `useConnection(): { online: boolean; network: boolean; checking: boolean; lastTry?: Date; retry: () => void }`; `reportUnsaved(): void`; `useUnsavedFlash(): boolean` (true for 5 s after the last `reportUnsaved`).
- Consumes: TanStack `onlineManager` from `@tanstack/react-query`; the existing `plandillo:disconnected` window event.

- [ ] **Step 1: In `api.ts`, `req` also dispatches `plandillo:disconnected` when `res.status` is 502, 503 or 504; `errorKind` returns `'offline'` for an `ApiError` with those statuses (before the `>= 500` branch).**
- [ ] **Step 2: Create `connection.ts`.** `startConnectionWatch` sets `onlineManager.setOnline(false)` on `plandillo:disconnected`; while offline it probes `fetch('/healthz', { cache: 'no-store' })` every 5000 ms and on the window `online` event; `ok` → `setOnline(true)`; 502–504 or a throw → stay offline. `network` mirrors `navigator.onLine`. The probe/timer logic moves here from `ConnectionGate` (Pwa.tsx keeps only UI after Task 2).
- [ ] **Step 3: In `main.tsx`:** call `startConnectionWatch()`; add `gcTime` 7 days to query defaults; add `mutations: { networkMode: 'always' }`; pass `mutationCache: new MutationCache({ onError: e => { if (errorKind(e) === 'offline') reportUnsaved() } })`. In `live.ts`, also call `open()` when `onlineManager` turns online (subscribe in the effect, unsubscribe in cleanup).
- [ ] **Step 4: Verify (Review Focus 5).** `make check` passes. Browser: load Today, `preview_stop`, change a task's status → the request fails at once. Start the server → within 5 s `onlineManager.isOnline()` is true (`javascript_tool`), and the network log shows the earlier status change was never sent.
- [ ] **Step 5: Commit** `feat(web): one online signal for fetch failures and proxy errors`

### Task 2: Status bar instead of the blocking page

**Files:**
- Modify: `web/src/Pwa.tsx` (`ConnectionGate`)
- Modify: `web/src/ui.tsx:169-175` (`SkeletonRows`)
- Modify: `web/src/messages.ts`

**Interfaces:**
- Consumes: Task 1 `useConnection`, `useUnsavedFlash`.
- Produces: `ConnectionGate` keeps its name and props (`pages/root.tsx` unchanged).

- [ ] **Step 1: `ConnectionGate` renders `children` plus a thin bar at the top when `!online`, or after a reconnect while `useIsFetching() > 0` (shown at least 1.5 s).** It renders the existing full-screen `ErrorPage` only when offline and `queryClient.getQueryData(['me'])` is undefined.
- [ ] **Step 2: Copy (Russian keys, exact):** offline with network `Нет связи с сервером · данные от {time} · изменения не сохраняются`; offline without network `Нет интернета · данные от {time} · изменения не сохраняются`; unsaved flash `Не сохранено: нет связи с сервером`; reconnect `Связь восстановлена, обновляем данные…`; nothing cached for the screen `Недоступно офлайн: откройте эту страницу, когда будет связь`. `{time}` = the oldest `dataUpdatedAt` among `queryCache.findAll({ type: 'active' })` queries that have data, formatted with `locale()`, with the date when it is not today. Re-render on `queryCache.subscribe`. The bar keeps the existing "check now" action.
- [ ] **Step 3: `SkeletonRows` shows the "Недоступно офлайн…" line instead of pulsing bars while `onlineManager` is offline** (`useSyncExternalStore(onlineManager.subscribe, () => onlineManager.isOnline())`).
- [ ] **Step 4: Verify.** `bun run i18n` and `make check` pass. Browser with the server stopped: the bar shows the server text and a time; a task status change flashes the unsaved text; a never-opened route shows the offline line; with `navigator.onLine` overridden to false and an `offline` event dispatched, the "Нет интернета" text shows; server back → reconnect text, then the bar disappears. Screenshot each state.
- [ ] **Step 5: Commit** `feat(web): offline status bar instead of a blocking page`

### Task 3: Roll back a failed task description edit

**Files:**
- Modify: `web/src/TaskPanel.tsx:119-127` (`patch`)

- [ ] **Step 1: `onMutate` returns `{ prev }` (the `['task', code]` data before the write); `onError` restores it with `qc.setQueryData` before `setErr`.**
- [ ] **Step 2: Verify.** Server stopped: tick a checkbox in a task description → it unticks and the error shows. `make check` passes.
- [ ] **Step 3: Commit** `fix(web): roll back a task edit that did not reach the server`

### Task 4: Persist the query cache in IndexedDB

**Files:**
- Create: `web/src/offlineCache.ts`
- Modify: `web/src/main.tsx` (provider), `web/src/api.ts` (`leaveWorkspace`, key helper)
- Modify: `web/package.json`, `web/bun.lock` (`bun add --exact @tanstack/react-query-persist-client@5.108.4`)

**Interfaces:**
- Produces in `offlineCache.ts`:
  - `createPersister(key: string): Persister` — IndexedDB database `plandillo-cache`, object store `clients`, entry id = `key`; `persistClient` / `restoreClient` / `removeClient`; every IndexedDB failure resolves quietly (restore → `undefined`).
  - `wipeOfflineCache(keepStamp?: string): Promise<void>` — deletes every entry whose id does not start with `${keepStamp}:`; with no argument deletes all.
  - `persistOptions(key: string)` → `{ persister: createPersister(key), maxAge, buster: OFFLINE_DATA_VERSION, dehydrateOptions: { shouldDehydrateQuery } }`; `OFFLINE_DATA_VERSION = '1'` (module constant, hand-bumped when a stored response changes shape); `shouldDehydrateQuery` = status `success` and first key in the allowlist.
- Produces in `api.ts`: `offlineStamp(): string` = `sessionStamp() ?? 'local'`; the cache key is `${offlineStamp()}:${workspaceId}`.

- [ ] **Step 1: Write `offlineCache.ts` as above; add the dependency.**
- [ ] **Step 2: `main.tsx` renders `PersistQueryClientProvider client={client} persistOptions={persistOptions(`${offlineStamp()}:${workspaceId}`)}`** instead of `QueryClientProvider`, and calls `void wipeOfflineCache(offlineStamp())` at start.
- [ ] **Step 3: `leaveWorkspace` keeps the abort first, then `wipeOfflineCache().finally(() => location.reload())`.**
- [ ] **Step 4: Verify (Review Focus 1).** Load Today, a board, a task and a wiki page; stop the server; reload → all four render from cache, the bar shows the time. Read `plandillo-cache` with `javascript_tool`: one entry, no `passkeys`, `devices`, `totp`, `admin`, `oauth-*`, `mcp-*` keys. Temporarily make `createPersister`'s open reject (local edit, reverted before commit) → the app loads and works online with no uncaught errors.
- [ ] **Step 5: Verify (Review Focus 2, 3).** Sign out → `plandillo-cache` is empty after the reload. In selfhost mode (`api-selfhost` config) sign in as another user → nothing of the first user shows, online or offline. While offline, invalidate the session on the server (delete it from the sessions table), bring the server back → sign-in page, cache empty.
- [ ] **Step 6: Commit** `feat(web): keep loaded data on the device for offline reading`

### Task 5: Precache the whole build in the service worker

**Files:**
- Modify: `web/vite.config.ts` (new plugin next to `woff2Only`)
- Modify: `web/public/sw.js`
- Delete: `web/public/offline.html`

**Interfaces:**
- Produces: `sw.js` holds `const BUILD = '__PLANDILLO_BUILD__'` and `const PRECACHE = __PLANDILLO_PRECACHE__` placeholders that the plugin replaces in `writeBundle` (build id = short hash of the sorted emitted asset names; list = `/index.html` + every emitted `/assets/*` file).

- [ ] **Step 1: Plugin `precacheList` in `vite.config.ts`**: in `writeBundle`, read `<outDir>/sw.js`, replace both placeholders, write it back; throw (failing the build) if a placeholder is missing.
- [ ] **Step 2: `sw.js`**: cache name `plandillo-${BUILD}`; `install` → `cache.addAll(PRECACHE.map(u => new Request(u, { cache: 'reload' })))`, then `skipWaiting()`; `activate` → delete every cache starting with `plandillo-` except the current one, then `clients.claim()`; `fetch` → GET `/assets/*` cache-first with network fallback; navigations outside `/api`, `/mcp`, `/healthz` network-first with the cached `/index.html` on failure; everything else untouched.
- [ ] **Step 3: Delete `public/offline.html`; `bun run build`;** `grep -c __PLANDILLO_ internal/web/dist/sw.js` → `0`.
- [ ] **Step 4: Verify (Review Focus 4).** Load the app once online; stop the server; open a route not visited in this session (Integrations) → its chunk loads from cache. Rebuild after a trivial change, start the server, reload → only the new `plandillo-<build>` cache remains (`caches.keys()`), the page works after at most one automatic reload. `make check` passes.
- [ ] **Step 5: Commit** `feat(web): precache the whole build so the app opens offline`

### Task 6: User docs and end-to-end check

**Files:**
- Modify: `docs/planner.md` (new "Offline" section: what works, what does not, how long data stays, that signing out clears it)

- [ ] **Step 1: Write the section** (4–8 lines, English, the file's tone).
- [ ] **Step 2: Run the spec's Verification paragraph on a fresh build**, screenshots of the offline bar, the unsaved flash, the offline-empty page and the reconnect state; record the `make check` output.
- [ ] **Step 3: Commit** `docs(web): what works offline`
