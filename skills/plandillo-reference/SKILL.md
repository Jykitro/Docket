---
name: plandillo-reference
description: Use when choosing a Plandillo MCP tool, a plandillo-cli command or their arguments, editing a task or wiki page without losing work, publishing HTML, paginating results, or handling a failed write.
---

# Plandillo tools reference

Every tool below works the same over MCP and through `plandillo-cli` (see **Use the CLI**). Use the schema advertised by the connected server. Tool names below are the names on the wire. Clients may prefix them with the connection name. The workspace selected during OAuth approval, or the workspace that issued a manual token, determines which data you can access. A client label such as `codex` identifies the author in history.

## Read the right record

| Tool | Arguments | Result and use |
|---|---|---|
| `list_projects` | `{}`, optionally `include_archived` | Key, slug, name and task counts by status. Resolve the user's project before writing. |
| `get_today` | `{}` for today or `date` as YYYY-MM-DD | Server-local `date`, planned tasks grouped in `projects`, and earlier open tasks in `overdue`. |
| `get_board` | Optional `project`, `status`, `area`, `planned_for`, `include_done` | Tasks grouped by status key. Done-category tasks are hidden by default. An explicit status selects that column. Fetch details to see checklists. |
| `search` | `q`, optional `limit` (default 20) | Prefix search of task titles and descriptions across projects, or lookup by ID; digits alone (`7`) list tasks with that number, then numbers starting with it, in every project. Match by project and status before reusing a result. For wiki titles use `list_pages`. |
| `get_task` | `id`, e.g. `TRK-07` | Latest description, custom fields, subtasks, notes, document headers and up to 20 recent history entries. |
| `list_pages` | `project` | Wiki headers including title and numeric ID. |
| `read_page` | `project`, `title`, optional `document_offset` | Matches title case-insensitively. Returns the page ID, `body_md` or `body_html`, `updated_at`, backlinks, linked tasks and document headers. |
| `read_html` | Numeric document `id`, optional `version` | Source and metadata of a versioned attachment. Get its ID from the owner's `documents` list. |

**Pagination.** `list_projects`, `get_board`, `get_today`, `get_task` and `list_pages` take `limit` (1–100, default 100) and `offset` (default 0). Advance by the requested limit while a list fills its page. Count board tasks across columns. For Today, check the total across planned groups and the separate overdue list. For task details, check notes, subtasks and attachments separately. On `read_page`, advance `document_offset` by 100 for further attachments or backlinks.

## Change tasks

| Tool | Arguments | Effect and constraints |
|---|---|---|
| `create_project` | `name`, optional `key`, `slug`, `icon`, `color`, `description` | Create a project when requested. The server can derive the key. The UI handles renaming and archiving. |
| `create_tasks` | `tasks` array, each item needs `project` and `title` | 1–100 items in one transaction. Returns IDs and titles in input order. Use `ref` on an epic and `parent_ref` on later children in the same batch, or `parent` for an existing epic. One subtask level within one project. |
| `update_task` | `id` and changed fields, e.g. `description_md`, `title`, `priority`, `area`, `custom`, `planned_for`, `parent` | Omitted fields stay. An empty date clears the plan date, an empty parent detaches a subtask, and a null custom value clears that field. Read the latest description before replacing it. |
| `move_tasks` | `ids` array and at least one of `status`, `planned_for`, `project` | 1–100 IDs; a repeated ID (code or number) counts once. Returns the count of tasks that actually changed: a task already in the target status, date and project is skipped. Moving across projects gives tasks new codes and moves an epic's children too. Look up the new codes in the destination board. When an agent moves or creates a task in a done-category status, the owner can receive a push notification, so do it only after the checks passed. |
| `add_note` | `task_id`, `body_md` | Append a decision or result to the task. Keep changes to the plan in the description. |
| `report_issue` | `project`, `title`, `observation`, optional `source_task`, `existing_issue`, `kind`, `priority` | Returns the issue ID and whether it was created or evidence was added. Same open title within a project is reused. Kinds are `problem`, `unverified`, `decision`, `blocker`. |
| `carry_over` | `from`, `to` as YYYY-MM-DD | Moves **all** open tasks planned for the source date across the workspace. For specific tasks, use `move_tasks` with their IDs. |
| `delete_tasks` | `ids` array | 1–100 IDs. Moves tasks and their children to trash when the user requests deletion. Off by default. The owner can restore them in the UI. |

