package vercelrun

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"docket/internal/codeeval"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Each invocation gets a new 128 MiB linear memory, no preopened directories,
// environment, or sockets. The host feeds only protocol frames through two pipes.
type Worker struct {
	runtime          wazero.Runtime
	module           wazero.CompiledModule
	replyURL, secret string
	slots            chan struct{}
}

func NewWorker(ctx context.Context, wasm []byte, repliesURL, secret string) (*Worker, error) {
	if len(secret) < 32 {
		return nil, errors.New("PLANDILLO_RUNNER_SECRET must contain at least 32 characters")
	}
	reply, err := endpoint(repliesURL, "_plandillo/reply")
	if err != nil {
		return nil, err
	}
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(2048).WithCloseOnContextDone(true))
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		rt.Close(ctx)
		return nil, err
	}
	mod, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		rt.Close(ctx)
		return nil, err
	}
	return &Worker{rt, mod, reply, secret, make(chan struct{}, 2)}, nil
}
func (w *Worker) Close(ctx context.Context) error { return w.runtime.Close(ctx) }
func (w *Worker) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+w.secret)) != 1 {
		http.NotFound(out, r)
		return
	}
	out.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/ready" {
		out.WriteHeader(204)
		return
	}
	if r.URL.Path != "/run" {
		http.NotFound(out, r)
		return
	}
	key := r.Header.Get("X-Plandillo-Job")
	code, err := io.ReadAll(http.MaxBytesReader(out, r.Body, codeeval.MaxCode))
	if err != nil || key == "" {
		http.Error(out, "invalid job", 400)
		return
	}
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	default:
		http.Error(out, "busy", 429)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	closePipes := func() { inR.Close(); inW.Close(); outR.Close(); outW.Close() }
	stop := context.AfterFunc(ctx, closePipes)
	defer stop()
	defer closePipes()
	go func() {
		cfg := wazero.NewModuleConfig().WithName("").WithStdin(inR).WithStdout(outW).WithSysWalltime().WithSysNanotime()
		_, e := w.runtime.InstantiateModule(ctx, w.module, cfg)
		outW.CloseWithError(e)
		inR.CloseWithError(e)
	}()
	if err = codeeval.Send(inW, codeeval.Message{Kind: "run", Text: string(code)}); err != nil {
		http.Error(out, "worker unavailable", 503)
		return
	}
	out.Header().Set("Content-Type", "application/octet-stream")
	out.Header().Set("X-Accel-Buffering", "no")
	ctl := http.NewResponseController(out)
	for seq := 1; ; seq++ {
		msg, e := codeeval.Receive(outR)
		if e != nil {
			msg = codeeval.Message{Kind: "done", Error: "unavailable: isolated evaluation ended"}
		}
		if msg.Kind != "done" && (seq > codeeval.MaxCalls || msg.Kind != "call") {
			return
		}
		if codeeval.Send(out, msg) != nil || ctl.Flush() != nil || msg.Kind == "done" {
			return
		}
		res, e := post(ctx, w.replyURL+"?seq="+strconv.Itoa(seq), key, bytes.NewReader(nil))
		if e != nil {
			return
		}
		var reply codeeval.Message
		e = json.NewDecoder(io.LimitReader(res.Body, codeeval.MaxFrame+1)).Decode(&reply)
		res.Body.Close()
		if res.StatusCode != 200 || e != nil || reply.Kind != "reply" {
			return
		}
		if codeeval.Send(inW, reply) != nil {
			return
		}
	}
}

func ListenPort(port string) (string, error) {
	if port == "" {
		port = "80"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strings.TrimSpace(port) != port {
		return "", errors.New("invalid PORT")
	}
	return ":" + port, nil
}
