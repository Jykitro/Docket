#!/bin/sh
# Plandillo: installs plandillo-cli from a GitHub release, checks its SHA-256 and signs it in to PLANDILLO_URL.
# PLANDILLO_BIN_DIR picks the folder (default ~/.local/bin); PLANDILLO_CLI_VERSION pins a release such as 0.1.0.
set -eu
repo=huntly-digital/plandillo
board=${PLANDILLO_URL:-}
dir=${PLANDILLO_BIN_DIR:-$HOME/.local/bin}
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "This installer supports macOS and Linux. On Windows run install.ps1 in PowerShell." >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "No plandillo-cli build for $(uname -m)." >&2; exit 1 ;;
esac
if [ -n "${PLANDILLO_CLI_VERSION:-}" ]; then
  base=https://github.com/$repo/releases/download/v${PLANDILLO_CLI_VERSION#v}
else
  base=https://github.com/$repo/releases/latest/download
fi
file=plandillo-cli_${os}_${arch}.tar.gz

tmp=$(mktemp -d "${TMPDIR:-/tmp}/plandillo-cli.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$file" "$base/$file"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/checksums.txt")
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
  echo "The checksum of $file does not match checksums.txt; nothing was installed." >&2
  exit 1
fi
tar -xzf "$tmp/$file" -C "$tmp" plandillo-cli
mkdir -p "$dir"
cp "$tmp/plandillo-cli" "$dir/plandillo-cli.new"
chmod 755 "$dir/plandillo-cli.new"
mv "$dir/plandillo-cli.new" "$dir/plandillo-cli"
echo "Installed $dir/plandillo-cli $("$dir/plandillo-cli" version)"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to PATH in your shell profile: export PATH=\"$dir:\$PATH\"" ;;
esac
if [ -z "$board" ]; then
  echo "Sign in to your board with: plandillo-cli login https://your-board"
  exit 0
fi
"$dir/plandillo-cli" login "$board" || {
  echo "Sign in later with: plandillo-cli login $board" >&2
  exit 1
}
