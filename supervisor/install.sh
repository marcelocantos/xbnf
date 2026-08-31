#!/bin/sh
# Render supervisor/xbnf-sandbox.ini into supervisor.d and (re)start xbnf-sandbox.
set -e

REPO="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
CONF_DIR="${SUPERVISOR_CONF_DIR:-/opt/homebrew/etc/supervisor.d}"
DEST="$CONF_DIR/xbnf-sandbox.ini"
TEMPLATE="$REPO/supervisor/xbnf-sandbox.ini"

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi

mkdir -p "$CONF_DIR"
mkdir -p "$HOME/.local/var/log"
chmod +x "$REPO/supervisor/run-sandbox.sh"

if [ ! -x "$REPO/bin/xbnf" ]; then
  (cd "$REPO" && make build)
fi

rm -f "$DEST"
sed "s|@REPO@|$REPO|g" "$TEMPLATE" >"$DEST"

if [ "${SUPERVISOR_SKIP_CTL:-}" = 1 ]; then
  echo "xbnf-sandbox rendered at $DEST (SUPERVISOR_SKIP_CTL=1)"
  exit 0
fi

supervisorctl reread
supervisorctl update
supervisorctl restart xbnf-sandbox 2>/dev/null || supervisorctl start xbnf-sandbox

echo "xbnf-sandbox installed at $DEST (from $TEMPLATE)"
supervisorctl status xbnf-sandbox
