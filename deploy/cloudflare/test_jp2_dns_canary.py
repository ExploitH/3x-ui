from __future__ import annotations

import importlib.util
import pathlib
import unittest

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / 'cloudflare_edge_jp2_dns_canary.py'
spec = importlib.util.spec_from_file_location('jp2_dns_canary', SCRIPT)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class JP2DNSOwnershipTest(unittest.TestCase):
    def test_same_name_records_are_paginated(self) -> None:
        calls: list[str] = []
        original_api = module.api

        def fake_api(token: str, method: str, path: str, body=None, allow_404: bool = False):
            calls.append(path)
            if path.endswith('page=1'):
                return [{'name': module.NAME}] * 100
            return []

        setattr(module, 'api', fake_api)
        try:
            rows = module.records_for_name('token', 'zone')
        finally:
            setattr(module, 'api', original_api)
        self.assertEqual(len(rows), 100)
        self.assertEqual(len(calls), 2)

    def test_expected_a_record_is_owned(self) -> None:
        self.assertTrue(module.owned_record({
            'id': 'record-id',
            'type': 'A',
            'name': 'edge-jp2.427357.xyz',
            'content': '141.11.148.116',
            'proxied': False,
        }))

    def test_cname_is_not_owned(self) -> None:
        self.assertFalse(module.owned_record({
            'id': 'record-id',
            'type': 'CNAME',
            'name': 'edge-jp2.427357.xyz',
            'content': 'other.example.',
            'proxied': False,
        }))

    def test_wrong_ip_or_proxy_is_not_owned(self) -> None:
        for content, proxied in (('141.11.148.117', False), ('141.11.148.116', True)):
            with self.subTest(content=content, proxied=proxied):
                self.assertFalse(module.owned_record({
                    'id': 'record-id',
                    'type': 'A',
                    'name': 'edge-jp2.427357.xyz',
                    'content': content,
                    'proxied': proxied,
                }))


if __name__ == '__main__':
    unittest.main()
