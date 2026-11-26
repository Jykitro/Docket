package httpapi

import (
	"net/http"
	"strconv"

	"plandillo/internal/service"
)

// JSON escaping can use six bytes per byte of HTML.
const htmlJSONLimit = 13 << 20

func (a *api) publishHTML(r *http.Request) (any, error) {
	var in service.PublishHTMLInput
	if err := decodeLimited(r, &in, htmlJSONLimit); err != nil {
		return nil, err
	}
	return a.svc.PublishHTML(r.Context(), service.ActorDenis, in)
}

func documentVersion(r *http.Request) (int, error) {
	v := r.URL.Query().Get("version")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, badRequest("version must be a positive number")
	}
	return n, nil
}

func (a *api) readHTML(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	v, err := documentVersion(r)
	if err != nil {
		return nil, err
	}
	return a.svc.ReadHTML(r.Context(), id, v)
}
func (a *api) deleteHTML(r *http.Request) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	v, err := documentVersion(r)
	if err != nil {
		return nil, err
	}
	return none{}, a.svc.DeleteHTML(r.Context(), id, v)
}
