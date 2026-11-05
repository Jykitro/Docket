package service

import (
	"context"
	"strings"
	"time"

	"docket/internal/store"
)

type SearchGroups struct {
	Tasks        []store.SearchHit `json:"tasks"`
	Descriptions []store.SearchHit `json:"descriptions"`
	Projects     []store.Project   `json:"projects"`
}

type SearchResult struct {
	Groups SearchGroups   `json:"groups"`
	Counts map[string]int `json:"counts"`
	TookMS int64          `json:"took_ms"`
}

// Search finds tasks by title (or by ID like TRK-07), tasks by description with a snippet, and projects.
// scope is all, tasks or projects.
func (s *Service) Search(ctx context.Context, q, scope string, limit int) (r SearchResult, err error) {
	if scope == "" {
		scope = "all"
	}
	if err := oneOf("scope", scope, []string{"all", "tasks", "projects"}); err != nil {
		return r, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	started := time.Now()
	r.Groups = SearchGroups{Tasks: []store.SearchHit{}, Descriptions: []store.SearchHit{}, Projects: []store.Project{}}
	err = s.read(ctx, func(tx *store.Tx) error {
		q = strings.TrimSpace(q)
		if scope != "projects" {
			seen := map[int64]bool{}
			if codeRe.MatchString(q) {
				if t, err := resolveTask(tx, q); err == nil && t.DeletedAt == nil {
					st, err := tx.GetStatus(t.StatusID)
					if err != nil {
						return err
					}
					r.Groups.Tasks = append(r.Groups.Tasks, store.SearchHit{ID: t.ID, ProjectID: t.ProjectID, Code: t.Code, Title: t.Title,
						Status: t.Status, StatusName: st.Name, StatusColor: st.Color})
					seen[t.ID] = true
				}
			}
			titles, err := tx.SearchTitles(q, limit)
			if err != nil {
				return err
			}
			for _, h := range titles {
				if !seen[h.ID] {
					r.Groups.Tasks = append(r.Groups.Tasks, h)
					seen[h.ID] = true
				}
			}
			descs, err := tx.SearchDescriptions(q, limit)
			if err != nil {
				return err
			}
			for _, h := range descs {
				if !seen[h.ID] {
					r.Groups.Descriptions = append(r.Groups.Descriptions, h)
				}
			}
		}
		if scope != "tasks" && q != "" {
			ps, err := tx.ListProjects(false)
			if err != nil {
				return err
			}
			needle := strings.ToLower(q)
			for _, p := range ps {
				if strings.Contains(strings.ToLower(p.Name), needle) || strings.Contains(strings.ToLower(p.Key), needle) ||
					strings.Contains(p.Slug, needle) {
					r.Groups.Projects = append(r.Groups.Projects, p)
				}
			}
		}
		return nil
	})
	r.Counts = map[string]int{"tasks": len(r.Groups.Tasks), "descriptions": len(r.Groups.Descriptions), "projects": len(r.Groups.Projects)}
	r.TookMS = time.Since(started).Milliseconds()
	return r, err
}
