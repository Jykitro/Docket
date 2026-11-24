package mcpserver

// Code Mode, after Cloudflare's (developers.cloudflare.com/agents/tools/codemode/): instead of one tool call
// per step, the assistant sends one `code` call with JavaScript that calls the Docket tools as
// `await codemode.<tool>(args)`, loops and filters, and returns only the value it needs. The code runs in
// goja inside this process with no network, timers or files. Every codemode.* call goes to the classic
// server over an in-memory MCP session, so the token, access mode, tool switches and call log apply to it
// exactly as to a direct call.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"docket/internal/service"
	"docket/skills"
)

var codeTimeout = 30 * time.Second // a var so the test can shorten it

const (
	codeMaxCalls  = 100
	codeMaxResult = 50_000 // bytes of result JSON; the assistant pays for every one
	codeMaxLogs   = 50
)

const codePreamble = "Run JavaScript inside Docket. `code` is an async arrow function such as " +
	"`async () => { const t = await codemode.get_today({}); return t.overdue.length }`, or just the statements of its body. " +
	"Call Docket tools as `await codemode.<tool>(args)`: each returns the same JSON as the MCP tool of that name and " +
	"a failed call throws Error(\"code: message\"). Return only what you need: the return value and console.log output " +
	"come back as {result, logs}. Use it to chain calls, loop over tasks and filter without sending intermediate data " +
	"through the chat. Limits: 30 s, 100 tool calls, 50 KB result; no fetch, timers, require or files.\n\n"

type codeIn struct {
	Code string `json:"code" jsonschema:"async arrow function, or the statements of its body; return the value you need"`
}

// codeServer is the /mcp/code server: one tool, `code`, whose calls go to tools (the classic server).
func codeServer(svc *service.Service, tools *mcp.Server, execute evaluator) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "docket-code", Version: "1.0.0"}, &mcp.ServerOptions{Instructions: skills.Instructions()})
	mcp.AddTool(s, &mcp.Tool{Name: "code", Description: codePreamble},
		func(ctx context.Context, _ *mcp.CallToolRequest, in codeIn) (*mcp.CallToolResult, any, error) {
			sess, err := connect(ctx, tools)
			if err != nil {
				return nil, nil, err
			}
			defer sess.Close()
			callsCtx, cancelCalls := context.WithTimeout(ctx, codeTimeout)
			defer cancelCalls()
			out, err := execute(ctx, in.Code, func(name string, args json.RawMessage) (string, error) {
				res, err := sess.CallTool(callsCtx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil {
					return "", fmt.Errorf("invalid_argument: %s: %v", name, err)
				}
				var text strings.Builder
				for _, c := range res.Content {
					if tc, ok := c.(*mcp.TextContent); ok {
						text.WriteString(tc.Text)
					}
				}
				if res.IsError {
					return "", errors.New(text.String())
				}
				return text.String(), nil
			})
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil, nil
		})
	s.AddReceivingMiddleware(policy(svc), declare(tools))
	return s
}

// connect opens an in-memory client session to s; closing it ends the server side too.
func connect(ctx context.Context, s *mcp.Server) (*mcp.ClientSession, error) {
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		return nil, err
	}
	return mcp.NewClient(&mcp.Implementation{Name: actor(ctx), Version: "1.0.0"}, nil).Connect(ctx, ct, nil)
}

// declare appends TypeScript declarations of the tools the classic server lists right now (so switched-off
// tools and, in read-only mode, write tools are left out) to the description of `code` in tools/list.
func declare(tools *mcp.Server) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			lr, ok := res.(*mcp.ListToolsResult)
			if method != "tools/list" || !ok || err != nil {
				return res, err
			}
			sess, err := connect(ctx, tools)
			if err != nil {
				return nil, err
			}
			defer sess.Close()
			tl, err := sess.ListTools(ctx, nil)
			if err != nil {
				return nil, err
			}
			for i, t := range lr.Tools {
				if t.Name == "code" {
					c := *t // the listed tool is the registered one: never mutate it
					c.Description = codePreamble + declarations(tl.Tools)
					lr.Tools[i] = &c
				}
			}
			return lr, nil
		}
	}
}

