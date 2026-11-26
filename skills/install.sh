#!/bin/sh
# Plandillo: installs the agent skills and registers the Plandillo MCP server in Claude Code and Codex.
# PLANDILLO_URL supplies the MCP endpoint; the client signs in through the browser, so no credential passes through here.
set -eu
endpoint=${PLANDILLO_URL:-}
: "${endpoint:?Set PLANDILLO_URL to the MCP endpoint}"
client=${PLANDILLO_CLIENT:-all}
case "$client" in
  all|codex|claude-code) ;;
  *) echo 'PLANDILLO_CLIENT must be codex, claude-code or all.' >&2; exit 1 ;;
esac

install_skills() {
#plandillo-skills#
}

found=
if [ "$client" != codex ] && command -v claude >/dev/null 2>&1; then
  install_skills "$HOME/.claude/skills"
  claude mcp remove plandillo --scope user >/dev/null 2>&1 || true
  claude mcp add --scope user --transport http plandillo "$endpoint"
  echo "Claude Code: skills installed, MCP server added"
  echo "To authorize Plandillo in your browser, run: claude mcp login plandillo (or open /mcp in Claude Code)."
  found=1
fi
if [ "$client" != claude-code ] && command -v codex >/dev/null 2>&1; then
  install_skills "$HOME/.agents/skills"
  codex mcp remove plandillo >/dev/null 2>&1 || true
  codex mcp add plandillo --url "$endpoint"
  echo "Codex: skills installed, MCP server added"
  echo "If you still need to sign in, run: codex mcp login plandillo"
  found=1
fi
if [ -z "$found" ]; then
  case "$client" in
    all) echo "Neither claude nor codex is in PATH. Install one of them and run this again." >&2 ;;
    *) echo "Install $client and run its Plandillo setup again." >&2 ;;
  esac
  exit 1
fi
