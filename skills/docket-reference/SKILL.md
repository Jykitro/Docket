---
name: docket-reference
description: Use when unsure how a Docket tool, field, status key, task ID, date or error code behaves, or which project, area or priority to use, before guessing at an MCP call.
---

# Docket reference

Docket is the user's personal task planner. You reach it over MCP as the actor `claude`; the user works in the same data through the web UI and sees your changes live. The workflow you follow (task per piece of work, plan, notes, issues, end checklist) is the `docket` skill. This file answers "how does X behave".

## Data model

- **Project**: `key` 2-5 characters, A-Z first then A-Z or 0-9 (`TRK`); `slug` a-z, 0-9, `-`; `name`; optional `icon`, `color` (`#rrggbb`), `description`. Task IDs are `KEY-NN` (`TRK-07`). Numbers are never reused. A key change renumbers every ID.
- **Status**: per project, addressed by `key`. A new project has `backlog` (Бэклог), `todo` (К работе), `doing` (В работе), `done` (Готово). Each status belongs to a category (todo, doing, done); the user can rename, add or remove statuses, so never assume `doing` exists: an unknown key returns the project's real keys in the error.
- **Task**: `title`, `description_md` (markdown; `- [ ]` and `- [x]` render as clickable checkboxes), `status`, `priority` (`P0` highest, `P1` default, `P2`; any other value fails with `priority must be one of P0, P1, P2`), `area` (free label up to 40 characters, an unknown one is created as a label), `planned_for` (`YYYY-MM-DD`, server time zone), `custom` fields (types: number, url, select) passed as `{"Field name": value}`, `parent`. There is no tool that lists custom fields: `get_task` shows a task's values by name, and an unknown name fails with `unknown custom field "x"`.
- **Epic and subtasks**: one level only. A subtask lives in its epic's project. A task that has subtasks cannot become a subtask. Moving an epic moves its subtasks.
- **Notes**: markdown comments on a task. **History**: every change is logged with its actor (`claude` or the user); `get_task` returns the last 20 entries.
- **Trash**: deleting moves tasks (with subtasks) to the trash; only the user restores them or empties the trash in the UI.
- **Done-category tasks** are hidden from boards by default but stay in `get_today` for their planned date.

## Tools

Answers are compact JSON. IDs always look like `TRK-07`; `project` arguments accept a key, a slug or an id.