func declarations(tools []*mcp.Tool) string {
	var b strings.Builder
	b.WriteString("declare const codemode: {\n")
	for _, t := range tools {
		var schema map[string]any
		raw, _ := json.Marshal(t.InputSchema)
		json.Unmarshal(raw, &schema)
		fmt.Fprintf(&b, "  /** %s */\n  %s(args: %s): Promise<any>;\n", t.Description, t.Name, tsType(schema, "  "))
	}
	b.WriteString("};")
	return b.String()
}

// tsType renders the JSON Schema subset the go-sdk derives from Go structs as a TypeScript type.
func tsType(s map[string]any, indent string) string {
	var kinds []string
	switch k := s["type"].(type) {
	case string:
		kinds = []string{k}
	case []any:
		for _, x := range k {
			if x, ok := x.(string); ok && x != "null" {
				kinds = append(kinds, x)
			}
		}
		if len(kinds) < len(k) {
			kinds = append(kinds, "null")
		}
	}
	var parts []string
	for _, k := range kinds {
		switch k {
		case "string", "boolean", "null":
			parts = append(parts, k)
		case "integer", "number":
			parts = append(parts, "number")
		case "array":
			items, _ := s["items"].(map[string]any)
			it := tsType(items, indent)
			if strings.Contains(it, " | ") {
				it = "(" + it + ")"
			}
			parts = append(parts, it+"[]")
		case "object":
			parts = append(parts, tsObject(s, indent))
		}
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " | ")
}

