from __future__ import annotations

import importlib.util
import json
import pathlib
import unittest
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / 'cloudflare_hk3_subscription_domain_canary.py'
spec = importlib.util.spec_from_file_location('jp2_subscription_base', SCRIPT)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
setattr(module, 'HOSTS', {'156.231.115.204'})
setattr(module, 'TARGET_MARKER', 'JP2')
setattr(module, 'EXCLUDE_LABEL_MARKERS', ('JP2*',))
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-jp2.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 39981, 8882: 39982, 8883: 39983})


class JP2SelectorTest(unittest.TestCase):
    def test_jp2_ipv4_is_mapped(self) -> None:
        line = 'vless://opaque@156.231.115.204:8881#JP2 IPv4 VLESS'
        candidate, changed = module.replace_uri_endpoint(line)
        self.assertTrue(changed)
        parsed = urllib.parse.urlsplit(candidate)
        self.assertEqual((parsed.hostname, parsed.port), ('edge-jp2.427357.xyz', 39981))

    def test_jp2_star_is_unchanged(self) -> None:
        line = 'vless://opaque@152.175.34.230:8881#JP2* IPv4 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_jp1_is_unchanged(self) -> None:
        line = 'vless://opaque@154.83.91.144:8881#JP1 IPv4 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_jp2_ipv6_is_unchanged(self) -> None:
        line = 'vless://opaque@[2602:fa4f:b00:daa:b169:3d9:1f7e:cb92]:8881#JP2 IPv6 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_clash_excludes_jp2_star(self) -> None:
        raw = json.dumps({'proxies': [
            {'name': 'JP2 IPv4 VLESS', 'server': '156.231.115.204', 'port': 8881},
            {'name': 'JP2* IPv4 VLESS', 'server': '152.175.34.230', 'port': 8881},
        ]}).encode()
        candidate, changed = module.clash_candidate(raw)
        self.assertEqual(changed, 1)
        catalog = json.loads(candidate)
        self.assertEqual(catalog['proxies'][0]['server'], 'edge-jp2.427357.xyz')
        self.assertEqual(catalog['proxies'][0]['port'], 39981)
        self.assertEqual(catalog['proxies'][1]['server'], '152.175.34.230')
        self.assertEqual(catalog['proxies'][1]['port'], 8881)


if __name__ == '__main__':
    unittest.main()
