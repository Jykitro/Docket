#!/bin/sh
# Docket: installs the agent skills and registers the Docket MCP server in Claude Code and Codex.
# Needs DOCKET_URL (the MCP endpoint). Credentials stay in the shell that starts the client.
set -eu
unset DOCKET_TOKEN # Older setup commands supplied this to the installer.
: "${DOCKET_URL:?DOCKET_URL is not set}"
# Hex encodes the full endpoint without collisions and works on both macOS and Linux.
endpoint_key=$(printf %s "$DOCKET_URL" | od -An -v -tx1 | tr -d ' \n')
credential=DOCKET_MCP_${endpoint_key}

skills() { # $1: the skills folder of a client
#SKILLS#
}

# Put the client label first, preserving any query parameters used by a proxy.
agent_url() {
  case "$DOCKET_URL" in
    *'?'*) printf '%s?agent=%s&%s' "${DOCKET_URL%%\?*}" "$1" "${DOCKET_URL#*\?}" ;;
    *) printf '%s?agent=%s' "$DOCKET_URL" "$1" ;;
  esac
}

found=
if command -v claude >/dev/null 2>&1; then
  skills "$HOME/.claude/skills"
  claude mcp remove docket --scope user >/dev/null 2>&1 || true
  claude mcp add --scope user --transport http docket "$(agent_url claude)" --header "Authorization: Bearer \${$credential}"
  echo "Claude Code: skills installed, MCP server added"
  found=1
fi
if command -v codex >/dev/null 2>&1; then
  skills "$HOME/.agents/skills"
  codex mcp remove docket >/dev/null 2>&1 || true
  # Codex reads the token from an environment variable, so it never lands in config.toml.
  codex mcp add docket --url "$(agent_url codex)" --bearer-token-env-var "$credential"
  echo "Codex: skills installed, MCP server added"
  found=1
fi
if [ -z "$found" ]; then
  echo "Neither claude nor codex is in PATH. Install one of them and run this again." >&2
  exit 1
fi
echo "In the shell that starts the client, run the following command and paste the token at the hidden prompt:"
cat <<DOCKET_PROMPT
export ${credential}="\$(bash -c 'IFS= read -rs -p "Docket token: " token </dev/tty && printf %s "\$token"')"
DOCKET_PROMPT
echo "Start the client from that shell, or provide the same variable through your credential manager."
