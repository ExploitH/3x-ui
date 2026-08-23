#!/usr/bin/env python3
"""Switch only JP2 direct IPv4 Subscription rows to the HK2 L4 edge."""
from __future__ import annotations

import importlib.util
import pathlib

BASE = pathlib.Path(__file__).with_name('cloudflare_hk3_subscription_domain_canary.py')
spec = importlib.util.spec_from_file_location('jp2_subscription_canary', BASE)
if spec is None or spec.loader is None:
    raise SystemExit('cannot load base canary helper')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

setattr(module, 'HOSTS', {'156.231.115.204'})
setattr(module, 'TARGET_MARKER', 'JP2')
setattr(module, 'EXCLUDE_LABEL_MARKERS', ('JP2*',))
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-jp2.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 39981, 8882: 39982, 8883: 39983})
setattr(module, 'CANARY_NAME', 'jp2-subscription-edge-ipv4')
setattr(module, 'CANARY_STATUS', 'JP2_SUBSCRIPTION_EDGE_IPV4_CANARY')

if __name__ == '__main__':
    raise SystemExit(module.main())
