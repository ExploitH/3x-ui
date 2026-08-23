#!/usr/bin/env python3
"""Switch only the already-domainized HK3 subscription host to the HK2 L4 edge."""
from __future__ import annotations

import importlib.util
import pathlib
import sys

BASE = pathlib.Path(__file__).with_name('cloudflare_hk3_subscription_domain_canary.py')
spec = importlib.util.spec_from_file_location('hk3_domain_canary', BASE)
if spec is None or spec.loader is None:
    raise SystemExit('cannot load base canary helper')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

# The base helper is deliberately reused so its byte/object invariants and
# fail-closed backup/rollback semantics remain identical.
setattr(module, 'HOSTS', {'hk3.427357.xyz'})
setattr(module, 'DOMAIN', 'edge-hk3.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 38881, 8882: 38882, 8883: 38883})
setattr(module, 'STATE', pathlib.Path('/www/projects/vpn-3xui-unification/audit/edge-hk3-subscription-canary-20260823.json'))

if __name__ == '__main__':
    raise SystemExit(module.main())
