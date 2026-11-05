package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"docket/internal/store"
)

// MCP administration: token, access mode, tool switches, call log, health data for the self-check.

const (
	settingToken    = "mcp_token"
	settingMode     = "mcp_access_mode"
	settingDisabled = "mcp_disabled_tools"
	settingLastUsed = "mcp_token_last_used_at"
	settingBackup   = "last_backup_at"

	ModeReadWrite = "rw"
	ModeReadOnly  = "ro"
)

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
	{"move_tasks", "write", "Change status (key), planned date and/or project of several tasks at once. Moved tasks go to the end of the " +
		"target column. planned_for \"\" clears the date."},
	{"add_note", "write", "Add a markdown note to a task."},
	{"carry_over", "write", "Move all open tasks planned for date `from` to date `to`."},
	{"delete_tasks", "write", "Move tasks (with their subtasks) to the trash. The owner can restore them. Disabled by default."},
}

func IsWriteTool(name string) bool {
	for _, t := range MCPTools {
		if t.Name == name {
			return t.Group == "write"
		}
	}
	return false
}

func writeToolNames() []string {
	var out []string
	for _, t := range MCPTools {
		if t.Group == "write" {
			out = append(out, t.Name)
		}
	}
	return out
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS entropy source failing is not recoverable
	}
	return "dkt_" + hex.EncodeToString(b)
}

func maskToken(t string) string {
	if len(t) < 8 {
		return "dkt_••••"
	}
	return "dkt_••••" + t[len(t)-4:]
}

// SetEnvToken makes DOCKET_MCP_TOKEN win over the stored token (and disables rotation in the UI).
func (s *Service) SetEnvToken(t string) { s.envToken = t }

// MCPToken returns the token the /mcp endpoint accepts.
func (s *Service) MCPToken(ctx context.Context) (tok string, err error) {
	if s.envToken != "" {
		return s.envToken, nil
	}
	err = s.read(ctx, func(tx *store.Tx) error { tok, _, err = tx.Setting(settingToken); return err })
	return tok, err
}

// EnsureMCPSettings fills defaults on first start: a generated token (returned once so it can be logged),
// read-write mode and delete_tasks switched off.
func (s *Service) EnsureMCPSettings(ctx context.Context) (newToken string, err error) {
	err = s.st.WithTx(ctx, func(tx *store.Tx) error {
		if _, ok, err := tx.Setting(settingToken); err != nil {
			return err
		} else if !ok && s.envToken == "" {
			newToken = randomToken()
			if err := tx.SetSetting(settingToken, newToken); err != nil {
				return err
			}
		}
		if _, ok, err := tx.Setting(settingMode); err != nil {
			return err
		} else if !ok {
			if err := tx.SetSetting(settingMode, ModeReadWrite); err != nil {
				return err
			}
		}
		if _, ok, err := tx.Setting(settingDisabled); err != nil {
			return err
		} else if !ok {
			return tx.SetSetting(settingDisabled, `["delete_tasks"]`)
		}
		return nil
	})
	return newToken, err
}

type TokenInfo struct {
	Token   string `json:"token"`
	Masked  string `json:"masked"`
	FromEnv bool   `json:"from_env"`
}

func (s *Service) RevealMCPToken(ctx context.Context) (TokenInfo, error) {
	tok, err := s.MCPToken(ctx)
	return TokenInfo{Token: tok, Masked: maskToken(tok), FromEnv: s.envToken != ""}, err
}

// RotateMCPToken replaces the stored token; refused while DOCKET_MCP_TOKEN is set.
func (s *Service) RotateMCPToken(ctx context.Context) (TokenInfo, error) {
	if s.envToken != "" {
		return TokenInfo{}, invalid("the token is set by DOCKET_MCP_TOKEN: change it in .env and restart")
	}
	tok := randomToken()
	err := s.write(ctx, ActorDenis, func(o *op) error {
		o.emit("settings_changed", 0, 0)
		return o.tx.SetSetting(settingToken, tok)
	})
	return TokenInfo{Token: tok, Masked: maskToken(tok)}, err
}

