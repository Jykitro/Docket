package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"docket/internal/events"
	"docket/internal/service"
)

// MCP screen: status, self-check, settings, token and call log.

type Check struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Value  string `json:"value"`
	Hint   string `json:"hint,omitempty"` // what to do when the check failed
}

type CheckResult struct {
	OK     bool    `json:"ok"`
	TookMS int64   `json:"took_ms"`
	At     string  `json:"at"`
	Checks []Check `json:"checks"`
}

type lastCheck struct {
	mu  sync.Mutex
	res *CheckResult
}

func (l *lastCheck) get() *CheckResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.res
}

func (l *lastCheck) set(r *CheckResult) {
	l.mu.Lock()
	l.res = r
	l.mu.Unlock()
}

// checkLoop refreshes the self-check every 30 seconds and tells the UI over SSE.
func (a *api) checkLoop(ctx context.Context) {
	t := time.NewTimer(2 * time.Second) // let the listener come up first
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.runAndStore(ctx)
			t.Reset(30 * time.Second)
		}
	}
}

func (a *api) runAndStore(ctx context.Context) *CheckResult {
	res := a.runCheck(ctx)
	a.last.set(res)
	a.bus.Publish(events.Event{Type: "mcp_status", Actor: "system"})
	return res
}

func (a *api) runCheck(ctx context.Context) *CheckResult {
	started := time.Now()
	checks := []Check{a.checkServer(ctx), a.checkProtocol(ctx), a.checkToken(ctx), a.checkHosts(), a.checkDB(ctx)}
	res := &CheckResult{OK: true, At: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Checks: checks}
	for _, c := range checks {
		res.OK = res.OK && c.OK
	}
	res.TookMS = time.Since(started).Milliseconds()
	return res
}

// checkServer requests our own /healthz over the network, the way a client would.
func (a *api) checkServer(ctx context.Context) Check {
	c := Check{Key: "server", Label: "Сервер отвечает", Hint: "Проверьте, что контейнер запущен: docker compose ps"}
	addr := a.selfAddr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if addr == "" || len(a.hosts) == 0 {
		c.Detail = "адрес сервера неизвестен"
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	req.Host = strings.TrimSpace(a.hosts[0])
	start := time.Now()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Detail = "нет ответа от /healthz"
		return c
	}
	res.Body.Close()
	ms := time.Since(start).Milliseconds()
	c.OK, c.Value = res.StatusCode == 200, fmt.Sprintf("%d мс", ms)
	c.Detail = "GET /healthz → " + strconv.Itoa(res.StatusCode)
	return c
}

// checkProtocol runs initialize and tools/list on the MCP handler inside the process.
func (a *api) checkProtocol(ctx context.Context) Check {
	c := Check{Key: "protocol", Label: "Протокол MCP", Hint: "Посмотрите лог контейнера: docker compose logs docket"}
	call := func(body string) (json.RawMessage, error) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rec := httptest.NewRecorder()
		a.mcp.ServeHTTP(rec, req)
		if rec.Code != 200 {
			return nil, fmt.Errorf("HTTP %d", rec.Code)
		}
		var env struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			return nil, err
		}
		if env.Error != nil {
			return nil, fmt.Errorf("%s", env.Error.Message)
		}
		return env.Result, nil
	}
	if _, err := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"docket-selfcheck","version":"1"}}}`); err != nil {
		c.Detail = "initialize: " + err.Error()
		return c
	}
	raw, err := call(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if err != nil {
		c.Detail = "tools/list: " + err.Error()
		return c
	}
	var list struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		c.Detail = "tools/list: " + err.Error()
		return c
	}
	c.OK, c.Value = true, strconv.Itoa(len(list.Tools))
	c.Detail = fmt.Sprintf("Streamable HTTP · включено инструментов: %d", len(list.Tools))
	return c
}

func (a *api) checkToken(ctx context.Context) Check {
	c := Check{Key: "token", Label: "Токен", Hint: "Задайте DOCKET_MCP_TOKEN в .env или перевыпустите токен на этой странице"}
	tok, err := a.svc.RevealMCPToken(ctx)
	if err != nil || tok.Token == "" {
		c.Detail = "токен не задан"
		return c
	}
	stats, err := a.svc.MCPStats(ctx)
	if err != nil {
		c.Detail = "не удалось прочитать статистику"
		return c
	}
	c.OK, c.Value = true, tok.Masked
	c.Detail = "токен задан · ещё не использовался"
	if stats.TokenLastUsedAt != "" {
		c.Detail = "токен задан · последний вызов " + localStamp(stats.TokenLastUsedAt)
	}
	return c
}

// localStamp renders a stored UTC timestamp as "05.11 19:04" in the server's local zone.
func localStamp(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("02.01 15:04")
}

func (a *api) checkHosts() Check {
	c := Check{Key: "hosts", Label: "Host и Origin", Hint: "Добавьте хост в DOCKET_ALLOWED_HOSTS и перезапустите контейнер"}
	list := strings.Join(a.hosts, ", ")
	c.OK = len(a.hosts) > 0
	c.Detail = "разрешённые хосты: " + list
	return c
}

func (a *api) checkDB(ctx context.Context) Check {
	c := Check{Key: "db", Label: "База данных", Hint: "Проверьте место на диске и том ./data; бэкапы лежат в data/backups"}
	h, err := a.svc.DBHealth(ctx)
	if err != nil {
		c.Detail = "не удалось проверить базу"
		return c
	}
	c.OK = h.QuickCheck == "ok" && strings.EqualFold(h.Journal, "wal")
	c.Value = fmt.Sprintf("%.1f МБ", float64(h.SizeBytes)/(1<<20))
	backup := "бэкапа ещё не было"
	if h.LastBackupAt != "" {
		backup = "бэкап " + localStamp(h.LastBackupAt)
	}
	c.Detail = fmt.Sprintf("quick_check %s · %s · %s", h.QuickCheck, strings.ToUpper(h.Journal), backup)
	return c
}

// ---- handlers ----

func (a *api) mcpStatus(r *http.Request) (any, error) {
	stats, err := a.svc.MCPStats(r.Context())
	if err != nil {
		return nil, err
	}
	last := a.last.get()
	return struct {
		service.MCPStats
		Online bool         `json:"online"`
		Check  *CheckResult `json:"check"`
	}{stats, last != nil && last.OK, last}, nil
}

func (a *api) mcpCheck(r *http.Request) (any, error) { return a.runAndStore(r.Context()), nil }

func (a *api) mcpSettings(r *http.Request) (any, error) { return a.svc.GetMCPSettings(r.Context()) }

func (a *api) mcpUpdateSettings(r *http.Request) (any, error) {
	var in service.MCPSettingsPatch
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateMCPSettings(r.Context(), in)
}

func (a *api) mcpToken(r *http.Request) (any, error) { return a.svc.RevealMCPToken(r.Context()) }

func (a *api) mcpRotate(r *http.Request) (any, error) { return a.svc.RotateMCPToken(r.Context()) }

func (a *api) mcpCalls(r *http.Request) (any, error) {
	q := r.URL.Query()
	filter := q.Get("filter")
	if filter == "" {
		filter = "all"
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	return a.svc.ListMCPCalls(r.Context(), filter, limit)
}

func (a *api) mcpClearCalls(r *http.Request) (any, error) {
	return none{}, a.svc.ClearMCPCalls(r.Context())
}
