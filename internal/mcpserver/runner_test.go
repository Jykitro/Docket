package mcpserver

import (
	"context"
	"docket/internal/codeeval"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerAdmissionAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.sock")
	// macOS limits socket paths to 104 bytes.
	if len(path) > 100 {
		path = filepath.Join("/tmp", "dck-"+time.Now().Format("150405.000000")+".sock")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeIPC(listener)
	entered := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer closeIPC(conn)
		var command [1]byte
		if _, e = io.ReadFull(conn, command[:]); e != nil {
			return
		}
		if _, e = codeeval.Receive(conn); e != nil {
			return
		}
		close(entered)
		_, _ = io.Copy(io.Discard, conn)
	}()
	runner := NewRunner(path)
	sameOwner := runner.workspace()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	noTools := func(string, json.RawMessage) (string, error) { return "", errors.New("unexpected call") }
	result := make(chan error, 1)
	go func() { _, err := sameOwner(ctx, "return 1", noTools); result <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := sameOwner(ctx, "return 2", noTools); !errors.Is(err, errBusy) {
		t.Fatalf("second job: %v", err)
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled job succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation stalled")
	}
	<-finished
}

func TestRunnerUnreachableFailsClosed(t *testing.T) {
	runner := NewRunner(filepath.Join(t.TempDir(), "offline.sock"))
	if _, err := runner.workspace()(context.Background(), "return 42", nil); err == nil {
		t.Fatal("executed with runner offline")
	}
}
