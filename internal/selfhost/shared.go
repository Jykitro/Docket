package selfhost

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"plandillo/internal/auth"
	"plandillo/internal/events"
	"plandillo/internal/push"
	"plandillo/internal/service"
	"plandillo/internal/store"
)

// UsePush turns on push notifications for the workspaces opened after it (call before serving).
func (w *Workspaces) UsePush(p *push.Push) { w.push = p }

// TodayOpen counts the open tasks planned for date in one workspace, for the morning reminder (push.Today).
func (w *Workspaces) TodayOpen(ctx context.Context, owner, space int64, date string) (string, int, error) {
	sp, err := w.shared.Space(ctx, owner, space)
	if err != nil {
		return "", 0, err
	}
	ws, err := w.openSpace(ctx, sp)
	if err != nil {
		return "", 0, err
	}
	r, err := ws.Svc.TodayView(ctx, date)
	if err != nil {
		return "", 0, err
	}
	open := 0
	for _, t := range r.Tasks {
		if t.StatusCategory != "done" {
			open++
		}
	}
	return sp.Slug, open, nil
}

// UseShared installs the catalog-backed workspace manager before serving requests.
func (w *Workspaces) UseShared(s *store.Store, envToken string, firstID int64) {
	w.shared = s
	w.envToken = envToken
	w.firstID = firstID
}
func (w *Workspaces) openSpace(ctx context.Context, sp store.Space) (*Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ws := w.m[sp.ID]; ws != nil {
		return ws, nil
	}
	st := w.shared.ForOwner(sp.ID, sp.OwnerID)
	bus := events.NewBus()
	svc := service.New(st, bus, w.loc)
	if w.push != nil {
		svc.SetPush(w.push.For(sp.OwnerID, sp.ID))
		svc.ObserveAgentDone(func(t store.Task) { w.push.AgentDone(sp.OwnerID, sp.ID, t) })
	}
	if sp.ID == w.firstID && w.envToken != "" {
		svc.SetEnvToken(w.envToken)
	}
	if _, err := svc.EnsureMCPSettings(ctx); err != nil {
		return nil, err
	}
	parent, cancel := context.WithCancel(w.parent)
	ws := &Workspace{ID: sp.ID, OwnerID: sp.OwnerID, generation: sp.Generation, Svc: svc, st: st, ctx: parent, cancel: cancel}
	ws.Router = w.build(context.WithValue(parent, workspaceOwnerKey{}, sp.Generation), svc, bus)
	if err := svc.ObserveMCPToken(ctx, func(tok string) { w.tokens.replace(ws, tok) }); err != nil {
		cancel()
		return nil, err
	}
	w.m[sp.ID] = ws
	return ws, nil
}
func (w *Workspaces) forgetSpace(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ws := w.m[id]; ws != nil {
		ws.cancel()
		w.tokens.replace(ws, "")
		delete(w.m, id)
	}
}
func (w *Workspaces) forgetOwner(id int64) {
	for _, ws := range w.All() {
		if ws.OwnerID == id {
			w.forgetSpace(ws.ID)
		}
	}
}
func spaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, 404, "not_found", "workspace not found")
	case errors.Is(err, store.ErrSpaceInput):
		writeError(w, 400, "invalid_argument", err.Error())
	case errors.Is(err, store.ErrLastSpace):
		writeError(w, 409, "last_workspace", err.Error())
	default:
		internalError(w, err)
	}
}
func (h *handler) catalogRoutes(r *chi.Mux) {
	r.Get("/api/workspaces", h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		rows, err := h.WS.shared.Spaces(r.Context(), u.ID)
		if err != nil {
			spaceError(w, err)
			return
		}
		writeJSON(w, 200, rows)
	}))
	r.Post("/api/workspaces", h.authed(h.changeSpace))
	r.Patch("/api/workspaces/{wid}", h.authed(h.changeSpace))
	r.Delete("/api/workspaces/{wid}", h.authed(h.changeSpace))
	r.Get("/api/workspaces/{wid}/icon", h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		id, _ := strconv.ParseInt(chi.URLParam(r, "wid"), 10, 64)
		data, err := h.WS.shared.SpaceIcon(u.ID, id)
		if err != nil {
			spaceError(w, err)
			return
		}
		// The UI asks for /icon?v=<updated_at>, so a changed icon gets a new URL and the old one may stay cached.
		sendPNG(w, data, r.URL.Query().Has("v"))
	}))
	r.Handle("/api/workspaces/{wid}/*", http.HandlerFunc(h.workspaceAPI))
	r.Put("/api/auth/profile", h.authed(h.updateProfile))
	r.Put("/api/auth/photo", h.authed(h.updatePhoto))
	r.Get("/api/users/{id}/photo", h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if id != u.ID {
			writeError(w, 404, "not_found", "photo not found")
			return
		}
		data, err := h.Auth.Photo(id)
		if err != nil {
			credentialError(w, err)
			return
		}
		sendPNG(w, data, false)
	}))
}

