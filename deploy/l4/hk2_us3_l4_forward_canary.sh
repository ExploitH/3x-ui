#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

FRONT_IP=141.11.148.116
BACKEND_IP=23.94.168.233
IFACE=eth0
TCP_VLESS_FRONT_PORT=42881
TCP_VLESS_BACKEND_PORT=8881
UDP_HY2_FRONT_PORT=42882
UDP_HY2_BACKEND_PORT=8882
UDP_TUIC_FRONT_PORT=42883
UDP_TUIC_BACKEND_PORT=8883
TCP_SS_FRONT_PORT=42885
UDP_SS_FRONT_PORT=42885
TCP_SS_BACKEND_PORT=8885
UDP_SS_BACKEND_PORT=8885
TCP_TROJAN_FRONT_PORT=42886
TCP_TROJAN_BACKEND_PORT=8886
TCP_H2_FRONT_PORT=42889
TCP_H2_BACKEND_PORT=8889
TCP_GRPC_FRONT_PORT=42890
TCP_GRPC_BACKEND_PORT=8890
TCP_ANYTLS_FRONT_PORT=42891
TCP_ANYTLS_BACKEND_PORT=8891
HELPER=/usr/local/sbin/neko-hk2-us3-edge-forward
CONF=/etc/neko-hk2-us3-edge-forward.conf
UNIT=/etc/systemd/system/neko-us3-edge-forward.service
BACKUP=/root/neko-vpn-backup-$(date -u +%Y%m%dT%H%M%SZ)-hk2-us3-edge-forward
DNAT_CHAIN=NEKO_US3EDGE_DNAT
SNAT_CHAIN=NEKO_US3EDGE_SNAT
FWD_CHAIN=NEKO_US3EDGE_FWD
CONFIG=/etc/sing-box/config.json
FRONT_PORT_REGEX='42881|42882|42883|42885|42886|42889|42890|42891'

[ "$(id -u)" -eq 0 ] || { echo 'must run as root' >&2; exit 1; }
for c in iptables iptables-save systemctl install cp mv rm mkdir sha256sum sysctl hostname sed grep awk ss ip tr; do
  command -v "$c" >/dev/null || { echo "missing command: $c" >&2; exit 1; }
done
[ "$(sysctl -n net.ipv4.ip_forward)" = 1 ] || {
  echo 'net.ipv4.ip_forward is not already enabled; refusing to change sysctl in canary' >&2
  exit 1
}
local_addresses=$(ip -4 addr show dev "$IFACE") || {
  echo "unable to inspect addresses on $IFACE; refusing to continue" >&2
  exit 1
}
if ! grep -Eq "[[:space:]]inet[[:space:]]+$FRONT_IP/" <<<"$local_addresses"; then
  echo "FRONT_IP $FRONT_IP is not assigned to $IFACE; refusing to continue" >&2
  exit 1
fi
listeners=$(ss -H -lntup) || {
  echo 'unable to inspect listeners; refusing to continue' >&2
  exit 1
}
if grep -Eq ":($FRONT_PORT_REGEX)([^0-9]|$)" <<<"$listeners"; then
  echo 'one or more US3 edge frontend ports are already listening; refusing to continue' >&2
  exit 1
fi
iptables_preflight=$(iptables-save) || {
  echo 'unable to inspect iptables; refusing to continue' >&2
  exit 1
}
if grep -Eq -- "(^|[[:space:]])--dport ($FRONT_PORT_REGEX)([[:space:]]|$)" <<<"$iptables_preflight"; then
  echo 'one or more US3 edge frontend ports already exist in iptables; refusing to continue' >&2
  exit 1
fi
if [ ! -r "$CONFIG" ] || [ ! -s "$CONFIG" ]; then
  echo "required sing-box config is missing or empty: $CONFIG" >&2
  exit 1
