# Project Wiki Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A third project tab "Вики": markdown pages per project with an editor, page↔page and page↔task links, readable and writable by the assistant over MCP.

**Architecture:** New `pages` table, `store/pages.go`, `service/pages.go`, REST routes, four MCP tools, one `Wiki.tsx` rendered inside `ProjectBoard` as a third tab. Links are not stored: `[[Title]]` and task codes are parsed from `body_md` in Go; backlinks are computed by scanning the project's pages.

**Tech Stack:** Go (chi, SQLite, MCP go-sdk), React + TypeScript + Tailwind v4, TanStack Query, `marked` + DOMPurify.

**Spec:** `docs/superpowers/specs/2026-11-07-project-wiki-design.md`

## Spec deviations (found while reading the code; they win over the spec)

- Error code is `validation` (not `invalid_argument`); new code `conflict` → HTTP 409. `statusFor` in `httpapi.go` gets the case.
- Project routes use `/projects/{id}` where `{id}` is a slug, key or number (`resolveProject`). Pages: `/projects/{id}/pages`, `/pages/{id}`.
- SQLite `lower()` and `LIKE` only fold ASCII, so uniqueness uses a `title_key` column computed in Go (`strings.ToLower(strings.TrimSpace(title))`), and backlinks are found by parsing every page body of the project in Go, not with `LIKE`. Only the task→pages lookup uses `LIKE '%CODE%'` (ASCII, then verified by regex).
- Pages do not write `activity` rows: the Today feed prints unknown actions raw and expects a task. Writes emit one SSE event `page_changed` instead.
- `PurgeProject` must delete the project's pages first (foreign key).
- The tab lives inside `ProjectBoard` (`Board.tsx`), which owns the project header and tab bar; route `/p/{slug}/wiki/{id?}` renders `Board` with a `wiki` prop.

## Global Constraints

- Commits: `type(scope): lowercase subject`, types feat/fix/docs/test/refactor/chore, scope `web`/`mcp`/…, no trailing period, no attribution lines. Ask the user before the first commit; one logical change per commit.
- Tests live next to the code (`*_test.go`) and change in the same commit as the code. No frontend test runner: `tsc` and Knip are the frontend gate.
- Layering: `httpapi` and `mcpserver` call only `service`; SQL only in `store`.
- `service` returns `*service.Error` with `Code` `validation`, `not_found` or `conflict`; validation messages say what is wrong and what is allowed.
- MCP results are compact JSON; the catalogue is `service.MCPTools`; write tools are blocked read-only via `IsWriteTool`. `delete_page` is deliberately NOT an MCP tool.
- UI strings: `tr('Русский текст')` plus English and Ukrainian entries in `web/src/messages.ts`, added together. Dates via `locale()`. No outside hosts.
- Knip fails on unused exports: do not export what nothing imports.
- Do not touch the body of `skills/docket/SKILL.md` (2048-byte MCP limit).
- Do not run `docker build` or `make rebuild`. Finish with `make check` and report what it printed; if the sandbox blocks the Go tests' local port, say so.
- Limits: title 1–120 characters, no `[`, `]` or newline; body ≤ 204800 bytes.

## Review Focus

- Titles differing only by Cyrillic case ("Настройки" / "настройки") must count as duplicates (Task 1).
- Title containing `]]`, `[` or a newline must be rejected, it would break `[[ ]]` links (Task 1).
- Moving a page under its own descendant (cycle) or under itself must be rejected (Task 1).
- `TRK-07` must not match a page that only mentions `TRK-070`; a code of a deleted or missing task is not a link (Task 2).
- Deleting a page with children re-parents them; renaming rewrites `[[старое]]` case-insensitively in sibling pages (Task 2).
- Purging a deleted project with pages must succeed (Task 1).

---

### Task 1: Store and service CRUD

**Files:**
- Create: `internal/store/migrations/003_pages.sql`, `internal/store/pages.go`, `internal/service/pages.go`, `internal/service/pages_test.go`
- Modify: `internal/store/queries.go` (`PurgeProject`, add `DELETE FROM pages WHERE project_id = ?` before tasks are deleted, and null `parent_id` first)

