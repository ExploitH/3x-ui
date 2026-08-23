#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

FRONT_IP=141.11.148.116
BACKEND_IP=39.109.50.213
IFACE=eth0
TCP_FRONT_PORT=38881
TCP_BACKEND_PORT=8881
UDP_HY2_FRONT_PORT=38882
UDP_HY2_BACKEND_PORT=8882
UDP_TUIC_FRONT_PORT=38883
UDP_TUIC_BACKEND_PORT=8883
HELPER=/usr/local/sbin/neko-hk3-edge-forward
CONF=/etc/neko-hk3-edge-forward.conf
UNIT=/etc/systemd/system/neko-hk3-edge-forward.service
BACKUP=/root/neko-vpn-backup-$(date -u +%Y%m%dT%H%M%SZ)-hk2-hk3-edge-forward
DNAT_CHAIN=NEKO_HK3EDGE_DNAT
SNAT_CHAIN=NEKO_HK3EDGE_SNAT
FWD_CHAIN=NEKO_HK3EDGE_FWD

[ "$(id -u)" -eq 0 ] || { echo 'must run as root' >&2; exit 1; }
for c in iptables iptables-save systemctl install cp mv rm mkdir sha256sum sysctl hostname sed; do
  command -v "$c" >/dev/null || { echo "missing command: $c" >&2; exit 1; }
done
[ "$(sysctl -n net.ipv4.ip_forward)" = 1 ] || {
  echo 'net.ipv4.ip_forward is not already enabled; refusing to change sysctl in canary' >&2
  exit 1
}

master_service_before=$(systemctl is-active sing-box.service 2>/dev/null || true)
cdt_service_before=$(systemctl is-active neko-cdt-forward.service 2>/dev/null || true)
relay_service_before=$(systemctl is-active neko-us-relay.service 2>/dev/null || true)
edge_enabled_before=$(systemctl is-enabled neko-hk3-edge-forward.service 2>/dev/null || true)
edge_active_before=$(systemctl is-active neko-hk3-edge-forward.service 2>/dev/null || true)
case "$edge_enabled_before:$edge_active_before" in
  enabled:*|*:active)
    echo 'existing HK3 edge unit is enabled or active; refusing to overwrite' >&2
    exit 1
    ;;
esac
if iptables -w 5 -t nat -nL "$DNAT_CHAIN" >/dev/null 2>&1 \
  || iptables -w 5 -t nat -nL "$SNAT_CHAIN" >/dev/null 2>&1 \
  || iptables -w 5 -t filter -nL "$FWD_CHAIN" >/dev/null 2>&1; then
  echo 'existing HK3 edge iptables chain found; refusing to overwrite' >&2
  exit 1
fi
config_hash_before=$(sha256sum /etc/sing-box/config.json 2>/dev/null | awk '{print $1}' || true)
cdt_signature_before=$(iptables-save 2>/dev/null | grep -E 'NEKO_CDT_|47\.243\.126\.59' | sed -E 's/\[[0-9]+:[0-9]+\]//g' | sha256sum | awk '{print $1}' || true)

mkdir -p "$BACKUP"
for path in "$HELPER" "$CONF" "$UNIT"; do
  if [ -e "$path" ]; then cp -a "$path" "$BACKUP/$(basename "$path").before"; fi
done
iptables-save > "$BACKUP/iptables.before"
printf '%s\n' "$config_hash_before" > "$BACKUP/singbox-config.sha256.before"
printf '%s\n' "$cdt_signature_before" > "$BACKUP/cdt-signature.before"

cleanup_rules() {
  set +e
  while iptables -w 5 -t nat -C PREROUTING -i "$IFACE" -d "$FRONT_IP" -j "$DNAT_CHAIN" 2>/dev/null; do
    iptables -w 5 -t nat -D PREROUTING -i "$IFACE" -d "$FRONT_IP" -j "$DNAT_CHAIN"
  done
  while iptables -w 5 -t nat -C POSTROUTING -o "$IFACE" -d "$BACKEND_IP" -j "$SNAT_CHAIN" 2>/dev/null; do
    iptables -w 5 -t nat -D POSTROUTING -o "$IFACE" -d "$BACKEND_IP" -j "$SNAT_CHAIN"
  done
  while iptables -w 5 -t filter -C FORWARD -i "$IFACE" -d "$BACKEND_IP" -j "$FWD_CHAIN" 2>/dev/null; do
    iptables -w 5 -t filter -D FORWARD -i "$IFACE" -d "$BACKEND_IP" -j "$FWD_CHAIN"
  done
  while iptables -w 5 -t filter -C FORWARD -i "$IFACE" -s "$BACKEND_IP" -j "$FWD_CHAIN" 2>/dev/null; do
    iptables -w 5 -t filter -D FORWARD -i "$IFACE" -s "$BACKEND_IP" -j "$FWD_CHAIN"
  done
  iptables -w 5 -t nat -F "$DNAT_CHAIN" 2>/dev/null || true
  iptables -w 5 -t nat -X "$DNAT_CHAIN" 2>/dev/null || true
  iptables -w 5 -t nat -F "$SNAT_CHAIN" 2>/dev/null || true
  iptables -w 5 -t nat -X "$SNAT_CHAIN" 2>/dev/null || true
  iptables -w 5 -t filter -F "$FWD_CHAIN" 2>/dev/null || true
  iptables -w 5 -t filter -X "$FWD_CHAIN" 2>/dev/null || true
}

