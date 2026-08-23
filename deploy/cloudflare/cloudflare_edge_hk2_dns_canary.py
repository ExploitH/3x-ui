#!/usr/bin/env python3
"""Add/rollback the HK1-backed HK2 IPv4 edge DNS canary."""
from __future__ import annotations

import argparse
import json
import os
import pathlib
from typing import Any
import urllib.error
import urllib.parse
import urllib.request

API = 'https://api.cloudflare.com/client/v4'
ZONE = '427357.xyz'
NAME = 'edge-hk2.427357.xyz'
IP = '91.229.132.66'
STATE = pathlib.Path('/www/projects/vpn-3xui-unification/audit/edge-hk2-dns-canary-20260823.json')
ENV = pathlib.Path('/root/.hermes/secrets/cloudflare.env')


def load_env() -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in ENV.read_text(encoding='utf-8').splitlines():
        line = raw.strip()
        if not line or line.startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        values[key.removeprefix('export ').strip()] = value.strip().strip('"\'')
    return values


def api(token: str, method: str, path: str, body: dict | None = None) -> Any:
    headers = {'Authorization': f'Bearer {token}', 'Accept': 'application/json'}
    data = None
    if body is not None:
        headers['Content-Type'] = 'application/json'
        data = json.dumps(body).encode()
    request = urllib.request.Request(API + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=45) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f'Cloudflare HTTP {exc.code}') from exc
    if not payload.get('success'):
        raise RuntimeError('Cloudflare API failure')
    return payload.get('result')


def zone_id(token: str, account: str) -> str:
    rows = api(token, 'GET', f'/zones?account.id={urllib.parse.quote(account)}&name={ZONE}')
    matches = [row for row in rows or [] if row.get('name') == ZONE]
    if len(matches) != 1:
        raise RuntimeError('zone count mismatch')
    return str(matches[0]['id'])


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('apply', 'rollback'))
    args = parser.parse_args()
    env = load_env()
    token = env['CLOUDFLARE_API_TOKEN']
    zone = zone_id(token, env['CLOUDFLARE_ACCOUNT_ID'])
    query = f'/zones/{zone}/dns_records?type=A&name={NAME}'
    rows = api(token, 'GET', query) or []
    if args.action == 'apply':
        conflicts = [row for row in rows if row.get('content') != IP or row.get('proxied') is not False]
        if conflicts:
            raise RuntimeError('conflicting edge-hk2 record')
        exact = next((row for row in rows if row.get('content') == IP and row.get('proxied') is False), None)
        created = [] if exact else [api(token, 'POST', f'/zones/{zone}/dns_records', {
            'type': 'A', 'name': NAME, 'content': IP, 'ttl': 120,
            'proxied': False, 'comment': 'HK2 IPv4 L4 edge canary',
        })]
        STATE.write_text(json.dumps({'zone': ZONE, 'name': NAME, 'expected': IP, 'created': [
            {'id': row['id'], 'type': row['type'], 'content': row['content']} for row in created
        ]}, indent=2) + '\n', encoding='utf-8')
        os.chmod(STATE, 0o600)
        print(json.dumps({'status': 'EDGE_HK2_DNS_CANARY_APPLIED', 'record': NAME, 'created_count': len(created), 'state': str(STATE)}, sort_keys=True))
        return 0
    saved = json.loads(STATE.read_text(encoding='utf-8'))
    for row in saved.get('created', []):
        current = api(token, 'GET', f'/zones/{zone}/dns_records/{row["id"]}')
        if current.get('name') != NAME or current.get('content') != IP or current.get('proxied') is not False:
            raise RuntimeError('changed record; refuse rollback')
        api(token, 'DELETE', f'/zones/{zone}/dns_records/{row["id"]}')
    print(json.dumps({'status': 'EDGE_HK2_DNS_CANARY_ROLLBACK_OK', 'deleted_count': len(saved.get('created', []))}, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
