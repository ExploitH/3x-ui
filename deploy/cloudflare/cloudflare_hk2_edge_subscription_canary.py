#!/usr/bin/env python3
"""Switch only HK2 IPv4 Subscription entries to the HK1 L4 edge."""
from __future__ import annotations

import importlib.util
import pathlib

BASE = pathlib.Path(__file__).with_name('cloudflare_hk3_subscription_domain_canary.py')
spec = importlib.util.spec_from_file_location('hk2_subscription_canary', BASE)
if spec is None or spec.loader is None:
    raise SystemExit('cannot load base canary helper')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

# Reuse the same fail-closed byte/object validation and KV rollback. Only HK2
# IPv4 rows are selected; HK2 IPv6 rows stay unchanged until a NAT6 edge exists.
setattr(module, 'HOSTS', {'141.11.148.116'})
setattr(module, 'TARGET_MARKER', 'HK2')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-hk2.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 38981, 8882: 38982, 8883: 38983})
setattr(module, 'CANARY_NAME', 'hk2-subscription-edge-ipv4')
setattr(module, 'CANARY_STATUS', 'HK2_SUBSCRIPTION_EDGE_IPV4_CANARY')

if __name__ == '__main__':
    raise SystemExit(module.main())