restore_service_state() {
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$cdt_service_before" = active ]; then systemctl start neko-cdt-forward.service >/dev/null 2>&1 || true; fi
  if [ "$relay_service_before" = active ]; then systemctl start neko-us-relay.service >/dev/null 2>&1 || true; fi
  if [ "$master_service_before" = active ]; then systemctl start sing-box.service >/dev/null 2>&1 || true; fi
}

rollback() {
  set +e
  systemctl stop neko-hk3-edge-forward.service >/dev/null 2>&1 || true
  case "$edge_enabled_before" in
    enabled) systemctl enable neko-hk3-edge-forward.service >/dev/null 2>&1 || true ;;
    *) systemctl disable neko-hk3-edge-forward.service >/dev/null 2>&1 || true ;;
  esac
  cleanup_rules
  if [ -f "$BACKUP/neko-hk3-edge-forward.before" ]; then cp -a "$BACKUP/neko-hk3-edge-forward.before" "$HELPER"; else rm -f "$HELPER"; fi
  if [ -f "$BACKUP/neko-hk3-edge-forward.conf.before" ]; then cp -a "$BACKUP/neko-hk3-edge-forward.conf.before" "$CONF"; else rm -f "$CONF"; fi
  if [ -f "$BACKUP/neko-hk3-edge-forward.service.before" ]; then cp -a "$BACKUP/neko-hk3-edge-forward.service.before" "$UNIT"; else rm -f "$UNIT"; fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  restore_service_state
  echo "ROLLBACK_APPLIED backup=$BACKUP"
}
trap 'rc=$?; if [ "$rc" -ne 0 ]; then rollback; fi; exit "$rc"' EXIT

cat > "$BACKUP/neko-hk3-edge-forward.generated" <<'EOF'
#!/bin/sh
set -eu

CONF=/etc/neko-hk3-edge-forward.conf
. "$CONF"
ipt() { iptables -w 5 "$@"; }

remove_jump_all() {
  table=$1; chain=$2; shift 2
  while ipt -t "$table" -C "$chain" "$@" 2>/dev/null; do ipt -t "$table" -D "$chain" "$@"; done
}

remove_rules() {
  remove_jump_all nat PREROUTING -i "$IFACE" -d "$FRONT_IP" -j "$DNAT_CHAIN"
  remove_jump_all nat POSTROUTING -o "$IFACE" -d "$BACKEND_IP" -j "$SNAT_CHAIN"
  remove_jump_all filter FORWARD -i "$IFACE" -d "$BACKEND_IP" -j "$FWD_CHAIN"
  remove_jump_all filter FORWARD -i "$IFACE" -s "$BACKEND_IP" -j "$FWD_CHAIN"
  ipt -t nat -F "$DNAT_CHAIN" 2>/dev/null || true; ipt -t nat -X "$DNAT_CHAIN" 2>/dev/null || true
  ipt -t nat -F "$SNAT_CHAIN" 2>/dev/null || true; ipt -t nat -X "$SNAT_CHAIN" 2>/dev/null || true
  ipt -t filter -F "$FWD_CHAIN" 2>/dev/null || true; ipt -t filter -X "$FWD_CHAIN" 2>/dev/null || true
}

add_pair() {
  proto=$1; front_port=$2; backend_port=$3
  ipt -t nat -A "$DNAT_CHAIN" -p "$proto" --dport "$front_port" -j DNAT --to-destination "$BACKEND_IP:$backend_port"
  ipt -t nat -A "$SNAT_CHAIN" -p "$proto" -d "$BACKEND_IP" --dport "$backend_port" -j SNAT --to-source "$FRONT_IP"
  ipt -t filter -A "$FWD_CHAIN" -p "$proto" -d "$BACKEND_IP" --dport "$backend_port" -m conntrack --ctstate NEW,ESTABLISHED -j ACCEPT
}

apply_rules() {
  [ "$(sysctl -n net.ipv4.ip_forward)" = 1 ] || { echo 'ip_forward must already be 1' >&2; exit 1; }
  remove_rules
  ipt -t nat -N "$DNAT_CHAIN"; ipt -t nat -N "$SNAT_CHAIN"; ipt -t filter -N "$FWD_CHAIN"
  ipt -t filter -A "$FWD_CHAIN" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  add_pair tcp "$TCP_FRONT_PORT" "$TCP_BACKEND_PORT"
  add_pair udp "$UDP_HY2_FRONT_PORT" "$UDP_HY2_BACKEND_PORT"
  add_pair udp "$UDP_TUIC_FRONT_PORT" "$UDP_TUIC_BACKEND_PORT"
  ipt -t filter -A "$FWD_CHAIN" -j RETURN
  ipt -t nat -I PREROUTING 1 -i "$IFACE" -d "$FRONT_IP" -j "$DNAT_CHAIN"
  ipt -t nat -I POSTROUTING 1 -o "$IFACE" -d "$BACKEND_IP" -j "$SNAT_CHAIN"
  ipt -t filter -I FORWARD 1 -i "$IFACE" -d "$BACKEND_IP" -j "$FWD_CHAIN"
  ipt -t filter -I FORWARD 1 -i "$IFACE" -s "$BACKEND_IP" -j "$FWD_CHAIN"
}

