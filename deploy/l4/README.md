## HK3 source ACL

After the HK2 edge canary passed real VLESS Reality, Hysteria2 and TUIC client
probes, the source ACL can be installed on HK3:

```bash
bash deploy/l4/hk3_edge_source_acl.sh apply
bash deploy/l4/hk3_edge_source_acl.sh status
bash deploy/l4/hk3_edge_source_acl.sh rollback
```

The ACL is an independent `inet neko_hk3_edge_acl` table:

```text
allow loopback
allow source 141.11.148.116 to TCP/8881 and UDP/8882,8883
drop all other TCP/8881 and UDP/8882,8883
```

The default input policy remains untouched. SSH/22, Master/adapter loopback,
DNS/NTP, outbound Node→Node traffic, HK→JP AI traffic and every other port are
outside the table. JP1* relay nodes are not involved.

The script refuses an existing same-name table/files, runs `nft -c` before
loading, stores the original ruleset and service/config invariants under a
root-only backup, and records the backup path under
`/var/lib/neko-hk3-edge-acl/last-backup`. Rollback verifies the current rules
and unit hashes before removing only this table/unit.

Do not install the ACL before the HK2 edge has passed all three protocol probes.

## HK2 IPv4-only canary

The next canary reuses the same isolated pattern for HK2, with HK1 as the
trusted IPv4 edge:

```text
edge-hk2.427357.xyz → 91.229.132.66
HK1:38981/tcp → HK2:8881/tcp
HK1:38982/udp → HK2:8882/udp
HK1:38983/udp → HK2:8883/udp
```

The HK2 Subscription wrapper changes only HK2 IPv4 rows. HK2 IPv6 rows remain
on their existing endpoint until a separate NAT6/L4 design is reviewed. The
matching `hk2_edge_source_acl_ipv4.sh` uses explicit `ip protocol tcp/udp`
rules, so the IPv6 path is not accidentally filtered by the `inet` table.

The HK1 helper uses `NEKO_HK2EDGE_*` chains and never invokes or flushes the
existing `NEKO_CDT_*` or US-relay chains.

## JP2 IPv4-only canary

JP2 direct IPv4 uses HK2 as the trusted L4 edge. This canary is separate from
the HK2 and HK3 units/chains:

```text
edge-jp2.427357.xyz → 141.11.148.116
HK2:39981/tcp → JP2:8881/tcp
HK2:39982/udp → JP2:8882/udp
HK2:39983/udp → JP2:8883/udp
```

`cloudflare_jp2_edge_subscription_canary.py` changes only JP2 direct IPv4
rows. It explicitly excludes `JP2*`, JP1/JP1* labels, IPv6 rows, and the JP2
CPA mixed `20170` surface. `jp2_edge_source_acl_ipv4.sh` uses a separate
`inet neko_jp2_edge_acl_ipv4` table and explicit IPv4 protocol matches, so the
JP2 IPv6 path is not claimed or accidentally filtered.

The L4 helper refuses pre-existing generated paths, unit states, or chains,
uses `NEKO_JP2EDGE_*`, and verifies targeted rollback cleanup without restoring
the whole iptables snapshot over unrelated concurrent rules.

## JP3 IPv4-only canary

JP3 has a separate AI SNI relay on TCP/443. The direct-node canary must not
touch that path. Only the IPv4 direct sing-box ports are fronted through HK2:

```text
edge-jp3.427357.xyz → 141.11.148.116
HK2:40881/tcp → JP3:8881/tcp
HK2:40882/udp → JP3:8882/udp
HK2:40883/udp → JP3:8883/udp
HK2:40885/tcp+udp → JP3:8885/tcp+udp
HK2:40886/tcp → JP3:8886/tcp
HK2:40889/tcp → JP3:8889/tcp
HK2:40890/tcp → JP3:8890/tcp
HK2:40891/tcp → JP3:8891/tcp
```

The JP3 ACL explicitly protects TCP `8881,8885,8886,8889,8890,8891` and UDP
`8882,8883,8885`, while leaving TCP/443 and the AI SNI service outside its
table. JP3 IPv6, JP1* and all relay entries are outside this canary.

## US2 IPv4-only canary

US2 is an exit node used by existing HK1/JP1*/JP2* relay paths. Its canary
therefore uses a fresh HK2 port range and an allowlist containing the existing
relay source addresses; it must not use the JP3 `408xx` range or allowlist only
HK2:

```text
edge-us2.427357.xyz → 141.11.148.116
HK2:41881/tcp       → US2:8881/tcp
HK2:41882/udp       → US2:8882/udp
HK2:41883/udp       → US2:8883/udp
HK2:41885/tcp+udp   → US2:8885/tcp+udp
HK2:41886/tcp       → US2:8886/tcp
HK2:41889/tcp       → US2:8889/tcp
HK2:41890/tcp       → US2:8890/tcp
HK2:41891/tcp       → US2:8891/tcp
```

The trusted IPv4 set is deliberately limited to HK2 `141.11.148.116`, HK1
`91.229.132.66`, JP1* `70.36.96.197`, JP2* `152.175.34.230`, and the existing
CDT backend `47.243.126.59`. Direct client IPv4 traffic to US2 proxy ports is
dropped; SSH, DNS/NTP, Master/Node traffic, and other ports remain outside the
table. JP1* is an allowlist source only and is not modified.

The L4 helper refuses local-address, listener, iptables-port, config, and
service-state preflight failures. It records non-empty CDT/config invariants,
uses isolated `NEKO_US2EDGE_*` chains, and rolls back only its own rules/unit.
The ACL helper additionally installs a `sing-box.service` `Requires=` drop-in so
sing-box cannot start without the US2 ACL after this canary is activated. This
drop-in is created by the ACL step, not by the earlier L4 step, so the staged
deployment window remains restart-safe.

## US3 IPv4-only canary

US3 is the second direct US exit. It uses a separate HK2 port range from both
US2 (`418xx`) and JP3 (`408xx`), and the helper refuses to proceed unless the
existing US2, JP2 and JP3 edge services remain active:

```text
edge-us3.427357.xyz → 141.11.148.116
HK2:42881/tcp       → US3:8881/tcp
HK2:42882/udp       → US3:8882/udp
HK2:42883/udp       → US3:8883/udp
HK2:42885/tcp+udp   → US3:8885/tcp+udp
HK2:42886/tcp       → US3:8886/tcp
HK2:42889/tcp       → US3:8889/tcp
HK2:42890/tcp       → US3:8890/tcp
HK2:42891/tcp       → US3:8891/tcp
```

The US3 ACL mirrors the US2 trusted-relay set in its own
`inet neko_us3_edge_acl_ipv4` table, unit, state directory, and sing-box
Requires drop-in. US3 IPv6, JP1* and existing relay entries remain outside the
direct IPv4 selector; JP1* is not modified.
