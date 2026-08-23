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