**Fields.** Project arguments accept a key, slug or ID. Status keys are project-specific. New projects start with `backlog`, `todo`, `doing`, `done`. If `backlog` has been removed, new tasks default to the first status. Priorities are `P0`, `P1` (default), `P2`. An unfamiliar `area` creates a label. Pass configured custom fields by name, such as `{"Estimate": 3}`. Keep tokens and credentials out of task records.

**Create an epic.** Pass this object to `create_tasks` after resolving the project. A failed item rolls back the entire batch. For an import with a stable external key, `external_id` matches across projects in the workspace. Resending the same import updates the matching task, so reuse the original payload. Use `update_task` for partial edits.

```json
{"tasks":[
  {"project":"TRK","title":"Export the board","ref":"export","description_md":"- [ ] Implement CSV export\n- [ ] Verify escaped fields"},
  {"project":"TRK","title":"Verify exported CSV","parent_ref":"export"}
]}
```

**Resume a checklist.** Read `get_task({"id":"TRK-07"})`, change the relevant checkbox in the returned Markdown and pass the complete text back as `description_md`. A task description has no revision guard. If concurrent edits are likely and you only need to record results, append a note. After a lost write response, inspect the task before retrying a create or appending a second note.

## Write wiki pages and HTML

| Tool | Arguments | Effect and constraints |
|---|---|---|
| `append_to_page` | `project`, `title`, `text` | Append a section to an existing Markdown page. |
| `write_page` | `project`, `title`, exactly one of `body_md` or `body_html`, optional `parent`, `expected_updated_at` | Create or replace the entire body. For an edit, read the page, preserve its format, merge the requested changes and pass its exact timestamp back. |
| `publish_html` | Exactly one of `task` or numeric `page_id`, plus `title`, `html`, optional `expected_version` | Attach a self-contained document. For a new title omit the version or pass 0. To revise, use the same owner and title with the current version from `read_html`. |

Use `[[Title]]` to link wiki pages and a task code to link an existing task. For an interactive block within Markdown use a top-level `html-preview` fence. For a whole HTML wiki page use `write_page` with `body_html`. For a separately versioned attachment use `publish_html`. Inline CSS, scripts and data in the HTML. A Markdown page holds up to 200 KiB, an HTML page or attachment up to 2 MiB. Versioned attachments share a workspace history budget of 64 MiB or 4096 revisions. To choose a layout, a table or a chart for the reader, use `plandillo-infographics`.

## Use the CLI

`plandillo-cli` is a shell client for the same board: an agent without an MCP connection, a script or CI can call every tool in the tables above with the same names, arguments, results and errors. It connects to the board's `/mcp` (and `/mcp/code` for `code`), so access mode, tool switches, the call log and attribution apply exactly as over MCP. Install and sign in with `plandillo-setup`.

| Command | Use |
|---|---|
| `plandillo-cli TOOL [JSON] [--ARG VALUE]` | Call any tool, e.g. `get_today`. Pass arguments as one JSON object, as `@file.json`, as `-` (stdin), or as `--ARG VALUE` for top-level fields; options override JSON keys. Values follow the schema: `--limit 5` is a number, a bare `--include_done` is true, `--planned_for=` sends an empty string, arrays and objects are JSON (`--ids '["TRK-07"]'`). A missing required argument fails before the call. |
| `plandillo-cli tools [--json]` | Tools this sign-in may call, marked `read` or `write`; `--json` adds each argument schema. Read-only mode and switched-off tools are not listed. |
| `plandillo-cli code JS` | Run Code Mode JavaScript (also `@file.js` or `-`). Needs a token for `/mcp/code`: a workspace token, or `login --code`. |
| `plandillo-cli skills [update]` | Compare the Plandillo skills installed for Claude Code and Codex with the CLI's own version (`to_update` counts outdated and missing files); `update` rewrites them, `--agent claude-code|codex` or `--dir DIR` adds a folder. `status` shows the same check in its `skills:` line. |
| `plandillo-cli status` | Board address, sign-in kind, access mode and tool list. Run it first when a call fails with exit 3. |
| `plandillo-cli login [URL]`, `logout` | Browser sign-in, `--with-token` reads a workspace token from stdin; `logout` deletes the saved sign-in. |

