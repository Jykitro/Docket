package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"plandillo/internal/testutil"
)

// Run on either engine with the same rows and offset. Fixture setup is outside the timed loop.
func BenchmarkBoardCards(b *testing.B) {
	var st *Store
	var err error
	if os.Getenv("PLANDILLO_TEST_POSTGRES_DSN") != "" {
		st, err = OpenPostgres(testutil.PostgresDSN(b), 1)
	} else {
		st, err = Open(filepath.Join(b.TempDir(), "board.db"))
	}
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { st.Close() })
	ctx := b.Context()
	err = st.WithTx(ctx, func(tx *Tx) error {
		for _, q := range []string{
			`INSERT INTO projects(workspace_id,slug,key,name,position) VALUES(:workspace,'board','QA','Board',0)`,
			`INSERT INTO statuses(workspace_id,project_id,key,name,color,category,position) VALUES(:workspace,1,'open','Open','td','todo',0)`,
			`WITH RECURSIVE fixture(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM fixture WHERE n<6000)
              INSERT INTO tasks(workspace_id,project_id,number,parent_id,title,status_id,position)
              SELECT :workspace,1,n,CASE WHEN n%7=0 THEN 1 END,'Task ' || n,1,n FROM fixture`,
			`ANALYZE tasks`,
		} {
			if _, err := tx.tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	for _, offset := range []int{0, 5000} {
		b.Run(fmt.Sprintf("offset_%d", offset), func(b *testing.B) {
			f := TaskFilter{Summary: true, IncludeDone: true, ProjectID: 1, Limit: 100, Offset: offset}
			for b.Loop() {
				err := st.WithTx(ctx, func(tx *Tx) error {
					cards, err := tx.ListTasks(f)
					if err == nil && len(cards) != f.Limit {
						return fmt.Errorf("got %d cards, want %d", len(cards), f.Limit)
					}
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
