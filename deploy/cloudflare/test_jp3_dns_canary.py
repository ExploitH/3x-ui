from __future__ import annotations

import importlib.util
import pathlib
import unittest

HERE = pathlib.Path(__file__).resolve().parent
SCRIPT = HERE / 'cloudflare_edge_jp3_dns_canary.py'
spec = importlib.util.spec_from_file_location('jp3_dns_canary', SCRIPT)
assert spec and spec.loader
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class JP3DNSOwnershipTest(unittest.TestCase):
    def test_expected_a_record_is_owned(self) -> None:
        self.assertTrue(module.owned_record({
            'id': 'record-id', 'type': 'A', 'name': 'edge-jp3.427357.xyz',
            'content': '141.11.148.116', 'proxied': False,
        }))

    def test_other_record_is_not_owned(self) -> None:
        self.assertFalse(module.owned_record({
            'id': 'record-id', 'type': 'CNAME', 'name': 'edge-jp3.427357.xyz',
            'content': 'other.example.', 'proxied': False,
        }))


if __name__ == '__main__':
    unittest.main()
