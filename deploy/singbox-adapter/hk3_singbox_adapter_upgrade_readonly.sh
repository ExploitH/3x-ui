#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

ARCHIVE_URL=${1:?release archive URL required}
ARCHIVE_SHA256=${2:?archive sha256 required}
EXPECTED_COMMIT=${3:?expected source commit required}
ARCHIVE=/tmp/hk3-singbox-adapter-readonly-upgrade.tar.gz
EXTRACT=/tmp/hk3-singbox-adapter-readonly-upgrade
BIN=/usr/local/bin/singbox-adapter
UNIT=/etc/systemd/system/singbox-adapter.service
ENV_FILE=/etc/default/singbox-adapter
TOKEN_FILE=/etc/sing-box/adapter.token
BACKUP=/root/neko-vpn-backup-$(date -u +%Y%m%dT%H%M%SZ)-singbox-adapter-42cc46ff

[ "$(id -u)" -eq 0 ] || { echo 'must run as root' >&2; exit 1; }
for c in curl sha256sum tar systemctl ss python3 install cp mv rm stat awk grep; do
  command -v "$c" >/dev/null || { echo "missing command: $c" >&2; exit 1; }
done
[ -f "$BIN" ] && [ -f "$UNIT" ] && [ -f "$ENV_FILE" ] && [ -f "$TOKEN_FILE" ] || {
  echo 'required existing adapter files are missing' >&2
  exit 1
}

managed_before=$(awk -F= '$1=="SINGBOX_ADAPTER_MANAGED" {print $2}' "$ENV_FILE" | tr -d '[:space:]' || true)
case "${managed_before,,}" in
  true|1|yes) echo 'refusing readonly upgrade while managed mode is enabled' >&2; exit 1 ;;
esac

adapter_before=$(systemctl is-active singbox-adapter.service 2>/dev/null || true)
adapter_enabled_before=$(systemctl is-enabled singbox-adapter.service 2>/dev/null || true)
singbox_before=$(systemctl is-active sing-box-hk3.service 2>/dev/null || true)
master_before=$(systemctl is-active x-ui.service 2>/dev/null || true)
config_before=$(sha256sum /etc/sing-box/config.json | awk '{print $1}')
token_hash_before=$(sha256sum "$TOKEN_FILE" | awk '{print $1}')

mkdir -p "$BACKUP"
cp -a "$BIN" "$BACKUP/singbox-adapter.before"
cp -a "$UNIT" "$BACKUP/singbox-adapter.service.before"
cp -a "$ENV_FILE" "$BACKUP/singbox-adapter.env.before"
cp -a "$TOKEN_FILE" "$BACKUP/adapter.token.before"
printf '%s\n' "$config_before" > "$BACKUP/singbox-config.sha256.before"
printf '%s\n' "$token_hash_before" > "$BACKUP/adapter-token.sha256.before"

restore_service_state() {
  systemctl daemon-reload >/dev/null 2>&1 || true
  case "$adapter_enabled_before" in
    enabled) systemctl enable singbox-adapter.service >/dev/null 2>&1 || true ;;
    disabled) systemctl disable singbox-adapter.service >/dev/null 2>&1 || true ;;
  esac
  case "$adapter_before" in
    active) systemctl start singbox-adapter.service >/dev/null 2>&1 || true ;;
    inactive|failed) systemctl stop singbox-adapter.service >/dev/null 2>&1 || true ;;
  esac
}

rollback() {
  set +e
  systemctl stop singbox-adapter.service >/dev/null 2>&1 || true
  cp -a "$BACKUP/singbox-adapter.before" "$BIN"
  cp -a "$BACKUP/singbox-adapter.service.before" "$UNIT"
  cp -a "$BACKUP/singbox-adapter.env.before" "$ENV_FILE"
  cp -a "$BACKUP/adapter.token.before" "$TOKEN_FILE"
  restore_service_state
  echo "ROLLBACK_APPLIED backup=$BACKUP"
}
trap 'rc=$?; if [ "$rc" -ne 0 ]; then rollback; fi; exit "$rc"' EXIT

rm -f "$ARCHIVE"
rm -rf "$EXTRACT"
curl -fL --retry 5 --retry-delay 3 --connect-timeout 15 --max-time 600 -o "$ARCHIVE" "$ARCHIVE_URL"
printf '%s  %s\n' "$ARCHIVE_SHA256" "$ARCHIVE" | sha256sum -c -
mkdir -p "$EXTRACT"
tar -xzf "$ARCHIVE" -C "$EXTRACT"
RELEASE_DIR="$EXTRACT/hk3-singbox-adapter-42cc46ff"
[ -x "$RELEASE_DIR/singbox-adapter" ] || { echo 'adapter binary missing' >&2; exit 1; }
[ -f "$RELEASE_DIR/singbox-adapter.service" ] || { echo 'adapter unit missing' >&2; exit 1; }
[ "$(tr -d '[:space:]' < "$RELEASE_DIR/SOURCE_COMMIT")" = "$EXPECTED_COMMIT" ] || {
  echo 'source commit mismatch' >&2
  exit 1
}
( cd "$RELEASE_DIR" && sha256sum -c SHA256SUM )

