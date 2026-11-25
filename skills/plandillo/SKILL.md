---
name: plandillo
description: Use when the user asks about their Plandillo board or wants work planned, tracked, resumed or reconciled in a Plandillo project.
---

# Plandillo workflow

Use the user's language. For a question, read only. If the user opts out of tracking, skip planner writes. Use the tools exposed by the connected workspace. On `/mcp/code`, send JavaScript to `code`, e.g. `return await codemode.get_today({})`.

## Find the work

- Today's plan or overdue work: `get_today`. Project overview: `get_board`. A known task ID: `get_task`. Otherwise `search` by distinctive words, then read the matching task.
- For tracked work, reuse the open task in the right project. If none fits, use `list_projects` to resolve the project, then `create_tasks` with a Markdown checklist in `description_md`. Ask if the project is ambiguous. For work starting today, use the date from `get_today` and the project's active status (normally `doing`). Change an existing task's status with `move_tasks`.

## Keep the record current

- Read the latest `get_task` before using `update_task` to replace the whole checklist. Preserve the user's edits. Use `add_note` for decisions, obstacles and results.
- Record a finding with `report_issue`: project, title, observation and `source_task` when known. Use `kind: "unverified"` for a suspicion or `existing_issue` to add evidence to a known open issue. Keep its ID.
- At handoff, tick verified steps and record the checks and remaining work. Move to the project's completed status only when the requested work and checks passed. Check your active tasks with `get_board`; report touched task and issue IDs. A small completed item can be recorded in one `create_tasks` call.

Use `plandillo-reference` for tool arguments, wiki edits and conflicts. Use `plandillo-setup` for connection or installation issues. On 401, 403, `forbidden` or `tool_disabled`, report the limitation and leave the intended change pending.
