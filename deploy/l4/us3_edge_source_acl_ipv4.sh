#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

TABLE=neko_us3_edge_acl_ipv4
FAMILY=inet
RULES=/etc/nftables.d/neko-us3-edge-acl-ipv4.nft
UNIT=/etc/systemd/system/neko-us3-edge-acl-ipv4.service
DROPIN_DIR=/etc/systemd/system/sing-box.service.d
DROPIN=$DROPIN_DIR/90-neko-us3-edge-acl-ipv4.conf
DROPIN_DIR_WAS_PRESENT=0
[ -d "$DROPIN_DIR" ] && DROPIN_DIR_WAS_PRESENT=1
STATE_DIR=/var/lib/neko-us3-edge-acl-ipv4
STATE=$STATE_DIR/last-backup
TRUSTED_RELAY_IPV4S='141.11.148.116,91.229.132.66,70.36.96.197,152.175.34.230,47.243.126.59'
PROXY_TCP_PORTS='8881,8885,8886,8889,8890,8891'
PROXY_UDP_PORTS='8882,8883,8885'
BACKUP=/root/neko-vpn-backup-$(date -u +%Y%m%dT%H%M%SZ)-us3-edge-acl-ipv4

require() { command -v "$1" >/dev/null || { echo "missing command: $1" >&2; exit 1; }; }
for command in nft systemctl install mkdir rm cp sha256sum sed awk tr grep; do require "$command"; done
[ "$(id -u)" -eq 0 ] || { echo 'must run as root' >&2; exit 1; }

service_state() { systemctl is-active "$1" 2>/dev/null || true; }

write_rules() {
  cat > "$RULES" <<EOF
# US3 IPv4 source ACL: only HK2 and existing trusted relay sources may reach direct proxy ports.
table $FAMILY $TABLE {
    set trusted_relay_ipv4 {
        type ipv4_addr
        elements = { $TRUSTED_RELAY_IPV4S }
    }

    chain input {
        type filter hook input priority -50; policy accept;

        iifname "lo" accept

        ip saddr @trusted_relay_ipv4 tcp dport { $PROXY_TCP_PORTS } accept
        ip saddr @trusted_relay_ipv4 udp dport { $PROXY_UDP_PORTS } accept

        ip protocol tcp tcp dport { $PROXY_TCP_PORTS } drop
        ip protocol udp udp dport { $PROXY_UDP_PORTS } drop

        ct state established,related accept
    }
}
EOF
}

write_unit() {
  cat > "$UNIT" <<EOF
[Unit]
Description=Neko US3 IPv4 source ACL for HK2 and existing relay sources
Wants=network-online.target
After=network-online.target
Before=sing-box.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f $RULES
ExecStop=-/usr/sbin/nft delete table $FAMILY $TABLE

[Install]
WantedBy=multi-user.target
EOF
}

write_dropin() {
  mkdir -p "$DROPIN_DIR"
  cat > "$DROPIN" <<EOF
[Unit]
Requires=neko-us3-edge-acl-ipv4.service
After=neko-us3-edge-acl-ipv4.service
EOF
}

remove_own() {
  set +e
  systemctl stop neko-us3-edge-acl-ipv4.service >/dev/null 2>&1 || true
  nft delete table "$FAMILY" "$TABLE" >/dev/null 2>&1 || true
  systemctl disable neko-us3-edge-acl-ipv4.service >/dev/null 2>&1 || true
  rm -f "$RULES" "$UNIT" "$DROPIN"
  if [ "$DROPIN_DIR_WAS_PRESENT" -eq 0 ]; then rmdir "$DROPIN_DIR" 2>/dev/null || true; fi
  systemctl daemon-reload >/dev/null 2>&1 || true
}

