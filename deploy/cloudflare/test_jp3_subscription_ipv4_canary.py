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
setattr(module, 'HOSTS', {'156.246.90.242'})
setattr(module, 'TARGET_MARKER', 'JP3')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'DOMAIN', 'edge-jp3.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 40881, 8882: 40882, 8883: 40883, 8885: 40885, 8886: 40886, 8889: 40889, 8890: 40890, 8891: 40891})


class JP3SelectorTest(unittest.TestCase):
    def test_all_ipv4_direct_ports_are_mapped(self) -> None:
        for old_port, new_port in module.PORT_MAP.items():
            line = f'vless://opaque@156.246.90.242:{old_port}#JP3 IPv4 port {old_port}'
            candidate, changed = module.replace_uri_endpoint(line)
            self.assertTrue(changed)
            parsed = urllib.parse.urlsplit(candidate)
            self.assertEqual((parsed.hostname, parsed.port), (module.DOMAIN, new_port))

    def test_ipv6_is_unchanged(self) -> None:
        line = 'vless://opaque@[2602:fa4f:a30:4e15:cfa5:eca4:8d02:c3b8]:8881#JP3 IPv6 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_jp2_and_jp1_are_unchanged(self) -> None:
        for label, host in (('JP2 IPv4', '156.231.115.204'), ('JP1 IPv4', '154.83.91.144')):
            line = f'vless://opaque@{host}:8881#{label}'
            with self.subTest(label=label):
                self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_clash_maps_only_jp3_ipv4(self) -> None:
        proxies = [{'name': f'JP3 IPv4 {port}', 'server': '156.246.90.242', 'port': port} for port in module.PORT_MAP]
        proxies += [
            {'name': 'JP3 IPv6 VLESS', 'server': '2602:fa4f:a30:4e15:cfa5:eca4:8d02:c3b8', 'port': 8881},
            {'name': 'JP1 IPv4 VLESS', 'server': '154.83.91.144', 'port': 8881},
        ]
        candidate, changed = module.clash_candidate(json.dumps({'proxies': proxies}).encode())
        self.assertEqual(changed, len(module.PORT_MAP))
        catalog = json.loads(candidate)['proxies']
        for index, port in enumerate(module.PORT_MAP.values()):
            self.assertEqual((catalog[index]['server'], catalog[index]['port']), (module.DOMAIN, port))
        self.assertEqual(catalog[-2]['server'], '2602:fa4f:a30:4e15:cfa5:eca4:8d02:c3b8')
        self.assertEqual(catalog[-1]['server'], '154.83.91.144')


if __name__ == '__main__':
    unittest.main()