func tsObject(s map[string]any, indent string) string {
	props, _ := s["properties"].(map[string]any)
	if len(props) == 0 {
		return "Record<string, unknown>"
	}
	required := map[string]bool{}
	if rs, ok := s["required"].([]any); ok {
		for _, r := range rs {
			if r, ok := r.(string); ok {
				required[r] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { // required first, then by name
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	inner := indent + "  "
	var b strings.Builder
	b.WriteString("{\n")
	for _, n := range names {
		p, _ := props[n].(map[string]any)
		if d, _ := p["description"].(string); d != "" {
			fmt.Fprintf(&b, "%s/** %s */\n", inner, d)
		}
		opt := "?"
		if required[n] {
			opt = ""
		}
		fmt.Fprintf(&b, "%s%s%s: %s;\n", inner, n, opt, tsType(p, inner))
	}
	b.WriteString(indent + "}")
	return b.String()
}

// runCode runs code in a fresh goja runtime and returns {"result":…,"logs":[…]} as JSON. call runs one
// Docket tool with JSON arguments and returns its JSON text, or an error worded "code: message".
// Local mode calls this directly. Selfhost runs it only inside the isolated worker.
func runCode(ctx context.Context, code string, call func(name string, args json.RawMessage) (string, error)) (string, error) {
	vm := goja.New()
	timer := time.AfterFunc(codeTimeout, func() { vm.Interrupt("timeout") })
	defer timer.Stop()
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("canceled") })
	defer stop()

	js := vm.Get("JSON").ToObject(vm)
	parse, _ := goja.AssertFunction(js.Get("parse"))
	stringify, _ := goja.AssertFunction(js.Get("stringify"))

	calls := 0
	cm := vm.NewObject()
	for _, t := range service.MCPTools {
		name := t.Name
		cm.Set(name, func(fc goja.FunctionCall) goja.Value {
			p, resolve, reject := vm.NewPromise()
			out, err := func() (goja.Value, error) {
				if calls++; calls > codeMaxCalls {
					return nil, fmt.Errorf("invalid_argument: more than %d tool calls in one run", codeMaxCalls)
				}
				args := "{}"
				if a := fc.Argument(0); !goja.IsUndefined(a) && !goja.IsNull(a) {
					s, err := stringify(goja.Undefined(), a)
					if err != nil {
						return nil, fmt.Errorf("invalid_argument: %s: arguments are not JSON: %v", name, err)
					}
					args = s.String()
				}
				text, err := call(name, []byte(args))
				if err != nil {
					return nil, err
				}
				return parse(goja.Undefined(), vm.ToValue(text))
			}()
			if err != nil {
				reject(vm.NewGoError(err))
			} else {
				resolve(out)
			}
			return vm.ToValue(p)
		})
	}
	vm.Set("codemode", cm)

	logs := []string{}
	console := vm.NewObject()
	logf := func(fc goja.FunctionCall) goja.Value {
		if len(logs) > codeMaxLogs {
			return goja.Undefined()
		}
		if len(logs) == codeMaxLogs {
			logs = append(logs, "… more console output dropped")
			return goja.Undefined()
		}
		parts := make([]string, len(fc.Arguments))
		for i, a := range fc.Arguments {
			parts[i] = a.String()
			if _, isObj := a.(*goja.Object); isObj {
				if s, err := stringify(goja.Undefined(), a); err == nil && !goja.IsUndefined(s) {
					parts[i] = s.String()
				}
			}
		}
		line := []rune(strings.Join(parts, " "))
		if len(line) > 1000 {
			line = append(line[:1000], '…')
		}
		logs = append(logs, string(line))
		return goja.Undefined()
	}
	for _, k := range []string{"log", "info", "warn", "error", "debug"} {
		console.Set(k, logf)
	}
	vm.Set("console", console)

	src := strings.TrimRight(strings.TrimSpace(code), "; \n\t")
	prog, err := goja.Compile("code.js", "("+src+"\n)", false)
	if err != nil { // not an expression: treat it as the body of an async function
		prog, err = goja.Compile("code.js", "(async () => {\n"+src+"\n})", false)
	}
	if err != nil {
		return "", fmt.Errorf("invalid_argument: %v", err)
	}
	v, err := vm.RunProgram(prog)
	if fn, ok := goja.AssertFunction(v); ok && err == nil {
		v, err = fn(goja.Undefined())
	}
	if err != nil {
		return "", jsError(err)
	}
	if p, ok := v.Export().(*goja.Promise); ok {
		switch p.State() {
		case goja.PromiseStateRejected:
			return "", thrown(p.Result())
		case goja.PromiseStatePending:
			return "", errors.New("invalid_argument: the code awaits something that never settles; only codemode calls can be awaited")
		}
		v = p.Result()
	}
	result := "null"
	if s, err := stringify(goja.Undefined(), v); err != nil {
		return "", jsError(err)
	} else if !goja.IsUndefined(s) {
		result = s.String()
	}
	if len(result) > codeMaxResult {
		return "", fmt.Errorf("invalid_argument: the result is %d bytes, more than %d: return less (filter, pick fields, count)", len(result), codeMaxResult)
	}
	b, err := json.Marshal(struct {
		Result json.RawMessage `json:"result"`
		Logs   []string        `json:"logs,omitempty"`
	}{json.RawMessage(result), logs})
	return string(b), err
}

func jsError(err error) error {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		if ie.Value() == "timeout" {
			return fmt.Errorf("timeout: the code ran longer than %s", codeTimeout)
		}
		return errors.New("internal: the request was canceled")
	}
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return thrown(ex.Value())
	}
	return fmt.Errorf("invalid_argument: %v", err)
}

// thrown keeps "code: message" of a failed tool call that the code did not catch; anything else the code
// threw (a TypeError, its own Error) is the caller's mistake.
func thrown(v goja.Value) error {
	if o, ok := v.(*goja.Object); ok {
		if m := o.Get("message"); m != nil {
			if code, _, found := strings.Cut(m.String(), ":"); found && knownCodes[code] {
				return errors.New(m.String())
			}
		}
	}
	return fmt.Errorf("invalid_argument: %s", v)
}
