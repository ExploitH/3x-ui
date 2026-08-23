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
setattr(module, 'HOSTS', {'91.229.132.66'})
setattr(module, 'TARGET_MARKER', 'HK1')
setattr(module, 'IPV4_ONLY', True)
setattr(module, 'EXCLUDE_LABEL_MARKERS', ('SHADOWSOCKS', 'SHADOWTLS'))
setattr(module, 'DOMAIN', 'edge-hk1.427357.xyz')
setattr(module, 'PORT_MAP', {8881: 43881, 8882: 43882, 8883: 43883, 8886: 43886, 8889: 43889, 8890: 43890, 8891: 43891, 28882: 43892, 28883: 43893, 28884: 43894, 28885: 43895, 28886: 43896, 28887: 43897})


class HK1SelectorTest(unittest.TestCase):
    def test_all_ipv4_direct_ports_are_mapped(self) -> None:
        for old_port, new_port in module.PORT_MAP.items():
            line = f'vless://opaque@91.229.132.66:{old_port}#HK1 IPv4 port {old_port}'
            candidate, changed = module.replace_uri_endpoint(line)
            self.assertTrue(changed)
            parsed = urllib.parse.urlsplit(candidate)
            self.assertEqual((parsed.hostname, parsed.port), (module.DOMAIN, new_port))

    def test_ipv6_is_unchanged(self) -> None:
        line = 'vless://opaque@[2602:fa4f:a30:4e15:cfa5:eca4:8d02:c3b8]:8881#HK1 IPv6 VLESS'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_relay_ports_are_mapped(self) -> None:
        for old_port in (28882, 28883, 28884, 28885, 28886, 28887):
            line = f'vless://opaque@91.229.132.66:{old_port}#HK1入口 relay IPv4 {old_port}'
            candidate, changed = module.replace_uri_endpoint(line)
            self.assertTrue(changed)
            parsed = urllib.parse.urlsplit(candidate)
            self.assertEqual((parsed.hostname, parsed.port), (module.DOMAIN, module.PORT_MAP[old_port]))

    def test_shadowsocks_and_shadowtls_are_not_enabled(self) -> None:
        for label, line in (
            ('Shadowsocks', 'ss://opaque@91.229.132.66:8885#🇭🇰 HK1 · IPv4 · Shadowsocks'),
            ('ShadowTLS', 'ss://opaque@91.229.132.66:8884#🇭🇰 HK1 · IPv4 · ShadowTLS'),
            ('opaque Shadowsocks', 'ss://opaque-host-without-port#🇭🇰 HK1 · IPv4 · Shadowsocks'),
        ):
            with self.subTest(label=label):
                self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_jp2_and_jp1_are_unchanged(self) -> None:
        for label, host in (('JP2 IPv4', '156.231.115.204'), ('JP1 IPv4', '154.83.91.144')):
            line = f'vless://opaque@{host}:8881#{label}'
            with self.subTest(label=label):
                self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_clash_maps_only_hk1_ipv4(self) -> None:
        proxies = [{'name': f'HK1 IPv4 {port}', 'server': '91.229.132.66', 'port': port} for port in module.PORT_MAP]
        proxies += [
            {'name': 'HK1 IPv6 VLESS', 'server': '2605:6f01:2000:f::e121:3c40', 'port': 8881},
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
