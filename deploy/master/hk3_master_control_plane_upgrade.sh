#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

ARCHIVE_URL=${1:?release archive URL required}
ARCHIVE_SHA256=${2:?archive sha256 required}
EXPECTED_COMMIT=${3:?expected source commit required}
EXPECTED_VERSION=${4:?expected binary version required}
ARCHIVE=/tmp/hk3-master-control-plane-upgrade.tar.gz
EXTRACT=/tmp/hk3-master-control-plane-upgrade
ROOT=/usr/local/x-ui
BIN=$ROOT/x-ui
UNIT=/etc/systemd/system/x-ui.service
ENV_FILE=/etc/default/x-ui
DB_DIR=/etc/x-ui
DB_FILE=$DB_DIR/x-ui.db
BACKUP=/root/neko-vpn-backup-$(date -u +%Y%m%dT%H%M%SZ)-hk3-master-upgrade

[ "$(id -u)" -eq 0 ] || { echo 'must run as root' >&2; exit 1; }
for c in curl sha256sum tar systemctl install cp mv rm stat awk grep python3 find; do
  command -v "$c" >/dev/null || { echo "missing command: $c" >&2; exit 1; }
done
[ -x "$BIN" ] && [ -f "$UNIT" ] && [ -f "$ENV_FILE" ] && [ -f "$DB_FILE" ] || {
  echo 'required Master files are missing' >&2
  exit 1
}

master_before=$(systemctl is-active x-ui.service 2>/dev/null || true)
master_enabled_before=$(systemctl is-enabled x-ui.service 2>/dev/null || true)
singbox_before=$(systemctl is-active sing-box-hk3.service 2>/dev/null || true)
adapter_before=$(systemctl is-active singbox-adapter.service 2>/dev/null || true)
config_before=$(sha256sum /etc/sing-box/config.json | awk '{print $1}')
binary_before=$(sha256sum "$BIN" | awk '{print $1}')

rm -f "$ARCHIVE"
rm -rf "$EXTRACT"
curl -fL --retry 5 --retry-delay 3 --connect-timeout 15 --max-time 600 -o "$ARCHIVE" "$ARCHIVE_URL"
printf '%s  %s\n' "$ARCHIVE_SHA256" "$ARCHIVE" | sha256sum -c -
mkdir -p "$EXTRACT"
tar -xzf "$ARCHIVE" -C "$EXTRACT"
mapfile -t release_dirs < <(find "$EXTRACT" -mindepth 1 -maxdepth 1 -type d -print)
[ "${#release_dirs[@]}" -eq 1 ] || { echo 'archive must contain exactly one top-level release directory' >&2; exit 1; }
RELEASE_DIR="${release_dirs[0]}"
[ -x "$RELEASE_DIR/x-ui" ] || { echo 'Master binary missing' >&2; exit 1; }
[ "$(tr -d '[:space:]' < "$RELEASE_DIR/SOURCE_COMMIT")" = "$EXPECTED_COMMIT" ] || {
  echo 'source commit mismatch' >&2
  exit 1
}
( cd "$RELEASE_DIR" && sha256sum -c SHA256SUM )
version=$($RELEASE_DIR/x-ui -v)
[ "$version" = "$EXPECTED_VERSION" ] || { echo "unexpected version=$version" >&2; exit 1; }

mkdir -p "$BACKUP"
cp -a "$BIN" "$BACKUP/x-ui.before"
cp -a "$UNIT" "$BACKUP/x-ui.service.before"
cp -a "$ENV_FILE" "$BACKUP/x-ui.env.before"
cp -a "$DB_DIR" "$BACKUP/x-ui-dir.before"
printf '%s\n' "$config_before" > "$BACKUP/singbox-config.sha256.before"
printf '%s\n' "$binary_before" > "$BACKUP/x-ui.sha256.before"

restore_service_state() {
  systemctl daemon-reload >/dev/null 2>&1 || true
  case "$master_enabled_before" in
    enabled) systemctl enable x-ui.service >/dev/null 2>&1 || true ;;
    disabled) systemctl disable x-ui.service >/dev/null 2>&1 || true ;;
  esac
  case "$master_before" in
    active) systemctl start x-ui.service >/dev/null 2>&1 || true ;;
    inactive|failed) systemctl stop x-ui.service >/dev/null 2>&1 || true ;;
  esac
}

