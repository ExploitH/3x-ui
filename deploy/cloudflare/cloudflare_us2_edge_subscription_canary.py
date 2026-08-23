#!/usr/bin/env python3
"""Switch only US2 direct IPv4 Subscription rows to the HK2 L4 edge."""
from __future__ import annotations

import importlib.util
import pathlib

BASE = pathlib.Path(__file__).with_name('cloudflare_hk3_subscription_domain_canary.py')
spec = importlib.util.spec_from_file_location('jp3_subscription_canary', BASE)
if spec is None or spec.loader is None:
    raise SystemExit('cannot load base canary helper')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

setattr(module, 'HOSTS', {'104.223.55.204'})
setattr(module, 'TARGET_MARKER', 'US2')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-us2.427357.xyz')
setattr(module, 'PORT_MAP', {
    8881: 41881,
    8882: 41882,
    8883: 41883,
    8885: 41885,
    8886: 41886,
    8889: 41889,
    8890: 41890,
    8891: 41891,
})
setattr(module, 'CANARY_NAME', 'us2-subscription-edge-ipv4')
setattr(module, 'CANARY_STATUS', 'US2_SUBSCRIPTION_EDGE_IPV4_CANARY')

if __name__ == '__main__':
    raise SystemExit(module.main())
