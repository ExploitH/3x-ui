from __future__ import annotations

import importlib.util
import json
import pathlib
import unittest
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / 'cloudflare_hk3_subscription_domain_canary.py'
spec = importlib.util.spec_from_file_location('hk2_subscription_base', SCRIPT)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
setattr(module, 'HOSTS', {'141.11.148.116'})
setattr(module, 'TARGET_MARKER', 'HK2')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-hk2.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 38981, 8882: 38982, 8883: 38983})


class HK2IPv4CanaryTest(unittest.TestCase):
    def test_ipv4_is_mapped(self) -> None:
        line = 'vless://opaque@141.11.148.116:8881#HK2 IPv4 VLESS'
        candidate, changed = module.replace_uri_endpoint(line)
        self.assertTrue(changed)
        parsed = urllib.parse.urlsplit(candidate)
        self.assertEqual((parsed.hostname, parsed.port), ('edge-hk2.427357.xyz', 38981))

    def test_ipv6_label_is_unchanged(self) -> None:
        line = 'vless://opaque@[2401:b60:5:10fd:742:d8e0:4ec7:8e82]:8881#HK2 IPv6 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_other_node_is_unchanged(self) -> None:
        line = 'vless://opaque@91.229.132.66:8881#HK1 IPv4 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_clash_ipv4_only(self) -> None:
        raw = json.dumps({'proxies': [
            {'name': 'HK2 IPv4 VLESS', 'server': '141.11.148.116', 'port': 8881},
            {'name': 'HK2 IPv6 VLESS', 'server': '2401:b60:5:10fd:742:d8e0:4ec7:8e82', 'port': 8881},
        ]}).encode()
        candidate, changed = module.clash_candidate(raw)
        self.assertEqual(changed, 1)
        catalog = json.loads(candidate)
        self.assertEqual(catalog['proxies'][0]['server'], 'edge-hk2.427357.xyz')
        self.assertEqual(catalog['proxies'][0]['port'], 38981)
        self.assertEqual(catalog['proxies'][1]['server'], '2401:b60:5:10fd:742:d8e0:4ec7:8e82')
        self.assertEqual(catalog['proxies'][1]['port'], 8881)


if __name__ == '__main__':
    unittest.main()