fi
master_service_before=$(systemctl is-active sing-box.service 2>/dev/null || true)
cdt_service_before=$(systemctl is-active neko-cdt-forward.service 2>/dev/null || true)
relay_service_before=$(systemctl is-active neko-us-relay.service 2>/dev/null || true)
jp2_edge_service_before=$(systemctl is-active neko-jp2-edge-forward.service 2>/dev/null || true)
jp3_edge_service_before=$(systemctl is-active neko-jp3-edge-forward.service 2>/dev/null || true)
us2_edge_service_before=$(systemctl is-active neko-us2-edge-forward.service 2>/dev/null || true)
[ "$master_service_before" = active ] || { echo 'sing-box.service is not active; refusing canary' >&2; exit 1; }
[ "$cdt_service_before" = active ] || { echo 'neko-cdt-forward.service is not active; refusing canary' >&2; exit 1; }
[ "$jp2_edge_service_before" = active ] || { echo 'neko-jp2-edge-forward.service is not active; refusing canary' >&2; exit 1; }
[ "$jp3_edge_service_before" = active ] || { echo 'neko-jp3-edge-forward.service is not active; refusing canary' >&2; exit 1; }
[ "$us2_edge_service_before" = active ] || { echo 'neko-us2-edge-forward.service is not active; refusing canary' >&2; exit 1; }
edge_enabled_before=$(systemctl is-enabled neko-us3-edge-forward.service 2>/dev/null || true)
edge_active_before=$(systemctl is-active neko-us3-edge-forward.service 2>/dev/null || true)
case "$edge_enabled_before:$edge_active_before" in
  enabled:*|masked:*|static:*|indirect:*|generated:*|transient:*|*:active)
    echo 'existing US3 edge unit is enabled or active; refusing to overwrite' >&2
    exit 1
    ;;
esac
for path in "$HELPER" "$CONF" "$UNIT"; do
  if [ -e "$path" ] || [ -L "$path" ]; then
    echo "existing US3 edge path found: $path; refusing to overwrite" >&2
    exit 1
  fi
done
edge_load_state=$(systemctl show -p LoadState --value neko-us3-edge-forward.service 2>/dev/null || true)
case "$edge_load_state" in
  ''|not-found) ;;
  *) echo "existing US3 edge unit is loaded ($edge_load_state); refusing to overwrite" >&2; exit 1 ;;
esac
if iptables -w 5 -t nat -nL "$DNAT_CHAIN" >/dev/null 2>&1 \
  || iptables -w 5 -t nat -nL "$SNAT_CHAIN" >/dev/null 2>&1 \
  || iptables -w 5 -t filter -nL "$FWD_CHAIN" >/dev/null 2>&1; then
  echo 'existing US3 edge iptables chain found; refusing to overwrite' >&2
  exit 1
fi
mkdir -p "$BACKUP"
for path in "$HELPER" "$CONF" "$UNIT"; do
  if [ -e "$path" ]; then cp -a "$path" "$BACKUP/$(basename "$path").before"; fi
done
iptables-save > "$BACKUP/iptables.before"
[ -s "$BACKUP/iptables.before" ] || { echo 'iptables snapshot is empty; refusing to continue' >&2; exit 1; }
config_hash_before=$(sha256sum "$CONFIG" | awk '{print $1}')
[ -n "$config_hash_before" ] || { echo 'sing-box config hash is empty' >&2; exit 1; }
cdt_lines_before=$(sed -n -E '/NEKO_CDT_|47\.243\.126\.59/ s/\[[0-9]+:[0-9]+\]//gp' "$BACKUP/iptables.before")
[ -n "$cdt_lines_before" ] || { echo 'CDT signature source is empty; refusing to continue' >&2; exit 1; }
cdt_signature_before=$(printf '%s\n' "$cdt_lines_before" | sha256sum | awk '{print $1}')
[ -n "$cdt_signature_before" ] || { echo 'CDT signature is empty' >&2; exit 1; }
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

verify_rules_absent() {
  ! iptables -w 5 -t nat -nL "$DNAT_CHAIN" >/dev/null 2>&1 || return 1
  ! iptables -w 5 -t nat -nL "$SNAT_CHAIN" >/dev/null 2>&1 || return 1
  ! iptables -w 5 -t filter -nL "$FWD_CHAIN" >/dev/null 2>&1 || return 1
  return 0
}

restore_service_state() {
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$cdt_service_before" = active ]; then systemctl start neko-cdt-forward.service >/dev/null 2>&1 || true; fi
  if [ "$relay_service_before" = active ]; then systemctl start neko-us-relay.service >/dev/null 2>&1 || true; fi
  if [ "$master_service_before" = active ]; then systemctl start sing-box.service >/dev/null 2>&1 || true; fi
}