rollback() {
  set +e
  systemctl stop x-ui.service >/dev/null 2>&1 || true
  cp -a "$BACKUP/x-ui.before" "$BIN"
  cp -a "$BACKUP/x-ui.service.before" "$UNIT"
  cp -a "$BACKUP/x-ui.env.before" "$ENV_FILE"
  rm -rf "$DB_DIR"
  cp -a "$BACKUP/x-ui-dir.before" "$DB_DIR"
  if [ -f "$BACKUP/x-ui.db.backup" ]; then
    cp -a "$BACKUP/x-ui.db.backup" "$DB_FILE"
    chmod 0600 "$DB_FILE"
  fi
  restore_service_state
  echo "ROLLBACK_APPLIED backup=$BACKUP"
}
trap 'rc=$?; if [ "$rc" -ne 0 ]; then rollback; fi; exit "$rc"' EXIT

systemctl stop x-ui.service
# Consistent SQLite backup after all Master writers have stopped.
python3 - "$DB_FILE" "$BACKUP/x-ui.db.backup" <<'PY'
import sqlite3, sys
src, dst = sys.argv[1:]
source = sqlite3.connect(f"file:{src}?mode=ro", uri=True)
target = sqlite3.connect(dst)
try:
    source.backup(target)
    target.commit()
finally:
    target.close()
    source.close()
PY
chmod 0600 "$BACKUP/x-ui.db.backup"
python3 - "$BACKUP/x-ui.db.backup" <<'PY'
import sqlite3, sys
con = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
assert con.execute("pragma quick_check").fetchone()[0] == "ok"
print("MASTER_DB_BACKUP_QUICK_CHECK=PASS")
PY

install -m 0755 "$RELEASE_DIR/x-ui" "$BIN.new"
mv -f "$BIN.new" "$BIN"
# Keep the existing HK3-customized systemd unit; this release is binary-only.
systemctl daemon-reload
case "$master_enabled_before" in
  enabled) systemctl enable x-ui.service >/dev/null ;;
  disabled) systemctl disable x-ui.service >/dev/null ;;
esac
case "$master_before" in
  active) systemctl start x-ui.service ;;
  *) systemctl stop x-ui.service ;;
esac
sleep 3
[ "$(systemctl is-active x-ui.service)" = "$master_before" ] || {
  echo 'Master service state changed unexpectedly' >&2
  exit 1
}

if [ "$master_before" = active ]; then
  . "$DB_DIR/install-result.env"
  code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:${XUI_PANEL_PORT}/${XUI_WEB_BASE_PATH}/")
  case "$code" in 200|301|302|307|308) ;; *) echo "panel health status=$code" >&2; exit 1 ;; esac
fi

config_after=$(sha256sum /etc/sing-box/config.json | awk '{print $1}')
singbox_after=$(systemctl is-active sing-box-hk3.service 2>/dev/null || true)
adapter_after=$(systemctl is-active singbox-adapter.service 2>/dev/null || true)
binary_after=$(sha256sum "$BIN" | awk '{print $1}')
[ "$config_before" = "$config_after" ] || { echo 'sing-box config changed unexpectedly' >&2; exit 1; }
[ "$singbox_before" = "$singbox_after" ] || { echo 'sing-box service state changed unexpectedly' >&2; exit 1; }
[ "$adapter_before" = "$adapter_after" ] || { echo 'adapter service state changed unexpectedly' >&2; exit 1; }
[ "$binary_after" != "$binary_before" ] || { echo 'Master binary did not change' >&2; exit 1; }

node4=$(python3 - "$DB_FILE" <<'PY'
import sqlite3, sys
con = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
row = con.execute("select id, name, enable, status, last_heartbeat from nodes where id=4").fetchone()
print(repr(row))
if not row or int(row[2]) != 0:
    raise SystemExit("Node id=4 is not disabled")
PY
)

trap - EXIT
rm -rf "$EXTRACT" "$ARCHIVE"
printf 'MASTER_CONTROL_PLANE_UPGRADE_OK\n'
printf 'backup=%s\n' "$BACKUP"
printf 'source_commit=%s\n' "$EXPECTED_COMMIT"
printf 'version=%s\n' "$version"
printf 'master_service=%s\n' "$(systemctl is-active x-ui.service)"
printf 'panel_http=%s\n' "${code:-not-run}"
printf 'singbox_service=%s\n' "$singbox_after"
printf 'adapter_service=%s\n' "$adapter_after"
printf 'singbox_config_sha256=%s\n' "$config_after"
printf 'node4=%s\n' "$node4"
printf 'master_binary_sha256=%s\n' "$binary_after"
