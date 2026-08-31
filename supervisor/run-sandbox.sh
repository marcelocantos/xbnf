#!/bin/sh
# Start xbnf sandbox for supervisord. Prefers a repo-root bin/xbnf build.
set -e

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
export USER="${USER:-$(id -un)}"
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${HOME}/.py/bin:${HOME}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"

BIND="${XBNF_SANDBOX_BIND:-0.0.0.0}"
PORT="${XBNF_SANDBOX_PORT:-8878}"
BIN="$ROOT/bin/xbnf"

if [ ! -x "$BIN" ]; then
  echo "xbnf sandbox: missing $BIN (run make build)" >&2
  exit 1
fi
exec "$BIN" sandbox -bind "$BIND" -port "$PORT"
