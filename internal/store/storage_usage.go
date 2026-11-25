package store

import "strings"

// Measure uncompressed PostgreSQL row payloads, including attachments, history and
// trash. Cleanup metadata and the operational MCP log do not consume demo storage.
var workspaceUsageSQL = func() string {
	var totals []string
	for _, table := range []string{"projects", "statuses", "labels", "custom_fields", "tasks", "saved_views", "notes", "activity", "settings", "pages", "issue_reports", "html_documents", "html_revisions"} {
		bytes := "octet_length(pg_catalog.record_send(r))"
		filter := ""
		switch table {
		case "projects", "tasks":
			bytes += " - coalesce(octet_length(deleted_at), 0)"
		case "activity":
			filter = " WHERE action NOT IN ('deleted', 'project_deleted', 'html_deleted')"
		case "settings":
			filter = " WHERE key <> 'mcp_token_last_used_at'"
		}
		totals = append(totals, "(SELECT coalesce(sum("+bytes+"),0) FROM "+table+" r"+filter+")")
	}
	return "SELECT " + strings.Join(totals, " + ")
}()

func (t *Tx) WorkspaceBytes() (int64, error) {
	var used int64
	err := t.tx.QueryRow(workspaceUsageSQL).Scan(&used)
	return used, err
}
