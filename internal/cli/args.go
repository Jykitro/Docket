package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// schemaOf returns the tool's input schema as plain JSON values.
func schemaOf(t *mcp.Tool) map[string]any {
	var out map[string]any
	if b, err := json.Marshal(t.InputSchema); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

// readArg reads a literal, @file or - (stdin).
func readArg(arg string, stdin io.Reader) ([]byte, error) {
	switch {
	case arg == "-":
		return io.ReadAll(stdin)
	case strings.HasPrefix(arg, "@"):
		b, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, usageError("%v", err)
		}
		return b, nil
	}
	return []byte(arg), nil
}

// buildArgs merges one JSON object argument with --name value options; options win.
// Each option must be a top-level property of the tool's schema and is converted to its type.
func buildArgs(args []string, schema map[string]any, stdin io.Reader) (map[string]any, error) {
	props, _ := schema["properties"].(map[string]any)
	out, opts := map[string]any{}, map[string]any{}
	seenJSON := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if name, ok := strings.CutPrefix(arg, "--"); ok && name != "" {
			name, val, hasVal := strings.Cut(name, "=")
			prop, ok := props[name].(map[string]any)
			if !ok {
				return nil, usageError("unknown option --%s; options: %s", name, optionList(props))
			}
			if !hasVal {
				if isBool(prop) {
					val = "true"
				} else if i+1 < len(args) {
					i++
					val = args[i]
				} else {
					return nil, usageError("--%s needs a value", name)
				}
			}
			v, err := convert(prop, val)
			if err != nil {
				return nil, usageError("--%s: %v", name, err)
			}
			opts[name] = v
			continue
		}
		if seenJSON {
			return nil, usageError("unexpected argument %q: give one JSON object and --name value options", arg)
		}
		seenJSON = true
		b, err := readArg(arg, stdin)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &out); err != nil || out == nil {
			return nil, usageError("the argument must be a JSON object such as '{\"id\":\"PLD-12\"}' (%v)", err)
		}
	}
	for k, v := range opts {
		out[k] = v
	}
	required, _ := schema["required"].([]any)
	for _, r := range required {
		if name, ok := r.(string); ok && out[name] == nil {
			return nil, usageError("missing --%s; options: %s", name, optionList(props))
		}
	}
	return out, nil
}

func types(prop map[string]any) []string {
	switch t := prop["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func isBool(prop map[string]any) bool {
	ts := types(prop)
	return slices.Contains(ts, "boolean") && !slices.Contains(ts, "string")
}

// convert turns an option value into the schema type; arrays and objects are given as JSON.
func convert(prop map[string]any, val string) (any, error) {
	ts := types(prop)
	switch {
	case slices.Contains(ts, "string"):
		return val, nil
	case slices.Contains(ts, "integer"):
		return strconv.ParseInt(val, 10, 64)
	case slices.Contains(ts, "number"):
		return strconv.ParseFloat(val, 64)
	case slices.Contains(ts, "boolean"):
		return strconv.ParseBool(val)
	}
	var v any
	if err := json.Unmarshal([]byte(val), &v); err != nil {
		return nil, fmt.Errorf("expected JSON: %v", err)
	}
	return v, nil
}

func optionList(props map[string]any) string {
	if len(props) == 0 {
		return "none"
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, "--"+n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
