---
name: plandillo
description: Use when the user asks about their Plandillo board or wants work planned, tracked, resumed or reconciled in a Plandillo project, through MCP or plandillo-cli.
---

# Plandillo workflow

Use the user's language. For a question, read only. If the user opts out of tracking, skip planner writes. Use the connected MCP tools. Without them, run the same tool in a shell: `plandillo-cli get_task --id TRK-07` or `plandillo-cli create_tasks '<json>'` (JSON on stdout, exit 3: sign in). On `/mcp/code`, send JavaScript to `code`: `return await codemode.get_today({})`.

## Find the work

- Today or overdue: `get_today`. Project: `get_board`. Known ID: `get_task`. Otherwise `search`, then read the match.
- Reuse the open task in the right project. If none fits, resolve the project with `list_projects` (ask if ambiguous) and `create_tasks` with a Markdown checklist in `description_md`. Work starting today gets the date from `get_today` and the active status (normally `doing`). Change status with `move_tasks`.

## Keep the record current

- Read the latest `get_task` before `update_task` replaces the checklist; keep the user's edits. Use `add_note` for decisions, obstacles and results.
- Record each bug, risk or decision you notice at once with `report_issue`, without asking: project, title, observation, `source_task`. Use `kind: "unverified"` for a suspicion, `existing_issue` to add evidence. Keep the ID.
- At handoff, tick verified steps and note the checks and what remains. Move to the completed status only when the work and its checks passed: the owner may get a notification. A small finished item can be one `create_tasks` call.

## Offer the next step

After tracked work, check `get_board` or `get_today`. Report touched task and issue IDs, name the most urgent open task (P0, overdue, then today's plan) and offer to take it, plus one way to work faster (batching, Code Mode). Do not start it unasked.

`plandillo-reference`: arguments, CLI, wiki edits, conflicts. `plandillo-infographics`: clear pages, tables and HTML visuals. `plandillo-setup`: connection and install. On 401, 403, `forbidden` or `tool_disabled`, report the limit and leave the change pending.
