# HK2 → HK3 L4 edge canary

This is a separate, rollback-capable canary for a future source-IP restriction
on HK3. It was not installed by the audit phase.

Proposed path:

```text
Client
  ↓ edge-hk3.427357.xyz
HK2 141.11.148.116:38881/tcp → HK3 39.109.50.213:8881/tcp
HK2 141.11.148.116:38882/udp → HK3 39.109.50.213:8882/udp
HK2 141.11.148.116:38883/udp → HK3 39.109.50.213:8883/udp
```

The helper uses independent chains:

```text
NEKO_HK3EDGE_DNAT
NEKO_HK3EDGE_SNAT
NEKO_HK3EDGE_FWD
```

It refuses to overwrite an existing helper/unit/chain, records the existing
iptables snapshot and sing-box/CDT invariants, changes no sing-box config, and
rolls back only its own resources on failure. It never calls the existing
`neko-cdt-forward` helper and therefore does not flush `NEKO_CDT_*` chains.

Before use, create a separately reviewed DNS record such as
`edge-hk3.427357.xyz` pointing to HK2, then execute the helper through the
trusted `hk-vps-116` SSH profile. Do not switch the existing
`hk3.427357.xyz` Subscription entry until the front path has been tested.

The helper does not alter HK3 firewall rules. A later HK3 source restriction
must be a separate transaction with a persistent rollback rule and explicit
allowlist for HK2, SSH, Master/health paths, DNS/NTP, Node→Node, and the
HK→JP AI path.

JP1* relay nodes are explicitly out of scope.
