from __future__ import annotations

import importlib.util
import json
import pathlib
import unittest
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / 'cloudflare_hk3_subscription_domain_canary.py'
spec = importlib.util.spec_from_file_location('jp3_subscription_base', SCRIPT)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
setattr(module, 'HOSTS', {'23.94.168.233'})
setattr(module, 'TARGET_MARKER', 'US3')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-us3.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 42881, 8882: 42882, 8883: 42883, 8885: 42885, 8886: 42886, 8889: 42889, 8890: 42890, 8891: 42891})


class US3SelectorTest(unittest.TestCase):
    def test_all_ipv4_direct_ports_are_mapped(self) -> None:
        for old_port, new_port in module.PORT_MAP.items():
            line = f'vless://opaque@23.94.168.233:{old_port}#US3 IPv4 port {old_port}'
            candidate, changed = module.replace_uri_endpoint(line)
            self.assertTrue(changed)
            parsed = urllib.parse.urlsplit(candidate)
            self.assertEqual((parsed.hostname, parsed.port), (module.DOMAIN, new_port))

    def test_ipv6_is_unchanged(self) -> None:
        line = 'vless://opaque@[2602:fa4f:a30:4e15:cfa5:eca4:8d02:c3b8]:8881#US3 IPv6 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_jp2_and_jp1_are_unchanged(self) -> None:
        for label, host in (('JP2 IPv4', '156.231.115.204'), ('JP1 IPv4', '154.83.91.144')):
            line = f'vless://opaque@{host}:8881#{label}'
            with self.subTest(label=label):
                self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_clash_maps_only_us3_ipv4(self) -> None:
        proxies = [{'name': f'US3 IPv4 {port}', 'server': '23.94.168.233', 'port': port} for port in module.PORT_MAP]
        proxies += [
            {'name': 'US3 IPv6 VLESS', 'server': '2605:6f01:2000:f::e121:3c40', 'port': 8881},
            {'name': 'JP1 IPv4 VLESS', 'server': '154.83.91.144', 'port': 8881},
        ]
        candidate, changed = module.clash_candidate(json.dumps({'proxies': proxies}).encode())
        self.assertEqual(changed, len(module.PORT_MAP))
        catalog = json.loads(candidate)['proxies']
        for index, port in enumerate(module.PORT_MAP.values()):
            self.assertEqual((catalog[index]['server'], catalog[index]['port']), (module.DOMAIN, port))
        self.assertEqual(catalog[-2]['server'], '2605:6f01:2000:f::e121:3c40')
        self.assertEqual(catalog[-1]['server'], '154.83.91.144')


if __name__ == '__main__':
    unittest.main()