| Tool | Use it for | Behaviour you must know |
|---|---|---|
| `list_projects` | Find the project key | `include_archived` optional. Returns key, slug, name, open count, counts per status key |
| `get_board` | Tasks of a project, by status | Filters: `project`, `status`, `planned_for`, `area`. Excludes done-category unless `include_done: true` (ignored when `status` is given). No descriptions: use `get_task`. Without `project` each task carries its project slug |
| `get_today` | The day's plan | `date` defaults to server today. Tasks planned for the date, grouped by project, plus `overdue` open tasks planned earlier. Use the returned `date`, don't compute your own |
| `get_task` | Full context of one task | Description, subtasks, notes, custom fields, last 20 history entries. Read it before you replace a description |
| `search` | Find a task or a duplicate | Prefix match on titles and descriptions; also finds by ID. `limit` default 20. Returns id, title, status, snippet |
| `create_project` | New project | Only when the user asked. `key` is derived from the name when omitted. No tool renames, re-keys, archives or deletes a project: ask the user to do that in the UI |
| `create_tasks` | New tasks | 1 to 100 per call, one transaction (any invalid item fails the whole call, reported as `tasks[2]: <reason>` with the item's index). Every item needs its own `project`, subtasks too. Defaults: status `backlog`, priority `P1`. New tasks go to the end of their column. Epic: give it `ref`, subtasks `parent_ref` = that ref, epic first. Returns `[{id, title}]` in input order. `external_id` (global across projects) makes the call idempotent: repeating it updates that task instead of duplicating, and a repeat without `description_md` keeps the old description. Use it when you might retry a batch |
| `update_task` | Edit fields | Omitted fields stay. `description_md` replaces the whole text. `planned_for: ""` clears the date, `parent: ""` detaches. Status and project are not here |
| `move_tasks` | Status, date, project of many tasks | Give at least one of `status`, `planned_for`, `project`. Moved tasks go to the end of the target column. Moving to another project assigns new IDs and subtasks follow their epic. The answer is only `{"updated": n}`: `search` by title to learn the new IDs |
| `add_note` | Comment on a task | `task_id`, `body_md` |
| `carry_over` | Roll a day forward | Moves open tasks planned for `from` to `to`. Only when the user asks |
| `delete_tasks` | Trash tasks | Off by default. Never use it on the user's tasks unprompted |

An epic with two subtasks in one call (epic first; the subtasks name its `ref`):

```json
{"tasks": [
  {"project": "TRK", "title": "Export the board to CSV", "status": "doing", "planned_for": "2026-11-06",
   "ref": "epic", "description_md": "- [ ] Button in the column header\n- [ ] CSV escaping\n- [ ] Tests"},
  {"project": "TRK", "title": "CSV escaping", "parent_ref": "epic"},
  {"project": "TRK", "title": "Button in the column header", "parent_ref": "epic", "area": "UI"}
]}
```

## Errors

Errors read `code: message`. Never retry the same call blindly.

| Code | Meaning | What to do |
|---|---|---|
| `invalid_argument` | Validation failed; the message names the field | Fix the argument. `unknown status "x" (this project has: a, b)` lists the valid keys |
| `not_found` | No such task or project | Check the ID with `search`; the key may have changed |
| `forbidden` | Docket is in read-only mode | Don't retry. List the intended updates in your final message |
| `tool_disabled` | The user switched this tool off | Same as `forbidden` |
| `status_in_use` | A status still holds tasks (admin calls) | Not expected over MCP |
| `internal` | Server fault | Retry once; if it repeats, tell the user and check `docket-setup` |

A 401 or 403 on connect, or missing tools, is a setup problem: use `docket-setup`.

## Conventions

**Issue** (a problem you notice but won't fix now): `create_tasks`, `area: "issue"`, status `backlog`, no date, `priority` `P1` unless it is clearly urgent (`P0`) or cosmetic (`P2`). Title states the problem, not the fix. Description:

```
Symptom: <what goes wrong>
Where: <file or screen>
How found: <what you were doing>
Found while: TRK-12
```

**Plan** (in the working task's description):

```
- [ ] Read how the board query is built
- [ ] Pass `area` through the request and the handler
- [ ] Run tests and the build
```

**Result note** (the `add_note` at the end): what changed, which checks ran, what is unchecked, follow-ups as IDs. Short and factual. Write it in the user's language.

**Titles**: imperative or plain noun phrase, the user's language, no ID in the title. **Areas**: reuse labels already on the board (`get_board` shows them) before inventing one.

## Questions you will have

- **Which project?** `list_projects`, match by key, slug or name. None fits: ask the user. Don't create projects on your own.
- **Is there already a task for this?** `search` with two or three distinctive words, then `get_board` for `doing` and `todo`. Reuse an open task; don't open a twin.
- **The matching task is already done.** If the user wants more work on it, create a new task whose description says "follows TRK-12". Reopen with `move_tasks` only when the user says to, and add a note why.
- **Where do I put a big job?** One epic (`ref`) with subtasks (`parent_ref`) in a single `create_tasks`. Keep the checklist in the epic's description.
- **What date?** Work you start now: `planned_for` today, taken from `get_today`'s `date`. Ideas and issues: no date.
- **How do I tick a step?** `get_task`, change `- [ ]` to `- [x]`, `update_task` with the whole description.
- **What counts as verified?** Your own checks ran and passed (tests, build, type check) and the note lists what you did not check. A manual check you could not do goes under "unchecked".
- **Can I put secrets in a task?** No. Descriptions, notes and exports are stored as plain text. Never write tokens, passwords or keys. If you find one in code or config, file an issue that says where it is ("an API token is committed in `x.env`") without the value, and tell the user in the final message.
- **Can I change a project (rename, key, archive, delete)?** No tool does. Say what you would change and ask the user to do it in the UI.
- **Several users (selfhost mode)?** Each user has their own database and their own token; your token selects the profile. You cannot see anyone else's data.
- **A tool I need is missing.** The user switched it off, or Docket is read-only. Say so in the final message; don't work around it.
- **The user said "just do it, no tracker".** Honour the speed, not the skip: one `create_tasks` with `status: done` and a one-line description costs one call.
