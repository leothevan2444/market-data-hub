#!/usr/bin/env bash
set -Eeuo pipefail

LOCK_FILE="/tmp/market-data-sync-daily.lock"
HOME_DIR="${MARKET_DATA_HOME:-/opt/market-data-hub}"
BIN="${MARKET_DATA_SYNC_BIN:-$HOME_DIR/bin/sync-daily}"
MARKET="${MARKET_DATA_MARKET:-us}"
STORAGE="${MARKET_DATA_STORAGE:-r2}"
ROOT="${MARKET_DATA_ROOT:-data}"

mkdir -p "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[sync-daily] another sync is already running; skipping"
  exit 0
fi

cd "$HOME_DIR"

args=(--storage "$STORAGE" --market "$MARKET")
if [[ "$STORAGE" == "local" ]]; then
  args+=(--root "$ROOT")
fi

echo "[sync-daily] starting market=$MARKET storage=$STORAGE date=auto"
"$BIN" "${args[@]}"
echo "[sync-daily] completed"
