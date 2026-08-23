#!/usr/bin/env python3
"""Switch only JP3 direct IPv4 Subscription rows to the HK2 L4 edge."""
from __future__ import annotations

import importlib.util
import pathlib

BASE = pathlib.Path(__file__).with_name('cloudflare_hk3_subscription_domain_canary.py')
spec = importlib.util.spec_from_file_location('jp3_subscription_canary', BASE)
if spec is None or spec.loader is None:
    raise SystemExit('cannot load base canary helper')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

setattr(module, 'HOSTS', {'156.246.90.242'})
setattr(module, 'TARGET_MARKER', 'JP3')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-jp3.427357.xyz')
setattr(module, 'PORT_MAP', {
    8881: 40881,
    8882: 40882,
    8883: 40883,
    8885: 40885,
    8886: 40886,
    8889: 40889,
    8890: 40890,
    8891: 40891,
})
setattr(module, 'CANARY_NAME', 'jp3-subscription-edge-ipv4')
setattr(module, 'CANARY_STATUS', 'JP3_SUBSCRIPTION_EDGE_IPV4_CANARY')

if __name__ == '__main__':
    raise SystemExit(module.main())
