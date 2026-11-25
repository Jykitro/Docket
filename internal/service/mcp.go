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

	"docket/internal/store"
	"docket/internal/toolcatalog"
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

type ToolInfo = toolcatalog.ToolInfo

var MCPTools = toolcatalog.MCPTools

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
func (s *Service) SetEnvToken(t string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	s.envToken = t
	if s.tokenChanged != nil {
		token, _ := s.mcpToken(context.Background()) // A failed read removes the entry until restart.
		s.tokenChanged(token)
	}
}

// ObserveMCPToken publishes the initial token and every committed rotation in commit order.
func (s *Service) ObserveMCPToken(ctx context.Context, changed func(string)) error {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	token, err := s.mcpToken(ctx)
	if err != nil {
		return err
	}
	s.tokenChanged = changed
	changed(token)
	return nil
}

// MCPToken returns the token the /mcp endpoint accepts.
func (s *Service) MCPToken(ctx context.Context) (tok string, err error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	return s.mcpToken(ctx)
}
func (s *Service) mcpToken(ctx context.Context) (tok string, err error) {
	if s.envToken != "" {
		return s.envToken, nil
	}
	err = s.read(ctx, func(tx *store.Tx) error { tok, _, err = tx.Setting(settingToken); return err })
	return tok, err
}

// EnsureMCPSettings fills defaults on first start: a generated token (returned once so it can be logged),
// read-write mode and delete_tasks switched off.
func (s *Service) EnsureMCPSettings(ctx context.Context) (newToken string, err error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
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
	if err == nil && newToken != "" && s.tokenChanged != nil {
		s.tokenChanged(newToken)
	}
	return newToken, err
}

type TokenInfo struct {
	Token   string `json:"token"`
	Masked  string `json:"masked"`
	FromEnv bool   `json:"from_env"`
}

func (s *Service) RevealMCPToken(ctx context.Context) (TokenInfo, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	tok, err := s.mcpToken(ctx)
	return TokenInfo{Token: tok, Masked: maskToken(tok), FromEnv: s.envToken != ""}, err
}

// RotateMCPToken replaces the stored token; refused while DOCKET_MCP_TOKEN is set.
func (s *Service) RotateMCPToken(ctx context.Context) (TokenInfo, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.envToken != "" {
		return TokenInfo{}, invalid("the token is set by DOCKET_MCP_TOKEN: change it in .env and restart")
	}
	tok := randomToken()
	err := s.write(ctx, ActorDenis, func(o *op) error {
		o.emit("settings_changed", 0, 0)
		return o.tx.SetSetting(settingToken, tok)
	})
	if err == nil && s.tokenChanged != nil {
		s.tokenChanged(tok)
	}
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
		counts, err := tx.ToolCallCounts(weekAgo)
		if err != nil {
			return err
		}
		out.AccessMode = pol.Mode
		for _, t := range MCPTools {
			out.Tools = append(out.Tools, MCPToolState{ToolInfo: t, Enabled: !pol.Disabled[t.Name], Calls7d: counts[t.Name]})
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	info, err := s.RevealMCPToken(ctx)
	out.TokenMasked, out.TokenFromEnv = info.Masked, info.FromEnv
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

// LogToolName keeps audit cardinality within the published catalogue, including Code Mode.
func LogToolName(name string) string {
	if name == "code" {
		return name
	}
	for _, t := range MCPTools {
		if name == t.Name {
			return t.Name
		}
	}
	return "unknown"
}

func shortAuditText(text string) string {
	n := 0
	for i := range text {
		if n == 119 {
			return text[:i] + "…"
		}
		n++
	}
	return text
}

// RecordMCPCall appends one line to the call log; errCode "" means the call succeeded.
func (s *Service) RecordMCPCall(ctx context.Context, actor, tool, args string, dur time.Duration, errCode string) error {
	args = shortAuditText(args)
	tool = LogToolName(tool)
	if len(errCode) > 48 {
		errCode = "internal"
	}
	c := store.MCPCall{At: nowTS(), Tool: tool, ArgsSummary: args, DurationMS: dur.Milliseconds(), Status: "ok", Actor: actor}
	if errCode != "" {
		c.Status, c.ErrorCode = "error", &errCode
	}
	return s.write(ctx, actor, func(o *op) error {
		o.skipStorageCheck = true
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

	err = s.read(ctx, func(tx *store.Tx) error {
		st, err := tx.CallStats(startOfDay)
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
	if path != "" {
		if err := s.st.WithTx(ctx, func(tx *store.Tx) error {
			return tx.SetSetting(settingBackup, now.UTC().Format("2006-01-02T15:04:05.000Z"))
		}); err != nil {
			return path, err
		}
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
