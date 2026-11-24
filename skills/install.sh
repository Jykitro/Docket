#!/bin/sh
# Plandillo: installs the agent skills and registers the Plandillo MCP server in Claude Code and Codex.
# PLANDILLO_URL supplies the MCP endpoint. Older commands use DOCKET_URL. Credentials stay in the client environment.
set -eu
unset DOCKET_TOKEN # Older setup commands supplied this to the installer.
endpoint=${PLANDILLO_URL:-${DOCKET_URL:-}}
: "${endpoint:?Set PLANDILLO_URL to the MCP endpoint}"
# Hex encodes the full endpoint without collisions and works on both macOS and Linux.
endpoint_key=$(printf %s "$endpoint" | od -An -v -tx1 | tr -d ' \n')
credential=DOCKET_MCP_${endpoint_key}

# Old commands and previously installed skill folders retain their tool and skill names.
install_skills() {
  name=plandillo
  if [ -z "${PLANDILLO_URL:-}" ] || { [ -f "$1/docket/SKILL.md" ] && [ ! -f "$1/plandillo/SKILL.md" ]; }; then
    name=docket
#docket-skills#
  else
#plandillo-skills#
  fi
}

# Put the client label first, preserving any query parameters used by a proxy.
agent_url() {
  case "$endpoint" in
    *'?'*) printf '%s?agent=%s&%s' "${endpoint%%\?*}" "$1" "${endpoint#*\?}" ;;
    *) printf '%s?agent=%s' "$endpoint" "$1" ;;
  esac
}

found=
if command -v claude >/dev/null 2>&1; then
  install_skills "$HOME/.claude/skills"
  claude mcp remove "$name" --scope user >/dev/null 2>&1 || true
  claude mcp add --scope user --transport http "$name" "$(agent_url claude)" --header "Authorization: Bearer \${$credential}"
  echo "Claude Code: skills installed, MCP server added"
  found=1
fi
if command -v codex >/dev/null 2>&1; then
  install_skills "$HOME/.agents/skills"
  codex mcp remove "$name" >/dev/null 2>&1 || true
  # Codex reads the token from an environment variable, so it never lands in config.toml.
  codex mcp add "$name" --url "$(agent_url codex)" --bearer-token-env-var "$credential"
  echo "Codex: skills installed, MCP server added"
  found=1
fi
if [ -z "$found" ]; then
  echo "Neither claude nor codex is in PATH. Install one of them and run this again." >&2
  exit 1
fi
echo "In the shell that starts the client, run the following command and paste the token at the hidden prompt:"
cat <<DOCKET_PROMPT
export ${credential}="\$(bash -c 'IFS= read -rs -p "Plandillo token: " token </dev/tty && printf %s "\$token"')"
DOCKET_PROMPT
echo "Start the client from that shell, or provide the same variable through your credential manager."