**Interfaces:**
- Produces (store): `type Page struct { ID, ProjectID int64; ParentID *int64; Title, BodyMD string; Position float64; CreatedAt, UpdatedAt string }` with json tags `id, project_id, parent_id, title, body_md, position, created_at, updated_at`;
  `(*Tx).ListPages(projectID int64) ([]Page, error)`, `GetPage(id int64) (Page, error)` (`ErrNotFound`), `PageByTitleKey(projectID int64, key string) (Page, error)` (`ErrNotFound`), `NextPagePosition(projectID int64, parentID *int64) (float64, error)` (max+1024, 1024 for the first), `InsertPage(p Page, titleKey string) (Page, error)`, `UpdatePage(p Page, titleKey string) error`, `DeletePage(id int64) error`, `ReparentPages(from int64, to *int64) error`, `PagesContaining(sub string) ([]Page, error)` (`LIKE '%'||escaped||'%' ESCAPE '\'`, any project).
- Produces (service): `type PageInput struct { Title, BodyMD string; ParentID *int64 }`; `type PagePatch struct { Title, BodyMD *string; ParentID *int64 /* 0 = top level */; ExpectedUpdatedAt string }`;
  `(*Service).ListPages(ctx, projectRef string) ([]store.Page, error)` (bodies blanked), `CreatePage(ctx, actor, projectRef string, in PageInput) (store.Page, error)`, `UpdatePage(ctx, actor string, id int64, p PagePatch) (store.Page, error)`, `DeletePage(ctx, actor string, id int64) error`; helper `titleKey(s string) string`; constructor `conflict(format string, a ...any) error` with Code `conflict`.

- [ ] **Step 1: Write failing tests** in `internal/service/pages_test.go`, using `newSvc`, `mustProject`, `ctx` from `service_test.go`: `TestPageTitleUniqueIgnoringCase` (create "Настройки", then "настройки" → error with Code `validation`); `TestPageTitleRejectsBrackets` (`"a]]b"`, `"a[b"`, `"a\nb"`, `""`, 121 chars → validation); `TestPageBodyTooLarge` (204801 bytes → validation); `TestPageParentRules` (parent from another project → validation; set A's parent to its child B → validation; set A's parent to A → validation; `ParentID` pointing to 0 detaches); `TestUpdatePageConflict` (update with `ExpectedUpdatedAt` of an older value → `Code == "conflict"`; with the current value → ok and `UpdatedAt` changes); `TestPurgeProjectWithPages` (create a page, `DeleteProject`, then the purge path used by the existing project-purge test → no error).
- [ ] **Step 2: Run** `go test ./internal/service -run 'Page' -count=1` → FAIL (undefined).
- [ ] **Step 3: Implement** migration (`pages` as in the spec plus `title_key TEXT NOT NULL`, `UNIQUE INDEX idx_pages_title ON pages(project_id, title_key)` instead of the `lower(title)` index), the store methods above, and the service methods. Each write goes through `s.write`, resolves the project with `resolveProject`, validates (limits above, parent same project, cycle check by walking parents from the new parent up), sets `UpdatedAt = nowTS()`, and ends with `o.emit("page_changed", 0, project.ID)`. `UpdatePage` compares `ExpectedUpdatedAt` (when non-empty) with the stored value and returns `conflict("page changed since %s; reload it and merge")`. Shortcut: `updated_at` has millisecond resolution, so two writes in the same millisecond cannot be told apart; fine for one user.
- [ ] **Step 4: Run** the same command → PASS.
- [ ] **Step 5: Commit** `feat(web): add wiki pages table and service` (add the files above).

### Task 2: Links, backlinks, rename, delete

**Files:**
- Modify: `internal/service/pages.go`, `internal/service/pages_test.go`