rollback() {
  set +e
  systemctl stop neko-us3-edge-forward.service >/dev/null 2>&1 || true
  case "$edge_enabled_before" in
    enabled) systemctl enable neko-us3-edge-forward.service >/dev/null 2>&1 || true ;;
    *) systemctl disable neko-us3-edge-forward.service >/dev/null 2>&1 || true ;;
  esac
  cleanup_rules
  if ! verify_rules_absent; then
    echo 'US3 edge rollback incomplete: iptables chains remain' >&2
    return 1
  fi
  rm -f "$HELPER" "$CONF" "$UNIT"
  if [ -e "$HELPER" ] || [ -e "$CONF" ] || [ -e "$UNIT" ] || [ -L "$HELPER" ] || [ -L "$CONF" ] || [ -L "$UNIT" ]; then
    echo 'US3 edge rollback incomplete: generated paths remain' >&2
    return 1
  fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  restore_service_state
  echo "ROLLBACK_APPLIED backup=$BACKUP"
}
trap 'rc=$?; if [ "$rc" -ne 0 ]; then if ! rollback; then rc=70; fi; fi; exit "$rc"' EXIT

cat > "$BACKUP/neko-hk2-us3-edge-forward.generated" <<'EOF'
#!/bin/sh
set -eu

CONF=/etc/neko-hk2-us3-edge-forward.conf
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
  add_pair tcp "$TCP_VLESS_FRONT_PORT" "$TCP_VLESS_BACKEND_PORT"
  add_pair udp "$UDP_HY2_FRONT_PORT" "$UDP_HY2_BACKEND_PORT"
  add_pair udp "$UDP_TUIC_FRONT_PORT" "$UDP_TUIC_BACKEND_PORT"
  add_pair tcp "$TCP_SS_FRONT_PORT" "$TCP_SS_BACKEND_PORT"
  add_pair udp "$UDP_SS_FRONT_PORT" "$UDP_SS_BACKEND_PORT"
  add_pair tcp "$TCP_TROJAN_FRONT_PORT" "$TCP_TROJAN_BACKEND_PORT"
  add_pair tcp "$TCP_H2_FRONT_PORT" "$TCP_H2_BACKEND_PORT"
  add_pair tcp "$TCP_GRPC_FRONT_PORT" "$TCP_GRPC_BACKEND_PORT"
  add_pair tcp "$TCP_ANYTLS_FRONT_PORT" "$TCP_ANYTLS_BACKEND_PORT"
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
  printf 'front=%s backend=%s ports=42881/tcp,42882/udp,42883/udp,42885/tcp+udp,42886/tcp,42889/tcp,42890/tcp,42891/tcp ip_forward=1\\n' "$FRONT_IP" "$BACKEND_IP"
}

case "${1:-}" in
  apply) apply_rules ;;
  remove) remove_rules ;;
  status) status_rules ;;
  *) echo "usage: $0 apply|remove|status" >&2; exit 2 ;;
