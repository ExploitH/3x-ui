#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

TABLE=neko_jp2_edge_acl_ipv4
FAMILY=inet
RULES=/etc/nftables.d/neko-jp2-edge-acl-ipv4.nft
UNIT=/etc/systemd/system/neko-jp2-edge-acl-ipv4.service
STATE_DIR=/var/lib/neko-jp2-edge-acl-ipv4
STATE=$STATE_DIR/last-backup
FRONTEND_IP=141.11.148.116
PROXY_TCP_PORT=8881
PROXY_UDP_PORTS='8882,8883'
BACKUP=/root/neko-vpn-backup-$(date -u +%Y%m%dT%H%M%SZ)-jp2-edge-acl-ipv4

require() { command -v "$1" >/dev/null || { echo "missing command: $1" >&2; exit 1; }; }
for command in nft systemctl install mkdir rm cp sha256sum sed awk; do require "$command"; done
[ "$(id -u)" -eq 0 ] || { echo 'must run as root' >&2; exit 1; }

service_state() { systemctl is-active "$1" 2>/dev/null || true; }

write_rules() {
  cat > "$RULES" <<EOF
# JP2 IPv4 source ACL: only the dedicated HK2 L4 edge may reach proxy ports.
table $FAMILY $TABLE {
    set trusted_hk2_ipv4 {
        type ipv4_addr
        elements = { $FRONTEND_IP }
    }

    chain input {
        type filter hook input priority -50; policy accept;

        ct state established,related accept
        iifname "lo" accept

        ip saddr @trusted_hk2_ipv4 tcp dport $PROXY_TCP_PORT accept
        ip saddr @trusted_hk2_ipv4 udp dport { $PROXY_UDP_PORTS } accept

        ip protocol tcp tcp dport $PROXY_TCP_PORT drop
        ip protocol udp udp dport { $PROXY_UDP_PORTS } drop
    }
}
EOF
}

write_unit() {
  cat > "$UNIT" <<EOF
[Unit]
Description=Neko JP2 IPv4 source ACL for dedicated L4 edge
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

remove_own() {
  set +e
  systemctl stop neko-jp2-edge-acl-ipv4.service >/dev/null 2>&1 || true
  nft delete table "$FAMILY" "$TABLE" >/dev/null 2>&1 || true
  systemctl disable neko-jp2-edge-acl-ipv4.service >/dev/null 2>&1 || true
  rm -f "$RULES" "$UNIT"
  systemctl daemon-reload >/dev/null 2>&1 || true
}

apply() {
  [ ! -e "$RULES" ] || { echo "rules file exists: $RULES" >&2; exit 1; }
  [ ! -e "$UNIT" ] || { echo "unit exists: $UNIT" >&2; exit 1; }
  if nft list table "$FAMILY" "$TABLE" >/dev/null 2>&1; then
    echo "nft table exists: $FAMILY $TABLE" >&2
    exit 1
  fi

  mkdir -p "$(dirname "$RULES")" "$BACKUP"
  printf '%s\n' "$(service_state sing-box.service)" > "$BACKUP/singbox.service.before"
  printf '%s\n' "$(service_state singbox-adapter.service)" > "$BACKUP/adapter.service.before"
  sha256sum /etc/sing-box/config.json > "$BACKUP/singbox-config.sha256.before"
  nft list ruleset > "$BACKUP/nft-ruleset.before"
  write_rules
  write_unit
  chmod 0644 "$RULES" "$UNIT"
  sha256sum "$RULES" > "$BACKUP/rules.sha256"
  sha256sum "$UNIT" > "$BACKUP/unit.sha256"
  mkdir -p "$STATE_DIR"
  printf '%s\n' "$BACKUP" > "$STATE"
  chmod 0600 "$STATE"

  trap 'rc=$?; if [ "$rc" -ne 0 ]; then rollback; fi; exit "$rc"' EXIT

  nft -c -f "$RULES"
  systemctl daemon-reload
  systemctl enable neko-jp2-edge-acl-ipv4.service >/dev/null
  systemctl start neko-jp2-edge-acl-ipv4.service
  nft list table "$FAMILY" "$TABLE" >/dev/null
  [ "$(service_state sing-box.service)" = "$(cat "$BACKUP/singbox.service.before")" ]
  [ "$(service_state singbox-adapter.service)" = "$(cat "$BACKUP/adapter.service.before")" ]
  sha256sum -c "$BACKUP/singbox-config.sha256.before" >/dev/null

  trap - EXIT
  printf 'JP2_EDGE_ACL_IPV4_APPLIED\n'
  printf 'table=%s %s\n' "$FAMILY" "$TABLE"
  printf 'trusted_hk2=%s\n' "$FRONTEND_IP"
  printf 'tcp_port=%s\n' "$PROXY_TCP_PORT"
  printf 'udp_ports=%s\n' "$PROXY_UDP_PORTS"
  printf 'service=%s\n' "$(service_state neko-jp2-edge-acl-ipv4.service)"
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
  remove_own
  printf 'JP2_EDGE_ACL_IPV4_ROLLBACK_OK\n'
  printf 'backup=%s\n' "$BACKUP"
}

status() {
  nft list table "$FAMILY" "$TABLE"
  systemctl is-active neko-jp2-edge-acl-ipv4.service
}

case "${1:-}" in
  apply) apply ;;
  rollback) rollback ;;
  status) status ;;
  *) echo "usage: $0 apply|rollback|status" >&2; exit 2 ;;
esac