**Interfaces:**
- Consumes: Task 1 store/service methods.
- Produces: `type PageRef struct { ID int64 \`json:"id"\`; Title string \`json:"title"\` }`; `type TaskRef struct { Code, Title string }` (json `code`, `title`); `type PageDetail struct { store.Page; Backlinks []PageRef \`json:"backlinks"\`; Links struct { Pages []PageRef \`json:"pages"\`; Tasks []TaskRef \`json:"tasks"\` } \`json:"links"\` }`; `type TaskPage struct { ID int64; Title, Project string }` (json `id, title, project` = project slug);
  `(*Service).GetPage(ctx, id int64) (PageDetail, error)`; `GetPageByTitle(ctx, projectRef, title string) (PageDetail, error)`; `TaskPages(ctx, taskRef string) ([]TaskPage, error)`;
  `parseLinks(body string) (titles []string, codes []string)` using `wikiLinkRe = \[\[([^\[\]\n]+)\]\]` and `taskCodeRe = \b[A-Z][A-Z0-9]{1,4}-\d+\b` (deduplicated, titles keyed with `titleKey`).
- Behavior changes: `UpdatePage` with a new title rewrites `[[...]]` whose `titleKey` equals the old key to `[[New]]` in every other page of the project, in the same transaction (bumping their `updated_at`); `DeletePage` calls `ReparentPages(id, page.ParentID)` first.

- [ ] **Step 1: Write failing tests:** `TestParseLinks` (table: `"[[Настройки]] и [[настройки]]"` → one title; `"TRK-07 TRK-070"` → both codes; `"xTRK-07"` → none; empty `[[]]` → none); `TestBacklinksCaseInsensitive` (page B contains `[[настройки]]`, `GetPage(A "Настройки")` lists B in `Backlinks`, and not A itself); `TestTaskCodeBoundary` (page mentions only `TRK-070`; `TaskPages("TRK-07")` empty; with `TRK-07` present returns the page); `TestLinksResolveOnlyExisting` (code of a missing task and a trashed task are absent from `Links.Tasks`; a title with no page is absent from `Links.Pages`); `TestRenameRewritesLinks`; `TestDeletePageReparentsChildren`.
- [ ] **Step 2: Run** `go test ./internal/service -run 'Page|ParseLinks|Backlink|TaskCode|Rename' -count=1` → FAIL.
- [ ] **Step 3: Implement** as listed. Backlinks: parse every page body of the project with `parseLinks` and match `titleKey(page.Title)`. Task codes in `Links.Tasks` resolve with `resolveTask`, skipping errors and tasks with `DeletedAt != nil`; `TaskPages` uses `PagesContaining(code)` then verifies with `taskCodeRe`. Shortcut: links inside fenced code are counted on the server; upgrade when it matters.
- [ ] **Step 4: Run** the same command → PASS; then `go test ./internal/service -count=1`.
- [ ] **Step 5: Commit** `feat(web): wiki links, backlinks and rename rewrite`.

### Task 3: REST API

**Files:**
- Modify: `internal/httpapi/httpapi.go` (routes next to the `/projects/{id}/...` and `/tasks/{id}/...` groups, handlers near `addNote`, `statusFor`: `case "conflict": return 409`), `internal/httpapi/httpapi_test.go`

**Interfaces:**
- Consumes: Task 1–2 service methods; helpers `a.h`, `decode`, `pathID`, `created`, `none`, `service.ActorDenis`.
- Produces routes: `GET /projects/{id}/pages` → `[]store.Page` (no bodies); `POST /projects/{id}/pages` body `{title, body_md, parent_id}` → 201 page; `GET /pages/{id}` → `PageDetail`; `PUT /pages/{id}` body `{title?, body_md?, parent_id?, expected_updated_at?}` → page; `DELETE /pages/{id}` → 204; `GET /tasks/{id}/pages` → `[]TaskPage`.