// sendPNG answers with an image. A versioned URL may be kept by the browser (not by shared caches), so switching
// workspaces reloads the page without fetching the icon again; anything else is not stored.
func sendPNG(w http.ResponseWriter, data []byte, versioned bool) {
	w.Header().Set("Content-Type", "image/png")
	cache := "private, no-store"
	if versioned {
		cache = "private, max-age=31536000, immutable"
	}
	w.Header().Set("Cache-Control", cache)
	w.Write(data)
}
func (h *handler) updateProfile(w http.ResponseWriter, r *http.Request, u auth.User) {
	var in struct{ Name string }
	if !decode(w, r, &in) {
		return
	}
	if err := h.Auth.UpdateProfile(u.ID, in.Name); err != nil {
		credentialError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *handler) updatePhoto(w http.ResponseWriter, r *http.Request, u auth.User) {
	var in struct {
		PNG []byte `json:"photo"`
	}
	if !decode(w, r, &in) {
		return
	}
	png, err := service.NormalizeIcon(in.PNG)
	if err != nil {
		writeError(w, 400, "invalid_argument", err.Error())
		return
	}
	if err = h.Auth.SetPhoto(u.ID, png); err != nil {
		credentialError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *handler) changeSpace(w http.ResponseWriter, r *http.Request, u auth.User) {
	id := int64(0)
	if r.Method != http.MethodPost {
		var err error
		id, err = strconv.ParseInt(chi.URLParam(r, "wid"), 10, 64)
		if err != nil || id <= 0 {
			spaceError(w, store.ErrNotFound)
			return
		}
	}
	var in struct {
		store.SpaceChange
		Password string
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Icon != nil {
		if err := service.ValidateIconName(*in.Icon); err != nil {
			spaceError(w, store.ErrSpaceInput)
			return
		}
	}
	if in.PNG != nil {
		png, err := service.NormalizeIcon(*in.PNG)
		if err != nil {
			writeError(w, 400, "invalid_argument", err.Error())
			return
		}
		in.PNG = &png
	}
	if r.Method == http.MethodDelete {
		in.Action = "delete"
	} else if in.Action == "delete" {
		spaceError(w, store.ErrSpaceInput)
		return
	}
	apply := func() {
		sp, err := h.WS.shared.ChangeSpace(r.Context(), u.ID, id, in.SpaceChange)
		if err != nil {
			spaceError(w, err)
			return
		}
		if in.Action == "archive" || in.Action == "delete" {
			h.WS.forgetSpace(sp.ID)
		}
		if in.Action == "delete" {
			w.WriteHeader(204)
			return
		}
		status := 200
		if id == 0 {
			status = 201
		}
		writeJSON(w, status, sp)
	}
	if in.Action == "delete" && h.LocalID == 0 {
		h.confirmed(w, r, u, in.Password, nil, func(auth.PasswordProof) { apply() })
		return
	}
	apply()
}
func (h *handler) workspaceAPI(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "wid")
	if ref == "" {
		spaceError(w, store.ErrNotFound)
		return
	}
	tail := chi.URLParam(r, "*")
	if strings.HasPrefix(tail, "auth/") || strings.HasPrefix(tail, "admin/") {
		http.NotFound(w, r)
		return
	}
	h.forwardAPI(w, r, ref, "/api/"+tail)
}