type MCPToolState struct {
	ToolInfo
	Enabled bool `json:"enabled"`
	Calls7d int  `json:"calls_7d"`
}

type MCPSettings struct {
	AccessMode   string         `json:"access_mode"`
	Tools        []MCPToolState `json:"tools"`
	TokenMasked  string         `json:"token_masked"`
	TokenFromEnv bool           `json:"token_from_env"`
}

// MCPPolicy is what the MCP server enforces on every request.
type MCPPolicy struct {
	Mode     string
	Disabled map[string]bool
}

func loadPolicy(tx *store.Tx) (MCPPolicy, error) {
	p := MCPPolicy{Mode: ModeReadWrite, Disabled: map[string]bool{}}
	if v, ok, err := tx.Setting(settingMode); err != nil {
		return p, err
	} else if ok {
		p.Mode = v
	}
	if v, ok, err := tx.Setting(settingDisabled); err != nil {
		return p, err
	} else if ok {
		var names []string
		if err := json.Unmarshal([]byte(v), &names); err != nil {
			return p, err
		}
		for _, n := range names {
			p.Disabled[n] = true
		}
	}
	return p, nil
}

func (s *Service) MCPPolicy(ctx context.Context) (p MCPPolicy, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { p, err = loadPolicy(tx); return err })
	return p, err
}

