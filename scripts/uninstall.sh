#!/usr/bin/env bash
set -Eeuo pipefail

PREFIX="${MARKET_DATA_INSTALL_PREFIX:-/opt/market-data-hub}"
ENV_DIR="${MARKET_DATA_ENV_DIR:-/etc/market-data-hub}"
ENV_FILE="${MARKET_DATA_ENV_FILE:-$ENV_DIR/env}"
SYSTEMD_DIR="${MARKET_DATA_SYSTEMD_DIR:-/etc/systemd/system}"
LOCK_FILE="${MARKET_DATA_LOCK_FILE:-/tmp/market-data-sync-daily.lock}"
PURGE_ENV="false"
SUDO=""

for arg in "$@"; do
  case "$arg" in
    --purge-env)
      PURGE_ENV="true"
      ;;
    -h|--help)
      echo "Usage: $0 [--purge-env]"
      echo
      echo "Uninstalls systemd units and installed runtime files."
      echo "By default, keeps $ENV_FILE because it contains secrets."
      exit 0
      ;;
    *)
      echo "[uninstall] unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

if [[ "${EUID}" -ne 0 ]]; then
  SUDO="sudo"
fi

echo "[uninstall] disabling systemd timer"
$SUDO systemctl disable --now market-data-sync.timer 2>/dev/null || true
$SUDO systemctl stop market-data-sync.service 2>/dev/null || true

echo "[uninstall] removing systemd units"
$SUDO rm -f "$SYSTEMD_DIR/market-data-sync.timer" "$SYSTEMD_DIR/market-data-sync.service"
$SUDO systemctl daemon-reload
$SUDO systemctl reset-failed market-data-sync.timer market-data-sync.service 2>/dev/null || true

echo "[uninstall] removing installed runtime files from $PREFIX"
$SUDO rm -f "$PREFIX/bin/sync-daily"
$SUDO rm -f "$PREFIX/scripts/run-sync-daily.sh"
$SUDO rm -f "$PREFIX/config/markets.yaml" "$PREFIX/config/watchlist.yaml"
$SUDO rmdir "$PREFIX/bin" "$PREFIX/scripts" "$PREFIX/config" "$PREFIX" 2>/dev/null || true
$SUDO rmdir "$(dirname "$PREFIX")" 2>/dev/null || true

echo "[uninstall] removing lock file $LOCK_FILE"
$SUDO rm -f "$LOCK_FILE"

if [[ "$PURGE_ENV" == "true" ]]; then
  echo "[uninstall] removing env file $ENV_FILE"
  $SUDO rm -f "$ENV_FILE"
  $SUDO rmdir "$ENV_DIR" 2>/dev/null || true
else
  echo "[uninstall] keeping env file $ENV_FILE"
fi

echo "[uninstall] completed"
