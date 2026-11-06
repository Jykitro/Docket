#!/bin/sh
# Docket: installs the agent skills and registers the Docket MCP server in Claude Code and Codex.
# Needs DOCKET_URL (the /mcp address) and DOCKET_TOKEN. Safe to run again.
set -eu
: "${DOCKET_URL:?DOCKET_URL is not set}" "${DOCKET_TOKEN:?DOCKET_TOKEN is not set}"

skills() { # $1: the skills folder of a client
#SKILLS#
}

found=
if command -v claude >/dev/null 2>&1; then
  skills "$HOME/.claude/skills"
  claude mcp remove docket --scope user >/dev/null 2>&1 || true
  claude mcp add --scope user --transport http docket "$DOCKET_URL" --header "Authorization: Bearer $DOCKET_TOKEN"
  echo "Claude Code: skills installed, MCP server added"
  found=1
fi
if command -v codex >/dev/null 2>&1; then
  skills "$HOME/.agents/skills"
  codex mcp remove docket >/dev/null 2>&1 || true
  # Codex reads the token from an environment variable, so it never lands in config.toml.
  codex mcp add docket --url "$DOCKET_URL" --bearer-token-env-var DOCKET_MCP_TOKEN
  echo "Codex: skills installed, MCP server added"
  if [ "${DOCKET_MCP_TOKEN:-}" != "$DOCKET_TOKEN" ]; then
    echo "Codex also needs the token in your shell profile: export DOCKET_MCP_TOKEN=$DOCKET_TOKEN"
  fi
  found=1
fi
if [ -z "$found" ]; then
  echo "Neither claude nor codex is in PATH. Install one of them and run this again." >&2
  exit 1
fi
echo "Done. Restart the client to pick up the skills."
