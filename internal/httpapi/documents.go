package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"docket/internal/service"
)

func (a *api) publishHTML(r *http.Request) (any, error) {
	var in service.PublishHTMLInput
	// A JSON encoder can use six bytes for a single HTML character.
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 13<<20)).Decode(&in); err != nil {
		return nil, badRequest("invalid HTML document JSON")
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
