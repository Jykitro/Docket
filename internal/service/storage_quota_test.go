package service

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestDemoQuotaAtomicWritesAndCleanup(t *testing.T) {
	if os.Getenv("DOCKET_TEST_POSTGRES_DSN") == "" {
		t.Skip("hosted demo requires PostgreSQL")
	}
	s := newSvc(t)
	p := mustProject(t, s, "demo")
	sample := strings.Repeat("я", 64*1024)
	s.SetStorageLimit(220_000) // one body fits, two do not, even with PostgreSQL TOAST compression
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := s.CreateTasks(ctx, "mcp", []NewTask{{ProjectID: p.ID, Title: "sample", DescriptionMD: sample}})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	var ok, capped int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case ErrorCode(err) == "quota_exceeded":
			capped++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || capped != 1 {
		t.Fatalf("success=%d capped=%d", ok, capped)
	}
	profile, err := s.GetProfile(ctx)
	if err != nil || profile.Storage == nil || profile.Storage.Used < int64(len(sample)) {
		t.Fatalf("usage: %+v %v", profile.Storage, err)
	}
	before := profile.Storage.Used
	if _, err := s.CreateTasks(ctx, "mcp", []NewTask{{ProjectID: p.ID, Title: "rollback"}, {ProjectID: p.ID, Title: "large", DescriptionMD: sample}}); ErrorCode(err) != "quota_exceeded" {
		t.Fatal(err)
	}
	profile, err = s.GetProfile(ctx)
	if err != nil || profile.Storage.Used != before {
		t.Fatal("partial batch persisted", err)
	}
	if _, err = s.DeleteProject(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PurgeTrash(ctx, reviewedTrash(t, s)); err != nil {
		t.Fatal(err)
	}
	profile, err = s.GetProfile(ctx)
	if err != nil || profile.Storage.Used >= before {
		t.Fatal("purge did not release quota", err)
	}
	s.SetStorageLimit(0)
	p = mustProject(t, s, "owner")
	mustTasks(t, s, "mcp", NewTask{ProjectID: p.ID, Title: "large", DescriptionMD: sample + sample})
	profile, err = s.GetProfile(ctx)
	if err != nil || profile.Storage != nil {
		t.Fatal("unlimited profile reports demo quota", err)
	}
}
