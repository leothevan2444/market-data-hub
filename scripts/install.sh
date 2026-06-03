#!/usr/bin/env bash
set -Eeuo pipefail

PREFIX="${MARKET_DATA_INSTALL_PREFIX:-/opt/market-data-hub}"
ENV_DIR="${MARKET_DATA_ENV_DIR:-/etc/market-data-hub}"
ENV_FILE="${MARKET_DATA_ENV_FILE:-$ENV_DIR/env}"
SYSTEMD_DIR="${MARKET_DATA_SYSTEMD_DIR:-/etc/systemd/system}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SUDO=""
SERVICE_TMP=""

cleanup() {
  if [[ -n "$SERVICE_TMP" && -f "$SERVICE_TMP" ]]; then
    rm -f "$SERVICE_TMP"
  fi
}
trap cleanup EXIT

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\/&]/\\&/g'
}

if [[ "${EUID}" -ne 0 ]]; then
  SUDO="sudo"
fi

cd "$REPO_ROOT"

echo "[install] building sync-daily"
mkdir -p bin
go build -o bin/sync-daily ./cmd/sync-daily

echo "[install] installing files to $PREFIX"
$SUDO install -d "$PREFIX/bin" "$PREFIX/scripts" "$PREFIX/config" "$ENV_DIR"
$SUDO install -m 0755 bin/sync-daily "$PREFIX/bin/sync-daily"
$SUDO install -m 0755 scripts/run-sync-daily.sh "$PREFIX/scripts/run-sync-daily.sh"
$SUDO install -m 0644 config/markets.yaml "$PREFIX/config/markets.yaml"
if [[ -f config/watchlist.yaml ]]; then
  $SUDO install -m 0644 config/watchlist.yaml "$PREFIX/config/watchlist.yaml"
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "[install] creating env template at $ENV_FILE"
  $SUDO install -m 0600 deploy/vps/env.example "$ENV_FILE"
else
  echo "[install] keeping existing $ENV_FILE"
fi

echo "[install] installing systemd units"
SERVICE_TMP="$(mktemp)"
sed \
  -e "s/\/opt\/market-data-hub\/current/$(escape_sed_replacement "$PREFIX")/g" \
  -e "s/\/etc\/market-data-hub\/env/$(escape_sed_replacement "$ENV_FILE")/g" \
  deploy/systemd/market-data-sync.service > "$SERVICE_TMP"
$SUDO install -m 0644 "$SERVICE_TMP" "$SYSTEMD_DIR/market-data-sync.service"
$SUDO install -m 0644 deploy/systemd/market-data-sync.timer "$SYSTEMD_DIR/market-data-sync.timer"

echo "[install] enabling timer"
$SUDO systemctl daemon-reload
$SUDO systemctl enable --now market-data-sync.timer

echo "[install] installed. Next checks:"
echo "  systemctl list-timers market-data-sync.timer"
echo "  systemctl status market-data-sync.timer"
echo "  sudo systemctl start market-data-sync.service"
echo "  journalctl -u market-data-sync.service -n 100 --no-pager"