- [ ] **Step 1: Write failing test** `TestPagesREST` in `httpapi_test.go`, following that file's existing request helpers: create project, `POST` page → 201; `GET` list has it with empty body; `PUT` with a stale `expected_updated_at` → 409 and body code `conflict`; `GET /pages/{id}` returns `backlinks`/`links`; `DELETE` → 204 and then 404; invalid title → 400.
- [ ] **Step 2: Run** `go test ./internal/httpapi -run TestPagesREST -count=1` → FAIL.
- [ ] **Step 3: Implement** the routes and handlers; absent `parent_id` means "unchanged", `0` means top level.
- [ ] **Step 4: Run** the same command, then `go test ./internal/httpapi -count=1` → PASS.
- [ ] **Step 5: Commit** `feat(web): wiki REST endpoints`.

### Task 4: MCP tools

**Files:**
- Modify: `internal/service/pages.go`, `internal/service/pages_test.go`, `internal/service/mcp.go` (four `MCPTools` entries after `add_note`), `internal/mcpserver/mcpserver.go` (`add(s, ...)` lines plus handlers)

**Interfaces:**
- Consumes: Task 1–2.
- Produces (service): `type WritePageInput struct { Title, BodyMD, Parent, ExpectedUpdatedAt string }` (`Parent` is a parent page title, "" = unchanged on update / top level on create); `(*Service).WritePage(ctx, actor, projectRef string, in WritePageInput) (page store.Page, created bool, err error)` (upsert by `titleKey`); `(*Service).AppendToPage(ctx, actor, projectRef, title, text string) (store.Page, error)` (`not_found` when missing; text appended after a blank line; empty text → validation).
- Produces (MCP): `list_pages{project}` (read) → `[{id,title,parent,updated_at}]`; `read_page{project,title}` (read) → `{id,title,parent,body_md,updated_at,backlinks:[titles],tasks:[codes]}`; `write_page{project,title,body_md,parent?,expected_updated_at?}` (write) → `{id,title,created,updated_at}`; `append_to_page{project,title,text}` (write) → `{id,title,updated_at}`. Descriptions are one sentence each, English, like the existing entries; `write_page` says it replaces the whole body (read first) and that links are `[[Title]]` and task IDs like TRK-07.

- [ ] **Step 1: Write failing tests:** `TestWritePageUpserts` (first call created=true, second replaces body and keeps ID, title with different case hits the same page, `Parent` resolves by title and unknown parent → validation); `TestWritePageExpectedUpdatedAt` (stale → `conflict`); `TestAppendToPage` (appends with blank line; missing page → `not_found`); `TestWritePageActorClaude` (works with `ActorClaude`); `TestIsWriteToolPages` (`write_page`, `append_to_page` true; `list_pages`, `read_page`, `delete_page` false).
- [ ] **Step 2: Run** `go test ./internal/service -run 'WritePage|AppendToPage|IsWrite' -count=1` → FAIL.
- [ ] **Step 3: Implement** service methods, catalogue entries, and handlers (inputs with `json` and `jsonschema` tags like `addNoteIn`). `mcpserver` panics at startup if an entry is missing, so register all four.
- [ ] **Step 4: Run** `go test ./... -count=1` (this also builds the MCP handler through the router tests) → PASS.
- [ ] **Step 5: Commit** `feat(mcp): wiki page tools`.

### Task 5: Wiki tab UI

**Files:**
- Create: `web/src/Wiki.tsx`
- Modify: `web/src/api.ts` (types `Page`, `PageRef`, `TaskRef`, `PageDetail`, `TaskPage`; `api.pages(project)`, `api.page(id)`, `api.createPage(project, body)`, `api.savePage(id, body)`, `api.deletePage(id)`, `api.taskPages(code)` through the existing `req`), `web/src/markdown.ts`, `web/src/Board.tsx`, `web/src/App.tsx`, `web/src/messages.ts`

