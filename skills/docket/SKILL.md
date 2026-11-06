---
name: docket
description: Use when starting, planning, finishing or pausing any work in a project tracked in Docket, or when you notice a bug, risk or follow-up while working. Docket is the user's personal planner and you keep it true.
---

# Docket: keep the planner true

Every piece of work has a task: the task is the record, not the diff or your chat. Use the user's language.

**Start**
1. `search` the request. Found: `get_task`, then `move_tasks` to `doing`. Missing: `create_tasks` once with the plan in `description_md`, `status: doing`, `planned_for` today, right project (`list_projects`; none fits: ask, don't create one).
2. The plan is `- [ ]` steps in `description_md`. A changed plan edits the list, never a second task.

**During**
- Decision, obstacle or scope change: `add_note`. Tick finished steps in one `update_task` (`get_task` first: it replaces the whole text), at a note or at the end, not one call per step.
- Bug, risk, flaky test or follow-up you won't fix now, even if unsure it is real: `search` for a duplicate, else `create_tasks` with `area: "issue"`, backlog, no date; description = symptom, where, how found, "found while TRK-nn". `add_note` the new ID on the current task. Never leave it only in chat.

**End** (before every final message)
1. Every step ticked or moved to its own task. `add_note` the result: what changed, which checks ran, what is unchecked.
2. `move_tasks` to `done` only when your checks ran and passed; otherwise stay `doing` and say why in the note.
3. `get_board` status `doing` has nothing of yours stale.
4. The final message lists task IDs touched and issues filed.

Tiny work: one `create_tasks` with `status: done`, one-line description. Hurry means one-line notes, same calls.
`forbidden`, `tool_disabled`, 401, 403: don't retry or work around; say it in the final message (`docket-setup`). Unsure how a tool or field behaves: `docket-reference`.

| Excuse | Reality |
|---|---|
| "Too small, the diff is the record" | The user reads Docket, not git. It is one call. |
| "Nobody sees `doing`" | Today shows it, and a crash leaves a trail. |
| "I'll file it at the end" | Late is forgotten. Write at the moment. |
| "Not sure it's a real problem" | File it with what you know. The user triages. |
