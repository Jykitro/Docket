package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"docket/internal/store"
)

// WriteExport streams one consistent snapshot; even exports made before HTML quotas fit in memory one row at a time.
func (s *Service) WriteExport(ctx context.Context, projectID int64, dst io.Writer, start func(string)) error {
	return s.read(ctx, func(tx *store.Tx) error {
		if projectID != 0 {
			if _, err := tx.GetProject(projectID); err != nil {
				return err
			}
		}
		stamp := nowTS()
		start(stamp)
		w := bufio.NewWriter(dst)
		enc := json.NewEncoder(w)
		if _, err := fmt.Fprintf(w, `{"exported_at":%q`, stamp); err != nil {
			return err
		}
		for _, section := range []string{"projects", "statuses", "labels", "fields", "views", "tasks", "notes", "pages", "activity", "html_documents"} {
			if _, err := fmt.Fprintf(w, `,%q:[`, section); err != nil {
				return err
			}
			first := true
			err := tx.ExportRows(section, projectID, func(row any) error {
				if !first {
					if err := w.WriteByte(','); err != nil {
						return err
					}
				}
				first = false
				return enc.Encode(row)
			})
			if err != nil {
				return err
			}
			if err := w.WriteByte(']'); err != nil {
				return err
			}
		}
		if err := w.WriteByte('}'); err != nil {
			return err
		}
		return w.Flush()
	})
}
