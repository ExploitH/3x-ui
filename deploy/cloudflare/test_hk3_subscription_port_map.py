from __future__ import annotations

import importlib.util
import json
import pathlib
import unittest
import urllib.parse

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / 'cloudflare_hk3_subscription_domain_canary.py'
spec = importlib.util.spec_from_file_location('hk3_subscription_canary', SCRIPT)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
setattr(module, 'DOMAIN', 'edge-hk3.427357.xyz')
setattr(module, 'HOSTS', {'hk3.427357.xyz'})
setattr(module, 'PORT_MAP', {8881: 38881, 8882: 38882, 8883: 38883})


class HK3EdgePortMapTest(unittest.TestCase):
    def test_vless_hy2_tuic_ports(self) -> None:
        for scheme, old, new in (
            ('vless', 8881, 38881),
            ('hysteria2', 8882, 38882),
            ('tuic', 8883, 38883),
        ):
            line = f'{scheme}://opaque@hk3.427357.xyz:{old}#HK3 test'
            candidate, changed = module.replace_uri_endpoint(line)
            self.assertTrue(changed)
            parsed = urllib.parse.urlsplit(candidate)
            self.assertEqual((parsed.hostname, parsed.port), ('edge-hk3.427357.xyz', new))

    def test_unknown_port_fails_closed(self) -> None:
        with self.assertRaises(RuntimeError):
            module.replace_uri_endpoint('vless://opaque@hk3.427357.xyz:9999#HK3 bad')

    def test_non_hk3_entry_is_byte_stable(self) -> None:
        line = 'vless://opaque@us3.example:8881#US3'
        self.assertEqual(module.replace_uri_endpoint(line), (line, False))

    def test_clash_server_and_port(self) -> None:
        raw = json.dumps({'proxies': [
            {'name': 'HK3 VLESS', 'server': 'hk3.427357.xyz', 'port': 8881},
            {'name': 'US3', 'server': 'us3.example', 'port': 8881},
        ]}).encode()
        candidate, changed = module.clash_candidate(raw)
        self.assertEqual(changed, 1)
        catalog = json.loads(candidate)
        self.assertEqual(catalog['proxies'][0]['server'], 'edge-hk3.427357.xyz')
        self.assertEqual(catalog['proxies'][0]['port'], 38881)
        self.assertEqual(catalog['proxies'][1], {'name': 'US3', 'server': 'us3.example', 'port': 8881})


if __name__ == '__main__':
    unittest.main()
