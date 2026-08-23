#!/usr/bin/env python3
"""Add or rollback the additive HK3 DNS canary; never changes subscriptions."""
from __future__ import annotations

import argparse
import json
import os
import pathlib
import socket
import sys
import urllib.error
import urllib.parse
import urllib.request

API_ROOT = "https://api.cloudflare.com/client/v4"
ZONE_NAME = "427357.xyz"
RECORD_NAME = "hk3.427357.xyz"
EXPECTED = {
    "A": "39.109.50.213",
    "AAAA": "2a0f:1cc6:b240:201::240",
}
STATE = pathlib.Path("/www/projects/vpn-3xui-unification/audit/hk3-dns-canary-20260823.json")
ENV_FILE = pathlib.Path("/root/.hermes/secrets/cloudflare.env")


def load_env() -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in ENV_FILE.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.removeprefix("export ").strip()] = value.strip().strip("\"'")
    return values


def api(token: str, method: str, path: str, body: dict | None = None):
    data = None if body is None else json.dumps(body).encode()
    headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(API_ROOT + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=45) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as exc:
        detail = exc.read(512).decode("utf-8", "replace")
        raise RuntimeError(f"Cloudflare HTTP {exc.code} at {path}: {detail[:160]}") from exc
    if not payload.get("success"):
        raise RuntimeError(f"Cloudflare API failure at {path}")
    return payload.get("result")


def zone_id(token: str, account: str) -> str:
    rows = api(token, "GET", "/zones?account.id=" + urllib.parse.quote(account) + "&name=" + ZONE_NAME)
    matches = [x for x in rows or [] if x.get("name") == ZONE_NAME]
    if len(matches) != 1:
        raise RuntimeError(f"expected one zone, got {len(matches)}")
    return str(matches[0]["id"])


def records(token: str, zone: str, record_type: str) -> list[dict]:
    path = "/zones/%s/dns_records?type=%s&name=%s" % (
        zone, record_type, urllib.parse.quote(RECORD_NAME, safe=""),
    )
    return api(token, "GET", path) or []


def exact(rows: list[dict], record_type: str) -> dict | None:
    wanted = EXPECTED[record_type]
    for row in rows:
        if row.get("name") != RECORD_NAME:
            continue
        if row.get("content") == wanted and row.get("proxied") is False:
            return row
    return None


def conflicts(rows: list[dict], record_type: str) -> list[dict]:
    return [
        row for row in rows
        if row.get("name") == RECORD_NAME
        and not (row.get("content") == EXPECTED[record_type] and row.get("proxied") is False)
    ]


def resolve() -> dict[str, list[str]]:
    out: dict[str, list[str]] = {}
    try:
        for family, socktype in (("A", socket.AF_INET), ("AAAA", socket.AF_INET6)):
            values = sorted({item[4][0] for item in socket.getaddrinfo(RECORD_NAME, None, socktype, 0, 0)})
            out[family] = values
    except OSError as exc:
        out["error"] = [type(exc).__name__]
    return out


def apply(token: str, account: str) -> None:
    zone = zone_id(token, account)
    found: dict[str, list[dict]] = {kind: records(token, zone, kind) for kind in EXPECTED}
    for kind, rows in found.items():
        if conflicts(rows, kind):
            raise RuntimeError(f"conflicting {kind} record exists for {RECORD_NAME}; refusing mutation")
    created = []
    retained = []
    try:
        for kind, content in EXPECTED.items():
            row = exact(found[kind], kind)
            if row:
                retained.append({"type": kind, "id": row["id"], "content": content})
                continue
            row = api(token, "POST", f"/zones/{zone}/dns_records", {
                "type": kind, "name": RECORD_NAME, "content": content,
                "ttl": 120, "proxied": False,
                "comment": "HK3 additive domain canary; rollback via recorded state",
            })
            created.append({"type": kind, "id": row["id"], "content": content})
        STATE.parent.mkdir(parents=True, exist_ok=True)
        STATE.write_text(json.dumps({
            "zone": ZONE_NAME,
            "record": RECORD_NAME,
            "expected": EXPECTED,
            "created_records": created,
            "retained_records": retained,
            "resolved": resolve(),
        }, ensure_ascii=False, indent=2) + "\n")
        os.chmod(STATE, 0o600)
        print(json.dumps({
            "status": "DNS_CANARY_APPLIED",
            "record": RECORD_NAME,
            "created": created,
            "retained": retained,
            "resolved": resolve(),
            "state": str(STATE),
        }, ensure_ascii=False, sort_keys=True))
    except Exception:
        for row in reversed(created):
            try:
                api(token, "DELETE", f"/zones/{zone}/dns_records/{row['id']}")
            except Exception:
                pass
        raise


def rollback(token: str, account: str) -> None:
    if not STATE.exists():
        raise RuntimeError(f"state file missing: {STATE}")
    state = json.loads(STATE.read_text())
    if state.get("record") != RECORD_NAME or state.get("zone") != ZONE_NAME:
        raise RuntimeError("state scope mismatch")
    zone = zone_id(token, account)
    deleted = []
    for row in state.get("created_records", []):
        current = api(token, "GET", f"/zones/{zone}/dns_records/{row['id']}")
        if current.get("name") != RECORD_NAME or current.get("type") != row["type"] or current.get("content") != row["content"] or current.get("proxied") is not False:
            raise RuntimeError(f"record {row['id']} changed; refusing destructive rollback")
        api(token, "DELETE", f"/zones/{zone}/dns_records/{row['id']}")
        deleted.append(row)
    print(json.dumps({"status": "DNS_CANARY_ROLLBACK_OK", "deleted": deleted}, ensure_ascii=False, sort_keys=True))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("apply", "rollback"))
    args = parser.parse_args()
    env = load_env()
    account = env["CLOUDFLARE_ACCOUNT_ID"]
    token = env["CLOUDFLARE_API_TOKEN"]
    if args.action == "apply":
        apply(token, account)
    else:
        rollback(token, account)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
