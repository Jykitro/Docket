package store

// PostgreSQL maintains content_bytes in the same transaction as each content write.
// The owner index bounds this sum to the owner's metadata, including archived spaces.
const workspaceUsageSQL = `SELECT coalesce(sum(content_bytes + 128 + octet_length(name) + coalesce(octet_length(icon_png),0)),0)
 FROM workspaces WHERE owner_user_id=(SELECT owner_user_id FROM workspaces WHERE id=:workspace)`

func (t *Tx) WorkspaceBytes() (int64, error) {
	var used int64
	err := t.tx.QueryRow(workspaceUsageSQL).Scan(&used)
	return used, err
}
