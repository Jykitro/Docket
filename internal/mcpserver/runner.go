package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"plandillo/internal/codeeval"
	"plandillo/internal/service"
)

type evaluator func(context.Context, string, func(string, json.RawMessage) (string, error)) (string, error)

// Runner connects workspaces to an isolated Code Mode service via a private Unix socket.
type Runner struct {
	socket string
	slots  chan struct{}
}

// NewRunner limits admission across all of the server's workspaces.
func NewRunner(socket string) *Runner { return &Runner{socket: socket, slots: make(chan struct{}, 2)} }

const maxCode = codeeval.MaxCode

var errBusy = errors.New("rate_limited: Code Mode is busy, try again later")

// closeIPC releases a listener or an IPC stream. A peer may already have closed it on cancellation.
func closeIPC(c io.Closer) { _ = c.Close() }

func (r *Runner) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "unix", r.socket)
	if err != nil {
		return nil, errors.New("unavailable: isolated Code Mode runner is offline")
	}
	return conn, nil
}

// Ready checks the protocol before an administrator opens signup.
func (r *Runner) Ready(ctx context.Context) error {
	c, err := r.dial(ctx)
	if err != nil {
		return err
	}
	defer closeIPC(c)
	if err = c.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	if _, err = c.Write([]byte{'P'}); err != nil {
		return err
	}
	var response [1]byte
	if _, err = io.ReadFull(c, response[:]); err != nil {
		return err
	}
	if response[0] != 'P' {
		return errors.New("unexpected Code Mode runner response")
	}
	return nil
}

func (r *Runner) workspace() evaluator {
	slot := make(chan struct{}, 1)
	return func(ctx context.Context, code string, call func(string, json.RawMessage) (string, error)) (string, error) {
		if len(code) > maxCode {
			return "", errors.New("invalid_argument: Code Mode input exceeds 256 KiB")
		}
		select {
		case slot <- struct{}{}:
			defer func() { <-slot }()
		default:
			return "", errBusy
		}
		select {
		case r.slots <- struct{}{}:
			defer func() { <-r.slots }()
		default:
			return "", errBusy
		}
		ctx, cancel := context.WithTimeout(ctx, codeTimeout)
		defer cancel()
		c, err := r.dial(ctx)
		if err != nil {
			return "", err
		}
		defer closeIPC(c)
		stop := context.AfterFunc(ctx, func() { closeIPC(c) })
		defer stop()
		if _, err = c.Write([]byte{'R'}); err != nil {
			return "", err
		}
		if err = codeeval.Send(c, codeeval.Message{Kind: "run", Text: code}); err != nil {
			return "", err
		}
		for calls := 0; ; calls++ {
			m, err := codeeval.Receive(c)
			if err != nil {
				return "", errors.New("unavailable: isolated Code Mode execution ended")
			}
			switch m.Kind {
			case "done":
				if len(m.Text) > 1<<20 || len(m.Error) > 1<<16 {
					return "", errors.New("invalid Code Mode result")
				}
				if m.Error != "" {
					return "", errors.New(m.Error)
				}
				return m.Text, nil
			case "call":
				if calls >= codeMaxCalls || !registeredTool(m.Text) {
					return "", errors.New("invalid Code Mode callback")
				}
				if err := ctx.Err(); err != nil {
					return "", err
				}
				result, callErr := call(m.Text, m.Args)
				reply := codeeval.Message{Kind: "reply", Text: result}
				if callErr != nil {
					reply.Error = callErr.Error()
				}
				if err = codeeval.Send(c, reply); err != nil {
					return "", err
				}
			default:
				return "", errors.New("unexpected Code Mode message")
			}
		}
	}
}
func registeredTool(name string) bool {
	for _, t := range service.MCPTools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func Evaluate(in io.Reader, out io.Writer) error { return codeeval.Evaluate(in, out) }

// ServeRunner supervises disposable evaluators. Deploy this command with OS memory and CPU limits.
func ServeRunner(ctx context.Context, path string) error {
	// Refuse to unlink any regular file. Only the runner has write access to the socket directory.
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return errors.New("runner path is not a socket")
		}
		if c, e := net.DialTimeout("unix", path, time.Second); e == nil {
			closeIPC(c)
			return errors.New("runner is already active")
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer closeIPC(l)
	if err = os.Chmod(path, 0660); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { closeIPC(l) })
	defer stop()
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			closeIPC(c)
			continue
		}
		wg.Go(func() {
			defer func() { <-slots }()
			defer closeIPC(c)
			serveExecution(ctx, c, executable)
		})
	}
}

func serveExecution(parent context.Context, c net.Conn, executable string) {
	if err := c.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return
	}
	var command [1]byte
	if _, err := io.ReadFull(c, command[:]); err != nil {
		return
	}
	if command[0] == 'P' {
		_, _ = c.Write(command[:]) // peer may disconnect while probing
		return
	}
	if command[0] != 'R' {
		return
	}
	if err := c.SetDeadline(time.Now().Add(codeTimeout + time.Second)); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, codeTimeout+time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "code-eval")
	child.Env = []string{"GOMEMLIMIT=128MiB", "GOMAXPROCS=1"}
	child.Stdout = c
	stdin, err := child.StdinPipe()
	if err != nil {
		return
	}
	defer closeIPC(stdin)
	if err = child.Start(); err != nil {
		return
	}
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		// EOF or a broken pipe both end this execution.
		_, _ = io.Copy(stdin, c)
		cancel()
		closeIPC(stdin)
	}()
	// Errors from terminated evaluators are reported to their request through EOF.
	_ = child.Wait()
	closeIPC(c)
	<-copied
}
