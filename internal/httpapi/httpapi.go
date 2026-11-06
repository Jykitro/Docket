// Package httpapi serves the REST API, the SSE stream, and wires /mcp and the SPA behind
// the host/origin guard. Every REST call acts as denis.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"docket/internal/events"
	"docket/internal/service"
	"docket/internal/store"
)

type Deps struct {
	Svc          *service.Service
	Bus          *events.Bus
	AllowedHosts []string
	MCP          http.Handler
	Web          http.Handler
	// SelfAddr is host:port the process listens on; the MCP self-check calls /healthz there. Empty disables
	// the background check.
	SelfAddr string
	// Ctx stops the background self-check; nil means it runs for the life of the process.
	Ctx context.Context
}

func Router(d Deps) http.Handler {
	a := &api{svc: d.Svc, bus: d.Bus, mcp: d.MCP, selfAddr: d.SelfAddr, hosts: d.AllowedHosts}
	if d.SelfAddr != "" {
		ctx := d.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		go a.checkLoop(ctx)
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer, HostGuard(d.AllowedHosts))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	// /mcp is both the MCP endpoint and a screen of the web app: a browser navigation gets the app.
	mcp := bearer(d.Svc, d.MCP)
	r.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && strings.Contains(req.Header.Get("Accept"), "text/html") {
			d.Web.ServeHTTP(w, req)
			return
		}
		mcp.ServeHTTP(w, req)
	}))
	r.Route("/api", func(r chi.Router) {
		r.Get("/auth/me", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"mode": "local"}) })
		r.Get("/projects", a.h(a.listProjects))
		r.Post("/projects", a.h(a.createProject))
		r.Patch("/projects/{id}", a.h(a.updateProject))
		r.Delete("/projects/{id}", a.h(a.deleteProject))
		r.Post("/projects/{id}/restore", a.h(a.restoreProject))
		r.Post("/projects/{id}/archive", a.h(a.archiveProject))
		r.Get("/projects/{id}/export", a.exportProject)
		r.Get("/projects/{id}/statuses", a.h(a.listStatuses))
		r.Post("/projects/{id}/statuses", a.h(a.createStatus))
		r.Post("/projects/{id}/statuses/reorder", a.h(a.reorderStatuses))
		r.Patch("/statuses/{id}", a.h(a.updateStatus))
		r.Delete("/statuses/{id}", a.h(a.deleteStatus))
		r.Get("/projects/{id}/labels", a.h(a.listLabels))
		r.Post("/projects/{id}/labels", a.h(a.createLabel))
		r.Patch("/labels/{id}", a.h(a.updateLabel))
		r.Delete("/labels/{id}", a.h(a.deleteLabel))
		r.Get("/projects/{id}/fields", a.h(a.listFields))
		r.Post("/projects/{id}/fields", a.h(a.createField))
		r.Patch("/fields/{id}", a.h(a.updateField))
		r.Delete("/fields/{id}", a.h(a.deleteField))
		r.Get("/views", a.h(a.listViews))
		r.Post("/views", a.h(a.createView))
		r.Patch("/views/{id}", a.h(a.updateView))
		r.Delete("/views/{id}", a.h(a.deleteView))
		r.Get("/today", a.h(a.today))
		r.Get("/search", a.h(a.search))
		r.Get("/tasks", a.h(a.listTasks))
		r.Post("/tasks", a.h(a.createTask))
		r.Post("/tasks/carry-over", a.h(a.carryOver))
		r.Get("/tasks/{id}", a.h(a.getTask))
		r.Patch("/tasks/{id}", a.h(a.updateTask))
		r.Post("/tasks/{id}/move", a.h(a.moveTask))
		r.Delete("/tasks/{id}", a.h(a.deleteTask))
		r.Post("/tasks/{id}/restore", a.h(a.restoreTask))
		r.Post("/tasks/{id}/notes", a.h(a.addNote))
		r.Get("/trash", a.h(a.trash))
		r.Get("/trash/projects", a.h(a.trashProjects))
		r.Delete("/trash", a.h(a.purge))
		r.Get("/mcp/status", a.h(a.mcpStatus))
		r.Post("/mcp/check", a.h(a.mcpCheck))
		r.Get("/mcp/settings", a.h(a.mcpSettings))
		r.Patch("/mcp/settings", a.h(a.mcpUpdateSettings))
		r.Get("/mcp/token", a.h(a.mcpToken))
		r.Post("/mcp/token/rotate", a.h(a.mcpRotate))
		r.Get("/mcp/calls", a.h(a.mcpCalls))
		r.Delete("/mcp/calls", a.h(a.mcpClearCalls))
		r.Get("/profile", a.h(a.getProfile))
		r.Put("/profile", a.h(a.setProfile))
		r.Get("/events", a.events)
		r.Get("/export", a.export)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeError(w, 404, "not_found", "no such endpoint") })
		r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, 405, "method_not_allowed", "method not allowed")
		})
	})
	r.Handle("/*", d.Web)
	return r
}

