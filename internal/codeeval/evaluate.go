// Package codeeval runs the same restricted JavaScript on native and WASI targets.
package codeeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"docket/internal/toolcatalog"

	"github.com/dop251/goja"
)

const (
	MaxCalls      = 100
	codeMaxResult = 50_000
	codeMaxLogs   = 50
)

var KnownCodes = map[string]bool{"page_too_large": true, "quota_exceeded": true, "not_found": true, "invalid_argument": true, "forbidden": true, "tool_disabled": true,
	"status_in_use": true, "conflict": true, "internal": true, "timeout": true, "unavailable": true, "rate_limited": true}

// runCode runs code in a fresh goja runtime and returns {"result":…,"logs":[…]} as JSON. call runs one
// Plandillo tool with JSON arguments and returns its JSON text, or an error worded "code: message".
// Local mode calls this directly. Selfhost runs it only inside the isolated worker.
func Run(ctx context.Context, timeout time.Duration, code string, call func(name string, args json.RawMessage) (string, error)) (string, error) {
	vm := goja.New()
	timer := time.AfterFunc(timeout, func() { vm.Interrupt("timeout") })
	defer timer.Stop()
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("canceled") })
	defer stop()

	js := vm.Get("JSON").ToObject(vm)
	parse, _ := goja.AssertFunction(js.Get("parse"))
	stringify, _ := goja.AssertFunction(js.Get("stringify"))

	calls := 0
	cm := vm.NewObject()
	for _, t := range toolcatalog.MCPTools {
		name := t.Name
		cm.Set(name, func(fc goja.FunctionCall) goja.Value {
			p, resolve, reject := vm.NewPromise()
			out, err := func() (goja.Value, error) {
				if calls++; calls > MaxCalls {
					return nil, fmt.Errorf("invalid_argument: more than %d tool calls in one run", MaxCalls)
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
		return "", jsError(err, timeout)
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
		return "", jsError(err, timeout)
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

func jsError(err error, timeout time.Duration) error {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		if ie.Value() == "timeout" {
			return fmt.Errorf("timeout: the code ran longer than %s", timeout)
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
			if code, _, found := strings.Cut(m.String(), ":"); found && KnownCodes[code] {
				return errors.New(m.String())
			}
		}
	}
	return fmt.Errorf("invalid_argument: %s", v)
}
