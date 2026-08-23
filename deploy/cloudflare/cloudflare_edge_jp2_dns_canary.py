#!/usr/bin/env python3
"""Add/rollback the HK2-backed JP2 IPv4 edge DNS canary."""
from __future__ import annotations

import argparse
import fcntl
import json
import os
import pathlib
import tempfile
from typing import Any
import urllib.error
import urllib.parse
import urllib.request

API = 'https://api.cloudflare.com/client/v4'
ZONE = '427357.xyz'
NAME = 'edge-jp2.427357.xyz'
IP = '141.11.148.116'
STATE = pathlib.Path('/www/projects/vpn-3xui-unification/audit/edge-jp2-dns-canary-20260823.json')
LOCK = STATE.with_name(STATE.name + '.lock')
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


def api(token: str, method: str, path: str, body: dict | None = None, allow_404: bool = False) -> Any:
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
        if allow_404 and exc.code == 404:
            return None
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


def records_for_name(token: str, zone: str) -> list[dict[str, Any]]:
    all_rows: list[dict[str, Any]] = []
    for page in range(1, 101):
        query = f'/zones/{zone}/dns_records?name={urllib.parse.quote(NAME)}&per_page=100&page={page}'
        rows = api(token, 'GET', query) or []
        all_rows.extend(row for row in rows if row.get('name') == NAME)
        if len(rows) < 100:
            return all_rows
    raise RuntimeError('DNS record pagination exceeded safety limit')


def owned_record(record: Any) -> bool:
    return bool(
        isinstance(record, dict)
        and record.get('type') == 'A'
        and record.get('name') == NAME
        and record.get('content') == IP
        and record.get('proxied') is False
    )


def load_state() -> dict[str, Any] | None:
    if not STATE.exists():
        return None
    try:
        state = json.loads(STATE.read_text(encoding='utf-8'))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError('JP2 DNS state is unreadable; refusing to continue') from exc
    if state.get('zone') != ZONE or state.get('name') != NAME or state.get('expected') != IP:
        raise RuntimeError('JP2 DNS state identity mismatch; refusing to continue')
    created = state.get('created')
    if not isinstance(created, list) or len(created) != 1 or not created[0].get('id'):
        raise RuntimeError('JP2 DNS state ownership is incomplete; refusing to continue')
    return state


def atomic_write_state(state: dict[str, Any]) -> None:
    STATE.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd, tmp_name = tempfile.mkstemp(prefix=STATE.name + '.', dir=STATE.parent)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            json.dump(state, stream, indent=2, sort_keys=True)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        # The temp file is already 0600 and fully fsynced before the atomic
        # rename. After os.replace succeeds, the state is committed; post-
        # rename directory durability is best-effort and must not trigger DNS
        # deletion, which would leave an orphaned state/ownership mismatch.
        os.replace(tmp_name, STATE)
        try:
            os.chmod(STATE, 0o600)
        except OSError:
            pass
        try:
            dir_fd = os.open(STATE.parent, os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0))
            try:
                os.fsync(dir_fd)
            finally:
                os.close(dir_fd)
        except OSError:
            pass
    finally:
        try:
            os.unlink(tmp_name)
        except FileNotFoundError:
            pass


def remove_state() -> None:
    STATE.unlink()
    dir_fd = os.open(STATE.parent, os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0))
    try:
        os.fsync(dir_fd)
    finally:
        os.close(dir_fd)


def delete_owned(token: str, zone: str, record_id: str) -> None:
    current = api(token, 'GET', f'/zones/{zone}/dns_records/{record_id}', allow_404=True)
    if current is None:
        raise RuntimeError('owned JP2 DNS record is already missing')
    if not owned_record(current):
        raise RuntimeError('JP2 DNS record ownership changed; refusing deletion')
    api(token, 'DELETE', f'/zones/{zone}/dns_records/{record_id}')
    if api(token, 'GET', f'/zones/{zone}/dns_records/{record_id}', allow_404=True) is not None:
        raise RuntimeError('JP2 DNS record still exists after deletion')


def apply(token: str, account: str) -> int:
    zone = zone_id(token, account)
    STATE.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with LOCK.open('a+', encoding='utf-8') as lock_stream:
        os.fchmod(lock_stream.fileno(), 0o600)
        fcntl.flock(lock_stream.fileno(), fcntl.LOCK_EX)
        if STATE.exists():
            state = load_state()
            assert state is not None
            record_id = str(state['created'][0]['id'])
            current = api(token, 'GET', f'/zones/{zone}/dns_records/{record_id}', allow_404=True)
            if owned_record(current):
                print(json.dumps({'status': 'EDGE_JP2_DNS_CANARY_ALREADY_APPLIED', 'record': NAME}, sort_keys=True))
                return 0
            raise RuntimeError('JP2 DNS state exists but its record is not owned/current; refusing apply')

        conflicts = records_for_name(token, zone)
        if conflicts:
            raise RuntimeError('existing DNS record(s) for edge-jp2.427357.xyz; refusing to adopt or overwrite')

        created = api(token, 'POST', f'/zones/{zone}/dns_records', {
            'type': 'A', 'name': NAME, 'content': IP, 'ttl': 120,
            'proxied': False, 'comment': 'JP2 IPv4 L4 edge canary',
        })
        if not owned_record(created):
            raise RuntimeError('Cloudflare returned a non-owned JP2 DNS record')
        state = {
            'zone': ZONE,
            'name': NAME,
            'expected': IP,
            'created': [{'id': created['id'], 'type': created['type'], 'content': created['content'], 'proxied': False}],
        }
        try:
            atomic_write_state(state)
        except Exception as exc:
            try:
                delete_owned(token, zone, str(created['id']))
            except Exception as cleanup_exc:
                raise RuntimeError('state write failed and DNS cleanup also failed; manual ownership recovery required') from cleanup_exc
            raise RuntimeError('state write failed; newly created DNS record was removed') from exc
        print(json.dumps({'status': 'EDGE_JP2_DNS_CANARY_APPLIED', 'record': NAME, 'created_count': 1, 'state': str(STATE)}, sort_keys=True))
        return 0


def rollback(token: str, account: str) -> int:
    zone = zone_id(token, account)
    with LOCK.open('a+', encoding='utf-8') as lock_stream:
        os.fchmod(lock_stream.fileno(), 0o600)
        fcntl.flock(lock_stream.fileno(), fcntl.LOCK_EX)
        state = load_state()
        assert state is not None
        delete_owned(token, zone, str(state['created'][0]['id']))
        remove_state()
        print(json.dumps({'status': 'EDGE_JP2_DNS_CANARY_ROLLBACK_OK', 'deleted_count': 1}, sort_keys=True))
        return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('apply', 'rollback'))
    args = parser.parse_args()
    env = load_env()
    if args.action == 'apply':
        return apply(env['CLOUDFLARE_API_TOKEN'], env['CLOUDFLARE_ACCOUNT_ID'])
    return rollback(env['CLOUDFLARE_API_TOKEN'], env['CLOUDFLARE_ACCOUNT_ID'])


if __name__ == '__main__':
    raise SystemExit(main())
