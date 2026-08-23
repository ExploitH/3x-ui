#!/usr/bin/env python3
"""Switch only HK1 direct IPv4 Subscription rows to the HK2 L4 edge."""
from __future__ import annotations

import importlib.util
import pathlib

BASE = pathlib.Path(__file__).with_name('cloudflare_hk3_subscription_domain_canary.py')
spec = importlib.util.spec_from_file_location('jp3_subscription_canary', BASE)
if spec is None or spec.loader is None:
    raise SystemExit('cannot load base canary helper')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

setattr(module, 'HOSTS', {'91.229.132.66'})
setattr(module, 'TARGET_MARKER', 'HK1')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'EXCLUDE_LABEL_MARKERS', ('SHADOWSOCKS', 'SHADOWTLS'))
setattr(module, 'RELAY_LABEL_MARKERS', ('中转', 'RELAY'))
setattr(module, 'DOMAIN', 'edge-hk1.427357.xyz')
setattr(module, 'PORT_MAP', {
    8881: 43881,
    8882: 43882,
    8883: 43883,
    8886: 43886,
    8889: 43889,
    8890: 43890,
    8891: 43891,
    28882: 43892,
    28883: 43893,
    28884: 43894,
    28885: 43895,
    28886: 43896,
    28887: 43897,
})
setattr(module, 'CANARY_NAME', 'hk1-subscription-edge-ipv4')
setattr(module, 'CANARY_STATUS', 'HK1_SUBSCRIPTION_EDGE_IPV4_CANARY')

if __name__ == '__main__':
    raise SystemExit(module.main())