Put global options before the command: `--url URL` picks the board, `--agent NAME` sets the author shown in task history (default `plandillo-cli`), `--pretty` indents JSON for people. The same settings come from `PLANDILLO_URL`, `PLANDILLO_AGENT`, `PLANDILLO_TOKEN` (a workspace token that overrides the saved sign-in) and `PLANDILLO_CLI_CONFIG` (another config file, e.g. one per board).

```sh
plandillo-cli --agent claude get_today
plandillo-cli search --q "release checklist" --limit 5
plandillo-cli create_tasks @tasks.json
plandillo-cli move_tasks --ids '["TRK-07","TRK-08"]' --status done
plandillo-cli add_note --task_id TRK-07 --body_md "Tests pass on macOS and Linux."
echo '{"project":"TRK","title":"Release notes","text":"## 1.2\n- CLI"}' | plandillo-cli append_to_page -
plandillo-cli code 'return (await codemode.get_board({project: "TRK"})).doing?.length ?? 0'
```

stdout carries only the tool result (compact JSON), so it can be piped into `jq`. Messages go to stderr; a failed tool prints `code: message` with the codes from the error table below.

| Exit | Meaning | Next step |
|---|---|---|
| 0 | Success | Parse stdout. |
| 1 | The tool returned an error | Handle the `code:` prefix as over MCP. |
| 2 | Wrong command, option or JSON | Read the message: it lists the valid options. |
| 3 | `unauthorized` or `forbidden` | Run `status`; sign in again, or report the missing access. |
| 4 | The board did not answer | Check the URL and that the board is running. |

## Code Mode and errors

At `/mcp/code` the client exposes one `code` tool. Pass JavaScript in the `code` string. For example, to return the IDs of overdue P0 tasks:

```json
{"code":"const day = await codemode.get_today({}); return day.overdue.filter(t => t.priority === 'P0').map(t => t.id)"}
```

The response includes the return value in `result` and any console output in `logs`. The runtime allows 100 calls in 30 seconds and a 50,000-byte result. Filter before returning. The runtime has no network, file or timer APIs. Each inner tool call has its own policy check and transaction. If a later call throws or times out, earlier writes can already be committed.

| Error | How to proceed |
|---|---|
| `invalid_argument` | Correct the named field. An unknown status error includes valid keys. A failed batch can identify the zero-based item index. |
| `not_found` | Check the workspace, task code or page title. A task may have moved to another project. A task in the trash, or in a project in the trash, is not found until the owner restores it. |
| `conflict` | Reread the current page or HTML revision. Merge the requested edit with current content and submit the fresh timestamp or version. A deleted or renamed page needs a new decision. An `external_id` whose task is in the trash is not reused: ask the owner to restore the task, or use a new `external_id`. |
| `forbidden`, `tool_disabled` | Report the blocked operation. Read-only mode hides writes, and the owner can disable individual tools. |
| `page_too_large` | Reduce `limit`, keeping the same `offset`. |
| `quota_exceeded` | Tell the owner which quota the service reported. |
| `timeout`, `rate_limited`, `unavailable`, `internal` | Check whether a write landed before retrying. If the isolated worker is unavailable, consult `plandillo-setup`. |

For an HTTP 401 or 403 during connection, use `plandillo-setup`.