esac
EOF
chmod 0755 "$BACKUP/neko-hk2-us3-edge-forward.generated"
cat > "$CONF" <<EOF
FRONT_IP=$FRONT_IP
BACKEND_IP=$BACKEND_IP
IFACE=$IFACE
TCP_VLESS_FRONT_PORT=$TCP_VLESS_FRONT_PORT
TCP_VLESS_BACKEND_PORT=$TCP_VLESS_BACKEND_PORT
UDP_HY2_FRONT_PORT=$UDP_HY2_FRONT_PORT
UDP_HY2_BACKEND_PORT=$UDP_HY2_BACKEND_PORT
UDP_TUIC_FRONT_PORT=$UDP_TUIC_FRONT_PORT
UDP_TUIC_BACKEND_PORT=$UDP_TUIC_BACKEND_PORT
TCP_SS_FRONT_PORT=$TCP_SS_FRONT_PORT
UDP_SS_FRONT_PORT=$UDP_SS_FRONT_PORT
TCP_SS_BACKEND_PORT=$TCP_SS_BACKEND_PORT
UDP_SS_BACKEND_PORT=$UDP_SS_BACKEND_PORT
TCP_TROJAN_FRONT_PORT=$TCP_TROJAN_FRONT_PORT
TCP_TROJAN_BACKEND_PORT=$TCP_TROJAN_BACKEND_PORT
TCP_H2_FRONT_PORT=$TCP_H2_FRONT_PORT
TCP_H2_BACKEND_PORT=$TCP_H2_BACKEND_PORT
TCP_GRPC_FRONT_PORT=$TCP_GRPC_FRONT_PORT
TCP_GRPC_BACKEND_PORT=$TCP_GRPC_BACKEND_PORT
TCP_ANYTLS_FRONT_PORT=$TCP_ANYTLS_FRONT_PORT
TCP_ANYTLS_BACKEND_PORT=$TCP_ANYTLS_BACKEND_PORT
DNAT_CHAIN=$DNAT_CHAIN
SNAT_CHAIN=$SNAT_CHAIN
FWD_CHAIN=$FWD_CHAIN
EOF
chmod 0600 "$CONF"
install -m 0755 "$BACKUP/neko-hk2-us3-edge-forward.generated" "$HELPER"
cat > "$UNIT" <<'EOF'
[Unit]
Description=Neko HK2 to US3 dedicated L4 edge forwarding canary
Wants=network-online.target
After=network-online.target netfilter-persistent.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/neko-hk2-us3-edge-forward apply
ExecReload=/usr/local/sbin/neko-hk2-us3-edge-forward apply
ExecStop=/usr/local/sbin/neko-hk2-us3-edge-forward remove

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$UNIT"
systemctl daemon-reload
systemctl enable neko-us3-edge-forward.service >/dev/null
systemctl start neko-us3-edge-forward.service
"$HELPER" status
[ "$(systemctl is-active sing-box.service)" = "$master_service_before" ]
[ "$(systemctl is-active neko-cdt-forward.service)" = "$cdt_service_before" ]
[ "$(systemctl is-active neko-us-relay.service)" = "$relay_service_before" ]
[ "$(systemctl is-active neko-jp2-edge-forward.service)" = "$jp2_edge_service_before" ]
[ "$(systemctl is-active neko-jp3-edge-forward.service)" = "$jp3_edge_service_before" ]
[ "$(systemctl is-active neko-us2-edge-forward.service)" = "$us2_edge_service_before" ]
iptables-save > "$BACKUP/iptables.after"
[ -s "$BACKUP/iptables.after" ] || { echo 'post-apply iptables snapshot is empty' >&2; exit 1; }
config_hash_after=$(sha256sum "$CONFIG" | awk '{print $1}')
cdt_signature_after=$(sed -n -E '/NEKO_CDT_|47\.243\.126\.59/ s/\[[0-9]+:[0-9]+\]//gp' "$BACKUP/iptables.after" | sha256sum | awk '{print $1}')
[ -n "$(sed -n -E '/NEKO_CDT_|47\.243\.126\.59/ s/\[[0-9]+:[0-9]+\]//gp' "$BACKUP/iptables.after")" ] || { echo 'post-apply CDT signature source is empty' >&2; exit 1; }
[ -n "$config_hash_after" ] || { echo 'post-apply sing-box config hash is empty' >&2; exit 1; }
[ -n "$cdt_signature_after" ] || { echo 'post-apply CDT signature is empty' >&2; exit 1; }
[ "$config_hash_before" = "$config_hash_after" ]
[ "$cdt_signature_before" = "$cdt_signature_after" ]

trap - EXIT
rm -f "$BACKUP/neko-hk2-us3-edge-forward.generated"
printf 'HK2_US3_L4_FORWARD_CANARY_OK\n'
printf 'backup=%s\n' "$BACKUP"
printf 'service=%s\n' "$(systemctl is-active neko-us3-edge-forward.service)"
printf 'config_hash=%s\n' "$config_hash_after"
printf 'cdt_signature_unchanged=true\n'
printf 'existing_singbox=%s\n' "$(systemctl is-active sing-box.service)"
printf 'existing_cdt_forward=%s\n' "$(systemctl is-active neko-cdt-forward.service)"
printf 'existing_us_relay=%s\n' "$(systemctl is-active neko-us-relay.service)"