func (s *Service) GetMCPSettings(ctx context.Context) (out MCPSettings, err error) {
	weekAgo := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	err = s.read(ctx, func(tx *store.Tx) error {
		pol, err := loadPolicy(tx)
		if err != nil {
			return err
		}
		st, err := tx.CallStats(weekAgo, weekAgo)
		if err != nil {
			return err
		}
		out.AccessMode = pol.Mode
		for _, t := range MCPTools {
			out.Tools = append(out.Tools, MCPToolState{ToolInfo: t, Enabled: !pol.Disabled[t.Name], Calls7d: st.PerTool[t.Name]})
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	tok, err := s.MCPToken(ctx)
	out.TokenMasked, out.TokenFromEnv = maskToken(tok), s.envToken != ""
	return out, err
}

type MCPSettingsPatch struct {
	AccessMode *string         `json:"access_mode"`
	Tools      map[string]bool `json:"tools"` // tool name -> enabled
}

func (s *Service) UpdateMCPSettings(ctx context.Context, in MCPSettingsPatch) (MCPSettings, error) {
	err := s.write(ctx, ActorDenis, func(o *op) error {
		pol, err := loadPolicy(o.tx)
		if err != nil {
			return err
		}
		if in.AccessMode != nil {
			if err := oneOf("access_mode", *in.AccessMode, []string{ModeReadWrite, ModeReadOnly}); err != nil {
				return err
			}
			if err := o.tx.SetSetting(settingMode, *in.AccessMode); err != nil {
				return err
			}
		}
		for name, on := range in.Tools {
			known := false
			for _, t := range MCPTools {
				known = known || t.Name == name
			}
			if !known {
				return invalid("unknown tool %q", name)
			}
			if on {
				delete(pol.Disabled, name)
			} else {
				pol.Disabled[name] = true
			}
		}
		names := []string{}
		for n := range pol.Disabled {
			names = append(names, n)
		}
		sort.Strings(names)
		b, _ := json.Marshal(names)
		o.emit("settings_changed", 0, 0)
		return o.tx.SetSetting(settingDisabled, string(b))
	})
	if err != nil {
		return MCPSettings{}, err
	}
	return s.GetMCPSettings(ctx)
}

// RecordMCPCall appends one line to the call log; errCode "" means the call succeeded.
func (s *Service) RecordMCPCall(ctx context.Context, tool, args string, dur time.Duration, errCode string) error {
	if utf8.RuneCountInString(args) > 120 {
		args = string([]rune(args)[:119]) + "…"
	}
	c := store.MCPCall{At: nowTS(), Tool: tool, ArgsSummary: args, DurationMS: dur.Milliseconds(), Status: "ok", Actor: ActorClaude}
	if errCode != "" {
		c.Status, c.ErrorCode = "error", &errCode
	}
	return s.write(ctx, ActorClaude, func(o *op) error {
		if err := o.tx.InsertCall(c); err != nil {
			return err
		}
		o.emit("mcp_call", 0, 0)
		return o.tx.SetSetting(settingLastUsed, c.At)
	})
}

func (s *Service) ListMCPCalls(ctx context.Context, filter string, limit int) (out []store.MCPCall, err error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if err := oneOf("filter", filter, []string{"all", "write", "error"}); err != nil {
		return nil, err
	}
	err = s.read(ctx, func(tx *store.Tx) error { out, err = tx.ListCalls(filter, writeToolNames(), limit); return err })
	return out, err
}

func (s *Service) ClearMCPCalls(ctx context.Context) error {
	return s.write(ctx, ActorDenis, func(o *op) error {
		o.emit("mcp_call", 0, 0)
		return o.tx.DeleteCalls()
	})
}

// PurgeOldMCPCalls keeps the log for 7 days.
func (s *Service) PurgeOldMCPCalls(ctx context.Context) (n int64, err error) {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	err = s.st.WithTx(ctx, func(tx *store.Tx) error { n, err = tx.PurgeCallsBefore(cutoff); return err })
	return n, err
}

type MCPStats struct {
	LastCallAt      string `json:"last_call_at"`
	CallsToday      int    `json:"calls_today"`
	MedianMS        int64  `json:"median_ms"`
	TokenLastUsedAt string `json:"token_last_used_at"`
}

func (s *Service) MCPStats(ctx context.Context) (out MCPStats, err error) {
	n := time.Now().In(s.loc)
	startOfDay := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, s.loc).UTC().Format("2006-01-02T15:04:05.000Z")
	weekAgo := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	err = s.read(ctx, func(tx *store.Tx) error {
		st, err := tx.CallStats(startOfDay, weekAgo)
		if err != nil {
			return err
		}
		out.LastCallAt, out.CallsToday, out.MedianMS = st.LastAt, st.CallsSince, st.MedianMS
		out.TokenLastUsedAt, _, err = tx.Setting(settingLastUsed)
		return err
	})
	return out, err
}

type DBHealth struct {
	QuickCheck   string `json:"quick_check"`
	Journal      string `json:"journal_mode"`
	SizeBytes    int64  `json:"size_bytes"`
	LastBackupAt string `json:"last_backup_at"`
}

func (s *Service) DBHealth(ctx context.Context) (h DBHealth, err error) {
	if h.QuickCheck, h.Journal, h.SizeBytes, err = s.st.Health(); err != nil {
		return h, err
	}
	err = s.read(ctx, func(tx *store.Tx) error { h.LastBackupAt, _, err = tx.Setting(settingBackup); return err })
	return h, err
}

// Maintenance writes a backup (keeping 14) and trims the MCP log; main runs it at start and every 24h.
func (s *Service) Maintenance(ctx context.Context, backupDir string) (string, error) {
	now := time.Now()
	path, err := s.st.Backup(filepath.Clean(backupDir), 14, now)
	if err != nil {
		return "", err
	}
	if err := s.st.WithTx(ctx, func(tx *store.Tx) error {
		return tx.SetSetting(settingBackup, now.UTC().Format("2006-01-02T15:04:05.000Z"))
	}); err != nil {
		return path, err
	}
	_, err = s.PurgeOldMCPCalls(ctx)
	return path, err
}

// ErrorCode maps an error to the code stored in the call log and shown to the assistant.
func ErrorCode(err error) string {
	var se *Error
	if errors.As(err, &se) {
		switch se.Code {
		case "validation":
			return "invalid_argument"
		default:
			return se.Code
		}
	}
	return "internal"
}

// Forbidden and ToolDisabled are the errors the MCP layer returns for policy violations.
func Forbidden(msg string) error    { return &Error{Code: "forbidden", Message: msg} }
func ToolDisabled(msg string) error { return &Error{Code: "tool_disabled", Message: msg} }
