# REST API

All requests are performed on behalf of the human actor (stored as `denis`; the UI shows the profile name). Errors come as
`{"error": {"code", "message"}}` (`validation` 400, `not_found` 404, `status_in_use` 409, `conflict` 409, `forbidden` 403). A task in the
path can be given by code (`TRK-07`) or by id.

## Accounts and workspaces

In selfhost, an authenticated account owns its workspaces. In local mode the same routes use the installation's local owner. The browser opens `/{slug}/`, where `slug` is the workspace's `slug` field. Workspace routes `/api/workspaces/{ref}/…` accept the slug or the numeric `id`; a slug is never all digits. Use `GET /api/workspaces` to list active and archived entries. Create with `POST /api/workspaces`, edit with `PATCH /api/workspaces/{id}`, and send `{"action":"archive"}` or `{"action":"restore"}` to change state. To delete an archived space, send its name and selfhost password with `DELETE /api/workspaces/{id}`.

Prefix planner requests with `/api/workspaces/{ref}/`, including `projects`, `tasks`, `events` and `export`. The existing unscoped planner endpoints select the owner's first active workspace. Authentication, passkeys, TOTP and device sessions live under `/api/auth`. MCP selects a workspace by its bearer token.

| Account endpoint | Purpose |
|---|---|
| `PUT /api/auth/profile`, `PUT /api/auth/photo` | Change the display name or profile thumbnail for every workspace. |
| `GET /api/auth/sessions`, `DELETE /api/auth/sessions/{id}` | List or revoke the current owner's device sessions. |
| `GET /api/auth/totp`, `POST /api/auth/totp/{begin,confirm,disable}` | Check availability and manage authenticator enrollment with password confirmation. |
| `POST /api/auth/login/totp` | Complete password sign-in with the challenge from HTTP 202 and a six-digit code. |

## Planner endpoints

Paged project, task, trash and Today reads accept `limit` and `offset` (up to 100 rows per page). Use task summaries for lists. Large detail pages may require a smaller page size. Export streams the full snapshot.

| Method and path | Purpose |
|---|---|
| `GET/POST /api/projects`, `PATCH/DELETE /api/projects/:id` | Projects; `POST` takes `lang` (`ru`, `en`, `uk`; empty means `ru`) for the default status names; `DELETE` moves a project to the trash |
| `POST /api/projects/:id/archive`, `POST /api/projects/:id/restore`, `GET /api/projects/:id/export` | Archive, restore from the trash, JSON export |
| `GET/POST /api/projects/:id/statuses`, `PATCH/DELETE /api/statuses/:id`, `POST …/statuses/reorder` | Statuses; `DELETE ?move_to=<id>` moves the tasks |
| `GET/POST /api/projects/:id/labels`, `PATCH/DELETE /api/labels/:id` | Area labels |
| `GET/POST /api/projects/:id/fields`, `PATCH/DELETE /api/fields/:id` | Custom fields (`number`, `url`, `select`) |
| `GET/POST /api/views?project=`, `PATCH/DELETE /api/views/:id` | Saved views |
| `GET /api/today?date=` | Tasks for a date, overdue tasks and recent activity |
| `GET /api/tasks?project=&status=&planned_for=&area=&created_by=&q=&parent=&include_done=1` | Task list |
| `POST /api/tasks`, `GET/PATCH/DELETE /api/tasks/:id` | CRUD; `planned_for: ""` clears the date. A create with `client_key` (up to 64 characters, e.g. an offline draft id) runs once: a repeat returns the existing task unchanged |
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
| `GET/PUT/DELETE /api/push` | Push notifications for this device: `GET` says whether push is on and gives the VAPID key, with the device's settings when the `X-Push-Endpoint` header names its subscription; `PUT` saves `{endpoint, p256dh, auth, agent_done, morning, tz, lang}`; `DELETE {endpoint}` unsubscribes |
| `GET /api/events` | SSE: `{type, task_id?, project_id?, actor}`. At most 16 open streams per workspace and 1024 per process; over that, `429 too_many_streams` with `Retry-After` |
| `GET /api/export`, `GET /healthz` | Full dump, liveness check |