apply() {
  [ ! -e "$RULES" ] && [ ! -L "$RULES" ] || { echo "rules path exists or is a symlink: $RULES" >&2; exit 1; }
  [ ! -e "$UNIT" ] && [ ! -L "$UNIT" ] || { echo "unit path exists or is a symlink: $UNIT" >&2; exit 1; }
  [ ! -e "$DROPIN" ] && [ ! -L "$DROPIN" ] || { echo "drop-in exists: $DROPIN" >&2; exit 1; }
  if nft list table "$FAMILY" "$TABLE" >/dev/null 2>&1; then
    echo "nft table exists: $FAMILY $TABLE" >&2
    exit 1
  fi

  mkdir -p "$(dirname "$RULES")" "$BACKUP"
  printf '%s\n' "$(service_state sing-box.service)" > "$BACKUP/singbox.service.before"
  printf '%s\n' "$(service_state singbox-adapter.service)" > "$BACKUP/adapter.service.before"
  sha256sum /etc/sing-box/conf/config.json > "$BACKUP/singbox-config.sha256.before"
  nft list ruleset > "$BACKUP/nft-ruleset.before"
  trap 'rc=$?; if [ "$rc" -ne 0 ]; then rollback || rc=70; fi; exit "$rc"' EXIT
  write_rules
  write_unit
  write_dropin
  chmod 0644 "$RULES" "$UNIT"
  chmod 0644 "$DROPIN"
  sha256sum "$RULES" > "$BACKUP/rules.sha256"
  sha256sum "$UNIT" > "$BACKUP/unit.sha256"
  sha256sum "$DROPIN" > "$BACKUP/dropin.sha256"
  mkdir -p "$STATE_DIR"
  printf '%s\n' "$BACKUP" > "$STATE"
  chmod 0600 "$STATE"

  nft -c -f "$RULES"
  systemctl daemon-reload
  systemctl enable neko-us3-edge-acl-ipv4.service >/dev/null
  systemctl start neko-us3-edge-acl-ipv4.service
  nft list table "$FAMILY" "$TABLE" >/dev/null
  [ "$(service_state sing-box.service)" = "$(cat "$BACKUP/singbox.service.before")" ]
  [ "$(service_state singbox-adapter.service)" = "$(cat "$BACKUP/adapter.service.before")" ]
  sha256sum -c "$BACKUP/singbox-config.sha256.before" >/dev/null
  singbox_requires=$(systemctl show -p Requires --value sing-box.service) || { echo 'cannot inspect sing-box Requires; refusing success' >&2; exit 1; }
  if ! grep -Fxq 'neko-us3-edge-acl-ipv4.service' <<<"$(tr ' ' '\n' <<<"$singbox_requires")"; then
    echo 'sing-box does not require the US3 ACL unit; refusing success' >&2
    exit 1
  fi

  trap - EXIT
  printf 'US3_EDGE_ACL_IPV4_APPLIED\n'
  printf 'table=%s %s\n' "$FAMILY" "$TABLE"
  printf 'trusted_relay_ipv4=%s\n' "$TRUSTED_RELAY_IPV4S"
  printf 'tcp_ports=%s\n' "$PROXY_TCP_PORTS"
  printf 'udp_ports=%s\n' "$PROXY_UDP_PORTS"
  printf 'service=%s\n' "$(service_state neko-us3-edge-acl-ipv4.service)"
  printf 'singbox_service=%s\n' "$(service_state sing-box.service)"
  printf 'adapter_service=%s\n' "$(service_state singbox-adapter.service)"
  printf 'backup=%s\n' "$BACKUP"
  trap - EXIT
}

rollback() {
  if [ -r "$STATE" ]; then
    read -r BACKUP < "$STATE"
  fi
  [ -d "$BACKUP" ] || { echo "backup not found: $BACKUP" >&2; exit 1; }
  [ -f "$BACKUP/rules.sha256" ] && sha256sum -c "$BACKUP/rules.sha256" >/dev/null
  [ -f "$BACKUP/unit.sha256" ] && sha256sum -c "$BACKUP/unit.sha256" >/dev/null
  if [ -e "$DROPIN" ] && [ -f "$BACKUP/dropin.sha256" ]; then
    sha256sum -c "$BACKUP/dropin.sha256" >/dev/null || { echo 'drop-in changed; refusing rollback' >&2; exit 1; }
  fi
  remove_own
  if [ "$(cat "$BACKUP/singbox.service.before")" = active ]; then
    systemctl start sing-box.service >/dev/null 2>&1 || { echo 'sing-box restart failed during ACL rollback' >&2; exit 1; }
  fi
  [ "$(service_state sing-box.service)" = "$(cat "$BACKUP/singbox.service.before")" ] || {
    echo 'sing-box service state was not restored during ACL rollback' >&2
    exit 1
  }
  printf 'US3_EDGE_ACL_IPV4_ROLLBACK_OK\n'
  printf 'backup=%s\n' "$BACKUP"
}

status() {
  nft list table "$FAMILY" "$TABLE"
  systemctl is-active neko-us3-edge-acl-ipv4.service
}

case "${1:-}" in
  apply) apply ;;
  rollback) rollback ;;
  status) status ;;
  *) echo "usage: $0 apply|rollback|status" >&2; exit 2 ;;
esac
