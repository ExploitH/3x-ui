# HK3 Cloudflare domain canary helpers

These helpers were used for the HK3 additive domain canary on 2026-08-23.
They are intentionally scoped to `hk3.427357.xyz` and must not be generalized
without a new review.

## DNS canary

```bash
python3 deploy/cloudflare/cloudflare_hk3_dns_canary.py apply
python3 deploy/cloudflare/cloudflare_hk3_dns_canary.py rollback
```

The DNS helper creates only these DNS-only records:

```text
hk3.427357.xyz A    39.109.50.213
hk3.427357.xyz AAAA 2a0f:1cc6:b240:201::240
```

It refuses conflicting records, records created IDs in the root-only audit
state, and rollback deletes only records whose name/type/content/proxy state
still exactly matches the recorded canary.

## Subscription canary

Dry-run first:

```bash
python3 deploy/cloudflare/cloudflare_hk3_subscription_domain_canary.py dry-run
```

Apply and rollback:

```bash
python3 deploy/cloudflare/cloudflare_hk3_subscription_domain_canary.py apply
python3 deploy/cloudflare/cloudflare_hk3_subscription_domain_canary.py rollback \
  --backup /root/neko-vpn-backup-20260823T021528Z-hk3-subscription-domain
```

The base helper reads the current `STATE_KV` values and changes only HK3
endpoint hosts in the five payload formats. The direct-domain stage leaves
`sub:keys`, credentials, ports, queries, labels, protocols, and all non-HK3
entries unchanged. The HK2 edge wrapper deliberately maps the three HK3
service ports as follows:

```text
8881/tcp → edge-hk3.427357.xyz:38881/tcp
8882/udp → edge-hk3.427357.xyz:38882/udp
8883/udp → edge-hk3.427357.xyz:38883/udp
```

It writes a root-only before/candidate manifest and refuses rollback if the
live KV values have changed since the canary.

The helper does not enforce SNI/Host, hide the source IP, alter firewall rules,
modify sing-box, touch JP1* relay nodes, or alter the HK→JP AI path. Current
VLESS Reality, Hysteria2 insecure, and TUIC allow-insecure semantics remain
unchanged.