// HostGuard blocks DNS rebinding (Host allowlist) and browser CSRF (Origin allowlist on API writes).
func HostGuard(allowedHosts []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, h := range allowedHosts {
		allowed[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowed[hostname(r.Host)] {
				writeError(w, 403, "forbidden_host", "host not allowed")
				return
			}
			safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
			if !safe && strings.HasPrefix(r.URL.Path, "/api/") {
				if o := r.Header.Get("Origin"); o != "" {
					u, err := url.Parse(o)
					if err != nil || !allowed[strings.ToLower(u.Hostname())] {
						writeError(w, 403, "forbidden_origin", "origin not allowed")
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hostname(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport
	}
	return strings.ToLower(strings.Trim(h, "[]"))
}

// bearer checks the MCP token on every request, so a rotated token takes effect at once.
func bearer(svc *service.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := svc.MCPToken(r.Context())
		want := []byte("Bearer " + token)
		if err != nil || token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, 401, "unauthorized", "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

type api struct {
	svc      *service.Service
	bus      *events.Bus
	mcp      http.Handler
	selfAddr string
	hosts    []string
	last     lastCheck
}

type created struct{ v any } // makes h() answer 201

type none struct{} // makes h() answer 204

func (a *api) h(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := fn(r)
		if err != nil {
			var se *service.Error
			switch {
			case errors.As(err, &se):
				writeError(w, statusFor(se.Code), se.Code, se.Message)
			default:
				slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
				writeError(w, 500, "internal", "internal error")
			}
			return
		}
		switch c := v.(type) {
		case created:
			writeJSON(w, 201, c.v)
		case none:
			w.WriteHeader(204)
		default:
			writeJSON(w, 200, v)
		}
	}
}

func statusFor(code string) int {
	switch code {
	case "not_found":
		return 404
	case "status_in_use":
		return 409
	case "forbidden":
		return 403
	}
	return 400
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

func badRequest(msg string) error { return &service.Error{Code: "validation", Message: msg} }

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v); err != nil {
		return badRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, badRequest("id must be a number")
	}
	return id, nil
}

func truthy(s string) bool { return s == "1" || s == "true" }

// ---- projects ----

func (a *api) listProjects(r *http.Request) (any, error) {
	return a.svc.ListProjects(r.Context(), truthy(r.URL.Query().Get("include_archived")))
}

func (a *api) createProject(r *http.Request) (any, error) {
	var in service.ProjectInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	p, err := a.svc.CreateProject(r.Context(), service.ActorDenis, in)
	return created{p}, err
}

func (a *api) updateProject(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.ProjectPatch
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateProject(r.Context(), service.ActorDenis, id, in)
}

func (a *api) deleteProject(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return a.svc.DeleteProject(r.Context(), service.ActorDenis, id)
}

func (a *api) restoreProject(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return a.svc.RestoreProject(r.Context(), service.ActorDenis, id)
}

func (a *api) trashProjects(r *http.Request) (any, error) {
	return a.svc.ListDeletedProjects(r.Context())
}

// archiveProject archives by default; {"archived": false} brings the project back.
func (a *api) archiveProject(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	archived := true
	var in struct {
		Archived *bool `json:"archived"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if in.Archived != nil {
			archived = *in.Archived
		}
	}
	return a.svc.UpdateProject(r.Context(), service.ActorDenis, id, service.ProjectPatch{Archived: &archived})
}

// ---- statuses, labels, fields, views ----

func (a *api) listStatuses(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return a.svc.ListStatuses(r.Context(), id)
}

func (a *api) createStatus(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.StatusInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	st, err := a.svc.CreateStatus(r.Context(), service.ActorDenis, id, in)
	return created{st}, err
}

func (a *api) reorderStatuses(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.ReorderStatuses(r.Context(), service.ActorDenis, id, in.IDs)
}

func (a *api) updateStatus(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.StatusPatch
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateStatus(r.Context(), service.ActorDenis, id, in)
}

// deleteStatus takes ?move_to=<status id> when the status still holds tasks.
func (a *api) deleteStatus(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var moveTo int64
	if v := r.URL.Query().Get("move_to"); v != "" {
		if moveTo, err = strconv.ParseInt(v, 10, 64); err != nil {
			return nil, badRequest("move_to must be a status id")
		}
	}
	return none{}, a.svc.DeleteStatus(r.Context(), service.ActorDenis, id, moveTo)
}

func (a *api) listLabels(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return a.svc.ListLabels(r.Context(), id)
}

func (a *api) createLabel(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in struct{ Name, Color string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	l, err := a.svc.CreateLabel(r.Context(), service.ActorDenis, id, in.Name, in.Color)
	return created{l}, err
}

func (a *api) updateLabel(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.LabelPatch
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateLabel(r.Context(), service.ActorDenis, id, in)
}

func (a *api) deleteLabel(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return none{}, a.svc.DeleteLabel(r.Context(), service.ActorDenis, id)
}

func (a *api) listFields(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return a.svc.ListFields(r.Context(), id)
}

func (a *api) createField(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.FieldInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	f, err := a.svc.CreateField(r.Context(), service.ActorDenis, id, in)
	return created{f}, err
}

func (a *api) updateField(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.FieldPatch
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateField(r.Context(), service.ActorDenis, id, in)
}

func (a *api) deleteField(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return none{}, a.svc.DeleteField(r.Context(), service.ActorDenis, id)
}

// listViews returns the global views plus those of ?project= (id, slug or key).
func (a *api) listViews(r *http.Request) (any, error) {
	var pid int64
	if ref := r.URL.Query().Get("project"); ref != "" {
		p, err := a.svc.ResolveProject(r.Context(), ref)
		if err != nil {
			return nil, err
		}
		pid = p.ID
	}
	return a.svc.ListViews(r.Context(), pid)
}

func (a *api) createView(r *http.Request) (any, error) {
	var in service.ViewInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	v, err := a.svc.CreateView(r.Context(), service.ActorDenis, in)
	return created{v}, err
}

func (a *api) updateView(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	var in service.ViewPatch
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateView(r.Context(), service.ActorDenis, id, in)
}

func (a *api) deleteView(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return none{}, a.svc.DeleteView(r.Context(), service.ActorDenis, id)
}

// ---- tasks ----

func (a *api) today(r *http.Request) (any, error) {
	return a.svc.TodayView(r.Context(), r.URL.Query().Get("date"))
}

func (a *api) search(r *http.Request) (any, error) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	return a.svc.Search(r.Context(), q.Get("q"), q.Get("scope"), limit)
}

func (a *api) listTasks(r *http.Request) (any, error) {
	q := r.URL.Query()
	f := store.TaskFilter{Status: q.Get("status"), PlannedFor: q.Get("planned_for"), Area: q.Get("area"),
		CreatedBy: q.Get("created_by"), Q: q.Get("q"), IncludeDone: truthy(q.Get("include_done"))}
	if ref := q.Get("project"); ref != "" {
		p, err := a.svc.ResolveProject(r.Context(), ref)
		if err != nil {
			return nil, err
		}
		f.ProjectID = p.ID
	}
	if v := q.Get("parent"); v != "" {
		t, err := a.svc.ResolveTask(r.Context(), v)
		if err != nil {
			return nil, err
		}
		f.ParentID = t.ID
	}
	return a.svc.ListTasks(r.Context(), f)
}

func (a *api) createTask(r *http.Request) (any, error) {
	var in service.NewTask
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	in.Ref, in.ParentRef = "", ""
	ts, err := a.svc.CreateTasks(r.Context(), service.ActorDenis, []service.NewTask{in})
	if err != nil {
		return nil, err
	}
	return created{ts[0]}, nil
}

// taskRef is the {id} of the path: a code like TRK-07 or a numeric id.
func taskRef(r *http.Request) string { return chi.URLParam(r, "id") }

func (a *api) getTask(r *http.Request) (any, error) {
	return a.svc.GetTask(r.Context(), taskRef(r), 50)
}

func (a *api) updateTask(r *http.Request) (any, error) {
	var in service.Changes
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.UpdateTask(r.Context(), service.ActorDenis, taskRef(r), in)
}

func (a *api) moveTask(r *http.Request) (any, error) {
	var in service.MoveInput
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return a.svc.MoveTask(r.Context(), service.ActorDenis, taskRef(r), in)
}

func (a *api) deleteTask(r *http.Request) (any, error) {
	n, err := a.svc.DeleteTasks(r.Context(), service.ActorDenis, []string{taskRef(r)})
	return map[string]int{"trashed": n}, err
}

func (a *api) restoreTask(r *http.Request) (any, error) {
	return a.svc.RestoreTask(r.Context(), service.ActorDenis, taskRef(r))
}

func (a *api) addNote(r *http.Request) (any, error) {
	var in struct {
		BodyMD string `json:"body_md"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	n, err := a.svc.AddNote(r.Context(), service.ActorDenis, taskRef(r), in.BodyMD)
	return created{n}, err
}

func (a *api) carryOver(r *http.Request) (any, error) {
	var in struct{ From, To string }
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	n, err := a.svc.CarryOver(r.Context(), service.ActorDenis, in.From, in.To)
	return map[string]int{"moved": n}, err
}

func (a *api) trash(r *http.Request) (any, error) { return a.svc.Trash(r.Context()) }

func (a *api) purge(r *http.Request) (any, error) {
	n, err := a.svc.PurgeTrash(r.Context())
	return map[string]int64{"purged": n}, err
}

func (a *api) sendExport(w http.ResponseWriter, r *http.Request, projectID int64) {
	e, err := a.svc.Export(r.Context(), projectID)
	if err != nil {
		var se *service.Error
		if errors.As(err, &se) {
			writeError(w, statusFor(se.Code), se.Code, se.Message)
			return
		}
		slog.Error("export failed", "err", err)
		writeError(w, 500, "internal", "internal error")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="docket-export-`+e.ExportedAt[:10]+`.json"`)
	writeJSON(w, 200, e)
}

func (a *api) export(w http.ResponseWriter, r *http.Request) { a.sendExport(w, r, 0) }

func (a *api) exportProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, 400, "validation", err.Error())
		return
	}
	a.sendExport(w, r, id)
}

func (a *api) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := a.bus.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}