**Interfaces:**
- Consumes: Task 3 routes (query keys `['pages', projectId]`, `['page', id]`).
- Produces: `export function Wiki({ project, pageId }: { project: Project; pageId?: number })`; in `markdown.ts` `export function renderWiki(src: string, ctx: { slug: string; pages: PageRef[]; tasks: TaskRef[] }): string`.
- `App.tsx`: route regex becomes `^/p/([^/]+)(?:/(settings)|/wiki(?:/(\d+))?)?$`; `Board` gets `wiki?: { id?: number }`; heading for the wiki route is `${project.name}: ${tr('вики')}`.
- `Board.tsx`: third `tab(tr('Вики'), 'doc', onWiki, () => navigate(`/p/${project.slug}/wiki`))`; the Board and List tabs navigate to `/p/${project.slug}` when on the wiki route; when `wiki` is set, render `<Wiki>` instead of `FilterBar` and the board/list sections (task queries stay).

- [ ] **Step 1: `markdown.ts`:** add `renderWiki` using a second `Marked` instance with two inline extensions: `[[Title]]` (resolved by lower-cased title against `ctx.pages` → `<a href="/p/{slug}/wiki/{id}" data-wiki="{id}">`, otherwise `<a href="#" data-wiki-new="{Title}" class="border-b border-dashed">`) and task codes (`(?<![A-Za-z0-9])[A-Z][A-Z0-9]{1,4}-\d+\b`, resolved against `ctx.tasks` → `<a href="?task={CODE}" data-task="{CODE}">`, otherwise plain text). Inline extensions do not run inside code spans or blocks. In the DOMPurify hook, do not set `target=_blank` on anchors that have `data-wiki`, `data-wiki-new` or `data-task`. Reuse `MD_PROSE` for styling.
- [ ] **Step 2: `Wiki.tsx`:** left column = tree built from the flat list (`parent_id`), page links, "Новая страница" button (creates "Новая страница N" and opens it); right column = `PageView` (rendered body, edit button, backlinks block "Ссылаются сюда", tasks block "Задачи", delete via `ConfirmButton`) and `PageEditor` (title input, textarea, "Редактор / Превью" toggle, "Сохранить" and ⌘S, `beforeunload` guard while dirty, a 409 `conflict` banner with "Загрузить новую" and "Сохранить поверх"). A delegated click handler on the rendered body handles `data-wiki` (navigate), `data-task` (`setParam('task', code, false)`) and `data-wiki-new` (create the page with that title, then open it). Empty states via `EmptyState`; loading via `Skeleton`. Saving sends `expected_updated_at` from the loaded page.
- [ ] **Step 3: Wire `App.tsx` and `Board.tsx`** as in Interfaces; add en and uk entries for every new `tr()` string to `messages.ts`.
- [ ] **Step 4: Verify** `make typecheck` and `make knip` print no errors.
- [ ] **Step 5: Verify in the browser:** `make dev-api` and `make dev-web` (separate tabs), open the project, create a page, add `[[Другая]]` and a real task code, check: link click opens the page, missing link creates it, backlinks show on the target, task chip opens the drawer, rename rewrites links, editing the same page in two tabs yields the conflict banner, Board/List tabs still work. Report what was not checked.
- [ ] **Step 6: Commit** `feat(web): wiki tab with editor and links`.

### Task 6: Task panel block, docs, final check

**Files:**
- Modify: `web/src/TaskPanel.tsx` (a block "Упомянута на страницах" below notes, list of `Link`s to `/p/{project}/wiki/{id}`, hidden when empty, using `api.taskPages(code)`), `web/src/messages.ts`, `README.md` (MCP tool table next to `add_note`; REST list), `skills/docket-reference/SKILL.md` (four tool rows in the tool table, a short "Wiki" section: `[[Title]]`, task IDs, `write_page` replaces the whole page, no delete tool)

- [ ] **Step 1: Implement** the task-panel block and doc rows.
- [ ] **Step 2: Run** `make check`; report the exact output (typecheck, Knip, Go tests). If the sandbox blocks the local port, say that instead of claiming tests passed.
- [ ] **Step 3: Commit** `docs: document wiki pages and tools` (panel block in its own `feat(web): show wiki pages on a task` commit).
