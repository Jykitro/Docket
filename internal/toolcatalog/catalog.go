// Package toolcatalog lists the tools available to planners and isolated evaluators.
package toolcatalog

// ToolInfo describes one MCP tool for tools/list and for the settings screen.
type ToolInfo struct {
	Name        string `json:"name"`
	Group       string `json:"group"` // read | write
	Description string `json:"description"`
}

// MCPTools is the single catalogue of tools: mcpserver registers exactly these.
var MCPTools = []ToolInfo{
	{"list_projects", "read", "List projects with their key (TRK), task counts per status key and open tasks."},
	{"get_board", "read", "Tasks grouped by status key. Filters are optional; done-category tasks are excluded unless include_done. " +
		"Descriptions are not included: use get_task."},
	{"get_today", "read", "Plan for a day (default: today): tasks planned for the date grouped by project, plus overdue open tasks planned earlier."},
	{"get_task", "read", "Full task by ID such as TRK-07: description, subtasks, notes, custom fields and the last 20 history entries."},
	{"search", "read", "Full-text search in task titles and descriptions (prefix match). Also finds a task by its ID."},
	{"create_project", "write", "Create a project. key (2-5 chars like TRK), icon, color and description are optional."},
	{"create_tasks", "write", "Create up to 100 tasks in one transaction. To create an epic with subtasks, give the epic a ref and the " +
		"subtasks parent_ref = that ref (the epic must come first). Items with an external_id that already exists update that task " +
		"instead of duplicating it. status is a status key (default backlog); an unknown area becomes a new label. New tasks go to the end of their column."},
	{"update_task", "write", "Edit task fields by ID such as TRK-07. Omitted fields stay as they are. planned_for \"\" clears the date, " +
		"parent \"\" detaches from the parent. custom is keyed by field name. Status and project change via move_tasks."},
	{"move_tasks", "write", "Change status (key), planned date and/or project of up to 100 tasks at once; each task changes once. Moved tasks go to the end of the " +
		"target column. planned_for \"\" clears the date."},
	{"publish_html", "write", "Publish a versioned interactive HTML document on a task or wiki page."},
	{"read_html", "read", "Read a saved HTML document, including source, by ID and optional version."},
	{"report_issue", "write", "Immediately record a finding, link its source task, or add evidence to an existing open issue. Returns the issue ID."},
	{"add_note", "write", "Add a markdown note to a task."},
	{"list_pages", "read", "List the wiki pages of a project (id, title, parent page title, updated_at) without bodies."},
	{"read_page", "read", "Read a wiki page by title: Markdown or HTML body, parent, updated_at, backlinks (titles of pages linking here) and linked task IDs."},
	{"write_page", "write", "Create a wiki page or replace the whole body of the page with that title (read it first so nothing is lost); " +
		"link pages as [[Title]] and tasks by ID such as TRK-07, and pass the updated_at you read as expected_updated_at so a human's newer edit is not overwritten."},
	{"append_to_page", "write", "Append markdown to the end of an existing Markdown wiki page, after a blank line, without reading it first."},
	{"carry_over", "write", "Move all open tasks planned for date `from` to date `to`."},
	{"delete_tasks", "write", "Move up to 100 tasks (with their subtasks) to the trash. The owner can restore them. Disabled by default."},
}