systemctl stop singbox-adapter.service
install -m 0755 "$RELEASE_DIR/singbox-adapter" "$BIN.new"
install -m 0644 "$RELEASE_DIR/singbox-adapter.service" "$UNIT.new"
mv -f "$BIN.new" "$BIN"
mv -f "$UNIT.new" "$UNIT"
systemctl daemon-reload
case "$adapter_enabled_before" in
  enabled) systemctl enable singbox-adapter.service >/dev/null ;;
  disabled) systemctl disable singbox-adapter.service >/dev/null ;;
esac
case "$adapter_before" in
  active) systemctl start singbox-adapter.service ;;
  *) systemctl stop singbox-adapter.service ;;
esac
sleep 2
[ "$(systemctl is-active singbox-adapter.service)" = "$adapter_before" ] || {
  echo 'adapter service state changed unexpectedly' >&2
  exit 1
}

if [ "$adapter_before" = active ]; then
  TOKEN=$(<"$TOKEN_FILE")
  BASE=http://127.0.0.1:23854/adapter
  curl_auth() {
    local url="$1"
    shift
    printf 'Authorization: Bearer %s\n' "$TOKEN" | curl -fsS --max-time 5 -H @- "$@" "$url"
  }
  health=$(curl_auth "$BASE/healthz")
  capabilities=$(curl_auth "$BASE/panel/api/server/capabilities")
  list=$(curl_auth "$BASE/panel/api/inbounds/list")
  status=$(curl_auth "$BASE/panel/api/server/status")
  wrong=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer invalid' "$BASE/healthz")
  mutation=$(printf 'Authorization: Bearer %s\n' "$TOKEN" | curl -sS -o /dev/null -w '%{http_code}' -X POST -H @- "$BASE/panel/api/clients/add")
  printf '%s' "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); o=d.get("obj",{}); assert d.get("success") is True and o.get("mode")=="readonly"'
  printf '%s' "$capabilities" | python3 -c 'import json,sys; o=json.load(sys.stdin).get("obj",{}); assert o.get("mode")=="readonly" and o.get("config") is True and o.get("inboundInventory") is True and not any(o.get(k) for k in ("clientCrud","clientEnable","perClientTraffic","trafficReset","clientIp","relayIdentity"))'
  printf '%s' "$list" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("success") is True and len(d.get("obj") or []) >= 1'
  printf '%s' "$status" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("success") is True and d.get("obj",{}).get("panelVersion")=="singbox-adapter-readonly"'
  [ "$wrong" = 401 ]
  [ "$mutation" = 405 ]
fi

config_after=$(sha256sum /etc/sing-box/config.json | awk '{print $1}')
token_hash_after=$(sha256sum "$TOKEN_FILE" | awk '{print $1}')
singbox_after=$(systemctl is-active sing-box-hk3.service 2>/dev/null || true)
master_after=$(systemctl is-active x-ui.service 2>/dev/null || true)
adapter_after=$(systemctl is-active singbox-adapter.service 2>/dev/null || true)
[ "$config_before" = "$config_after" ] || { echo 'sing-box config changed unexpectedly' >&2; exit 1; }
[ "$token_hash_before" = "$token_hash_after" ] || { echo 'adapter token changed unexpectedly' >&2; exit 1; }
[ "$singbox_before" = "$singbox_after" ] || { echo 'sing-box state changed unexpectedly' >&2; exit 1; }
[ "$master_before" = "$master_after" ] || { echo 'Master state changed unexpectedly' >&2; exit 1; }

trap - EXIT
rm -rf "$EXTRACT" "$ARCHIVE"
printf 'ADAPTER_READONLY_UPGRADE_OK\n'
printf 'backup=%s\n' "$BACKUP"
printf 'source_commit=%s\n' "$EXPECTED_COMMIT"
printf 'adapter_service=%s\n' "$adapter_after"
printf 'adapter_enabled=%s\n' "$adapter_enabled_before"
printf 'listen=127.0.0.1:23854\n'
printf 'managed_mode=disabled\n'
printf 'wrong_token_http=%s\n' "${wrong:-not-run}"
printf 'mutation_http=%s\n' "${mutation:-not-run}"
printf 'singbox_service=%s\n' "$singbox_after"
printf 'master_service=%s\n' "$master_after"
printf 'singbox_config_sha256=%s\n' "$config_after"
printf 'adapter_token_sha256=%s\n' "$token_hash_after"
