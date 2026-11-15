#!/usr/bin/env bash
# Shows the app's screens in a browser (http://localhost:8080) against a fake
# Slack: a gateway with sample rules (internal/devpreview) and the GUI in
# Wails server mode. The real config, data and Keychain tokens are not used.
#
#   scripts/preview.sh                 # or: mise run preview
#   PREVIEW_RESET=1 scripts/preview.sh # start over with the sample rules
#   PREVIEW_NO_IM=1 scripts/preview.sh # a Slack app without im:history
#   PREVIEW_NO_FILES=1 scripts/preview.sh # a Slack app without files:read
#   PREVIEW_UPDATE=github scripts/preview.sh # find the real latest release (after a minute)
#   curl 'http://127.0.0.1:8766/dm?text=todo%20look&file=app.log'  # a DM with a file
#   curl 'http://127.0.0.1:8766/dm?text=123456'   # a DM from 田中 太郎
#
# The data lives in ~/.cache/sockdock-preview (a short path: the control
# socket's path must fit in about 100 bytes).
set -euo pipefail
cd "$(dirname "$0")/.."

export SOCKDOCK_DATA_DIR="${SOCKDOCK_PREVIEW_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/sockdock-preview}"
export SOCKDOCK_CONFIG="$SOCKDOCK_DATA_DIR/config.yaml"
export SOCKDOCK_KEYCHAIN_SERVICE=sockdock-preview
export SLACK_APP_TOKEN=xapp-1-preview SLACK_BOT_TOKEN=xoxb-preview

if [ "${PREVIEW_RESET:-}" = 1 ]; then rm -rf "$SOCKDOCK_DATA_DIR"; fi
mkdir -p "$SOCKDOCK_DATA_DIR"

(cd gui/frontend && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build >/dev/null && touch dist/.gitkeep)
go build -o "$SOCKDOCK_DATA_DIR/devpreview" ./internal/devpreview

"$SOCKDOCK_DATA_DIR/devpreview" &
gateway=$!
trap 'kill "$gateway" 2>/dev/null || true' EXIT INT TERM
for _ in $(seq 100); do
  [ -S "$SOCKDOCK_DATA_DIR/control.sock" ] && break
  kill -0 "$gateway" 2>/dev/null || exit 1
  sleep 0.1
done

go run -tags server ./gui
