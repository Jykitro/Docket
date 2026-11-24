package service

import (
	"regexp"
	"strings"
	"unicode"
)

var actorID = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}-]{0,47}$`)

// AgentActor returns a short, stable label for an MCP client. The profile owner's
// reserved ID can only be assigned by the REST service caller.
func AgentActor(name string) string {
	if len(name) > 256 {
		return "mcp"
	}
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(words) == 0 {
		return "mcp"
	}
	id := strings.Join(words, "-")
	switch id {
	case "claude-ai", "claude-cli", "claude-code", "claude-desktop":
		return ActorClaude
	case "codex-cli", "codex-cli-rs", "codex-mcp-client", "codex-desktop", "codex-app-server":
		return "codex"
	}
	if id == ActorDenis || !actorID.MatchString(id) {
		return "mcp"
	}
	return id
}
