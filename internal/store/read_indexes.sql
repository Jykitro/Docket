-- These indexes also apply when an installation already has the shared tables.
CREATE INDEX IF NOT EXISTS idx_tasks_children ON tasks(workspace_id,parent_id,status_id)
    WHERE parent_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_pages_tree ON pages(workspace_id,project_id,position,id);
CREATE INDEX IF NOT EXISTS idx_notes_task_id ON notes(workspace_id,task_id,id);
CREATE INDEX IF NOT EXISTS idx_html_revisions_workspace ON html_revisions(workspace_id);
-- The UNIQUE(workspace_id,id) constraint already covers this exact lookup.
DROP INDEX IF EXISTS idx_mcp_calls_workspace_id;
