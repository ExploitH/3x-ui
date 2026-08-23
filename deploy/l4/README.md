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