status_rules() {
  [ "$(sysctl -n net.ipv4.ip_forward)" = 1 ]
  ipt -t nat -C PREROUTING -i "$IFACE" -d "$FRONT_IP" -j "$DNAT_CHAIN"
  ipt -t nat -C POSTROUTING -o "$IFACE" -d "$BACKEND_IP" -j "$SNAT_CHAIN"
  ipt -t filter -C FORWARD -i "$IFACE" -d "$BACKEND_IP" -j "$FWD_CHAIN"
  ipt -t filter -C FORWARD -i "$IFACE" -s "$BACKEND_IP" -j "$FWD_CHAIN"
  printf 'front=%s backend=%s tcp=%s->%s hy2=%s->%s tuic=%s->%s ip_forward=1\n' "$FRONT_IP" "$BACKEND_IP" "$TCP_FRONT_PORT" "$TCP_BACKEND_PORT" "$UDP_HY2_FRONT_PORT" "$UDP_HY2_BACKEND_PORT" "$UDP_TUIC_FRONT_PORT" "$UDP_TUIC_BACKEND_PORT"
}

case "${1:-}" in
  apply) apply_rules ;;
  remove) remove_rules ;;
  status) status_rules ;;
  *) echo "usage: $0 apply|remove|status" >&2; exit 2 ;;
esac
EOF
chmod 0755 "$BACKUP/neko-hk3-edge-forward.generated"
cat > "$CONF" <<EOF
FRONT_IP=$FRONT_IP
BACKEND_IP=$BACKEND_IP
IFACE=$IFACE
TCP_FRONT_PORT=$TCP_FRONT_PORT
TCP_BACKEND_PORT=$TCP_BACKEND_PORT
UDP_HY2_FRONT_PORT=$UDP_HY2_FRONT_PORT
UDP_HY2_BACKEND_PORT=$UDP_HY2_BACKEND_PORT
UDP_TUIC_FRONT_PORT=$UDP_TUIC_FRONT_PORT
UDP_TUIC_BACKEND_PORT=$UDP_TUIC_BACKEND_PORT
DNAT_CHAIN=$DNAT_CHAIN
SNAT_CHAIN=$SNAT_CHAIN
FWD_CHAIN=$FWD_CHAIN
EOF
chmod 0600 "$CONF"
install -m 0755 "$BACKUP/neko-hk3-edge-forward.generated" "$HELPER"
cat > "$UNIT" <<'EOF'
[Unit]
Description=Neko HK3 dedicated L4 edge forwarding canary
Wants=network-online.target
After=network-online.target netfilter-persistent.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/neko-hk3-edge-forward apply
ExecReload=/usr/local/sbin/neko-hk3-edge-forward apply
ExecStop=/usr/local/sbin/neko-hk3-edge-forward remove

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$UNIT"
systemctl daemon-reload
systemctl enable neko-hk3-edge-forward.service >/dev/null
systemctl start neko-hk3-edge-forward.service
"$HELPER" status
[ "$(systemctl is-active sing-box.service)" = "$master_service_before" ]
[ "$(systemctl is-active neko-cdt-forward.service)" = "$cdt_service_before" ]
[ "$(systemctl is-active neko-us-relay.service)" = "$relay_service_before" ]
config_hash_after=$(sha256sum /etc/sing-box/config.json 2>/dev/null | awk '{print $1}' || true)
cdt_signature_after=$(iptables-save 2>/dev/null | grep -E 'NEKO_CDT_|47\.243\.126\.59' | sed -E 's/\[[0-9]+:[0-9]+\]//g' | sha256sum | awk '{print $1}' || true)
[ "$config_hash_before" = "$config_hash_after" ]
[ "$cdt_signature_before" = "$cdt_signature_after" ]

trap - EXIT
rm -f "$BACKUP/neko-hk3-edge-forward.generated"
printf 'HK2_HK3_L4_FORWARD_CANARY_OK\n'
printf 'backup=%s\n' "$BACKUP"
printf 'service=%s\n' "$(systemctl is-active neko-hk3-edge-forward.service)"
printf 'config_hash=%s\n' "$config_hash_after"
printf 'cdt_signature_unchanged=true\n'
printf 'existing_singbox=%s\n' "$(systemctl is-active sing-box.service)"
printf 'existing_cdt_forward=%s\n' "$(systemctl is-active neko-cdt-forward.service)"
printf 'existing_us_relay=%s\n' "$(systemctl is-active neko-us-relay.service)"
