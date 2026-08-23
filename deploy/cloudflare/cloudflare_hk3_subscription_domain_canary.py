#!/usr/bin/env python3
"""Rollback-capable HK3-only Subscription domain canary."""
from __future__ import annotations

import argparse
import base64
import copy
import datetime as dt
import hashlib
import json
import os
import pathlib
import re
import urllib.error
import urllib.parse
import urllib.request

API_ROOT = "https://api.cloudflare.com/client/v4"
ZONE_NAME = "427357.xyz"
DOMAIN = "hk3.427357.xyz"
HOSTS = {"39.109.50.213", "2a0f:1cc6:b240:201::240"}
KV_KEYS = (
    "sub:keys",
    "sub:payload:neko",
    "sub:payload:shadowrocket",
    "sub:payload:v2rayn",
    "sub:payload:sing-box",
    "sub:payload:clash",
)
URI_FORMATS = ("neko", "shadowrocket", "v2rayn", "sing-box")
ENV_FILE = pathlib.Path("/root/.hermes/secrets/cloudflare.env")
BACKUP_ROOT = pathlib.Path("/root")


def load_env() -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in ENV_FILE.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.removeprefix("export ").strip()] = value.strip().strip("\"'")
    return values


def api(token: str, method: str, path: str, body: bytes | None = None) -> object:
    headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
    if body is not None:
        headers["Content-Type"] = "text/plain; charset=utf-8"
    req = urllib.request.Request(API_ROOT + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as exc:
        detail = exc.read(512).decode("utf-8", "replace")
        raise RuntimeError(f"Cloudflare HTTP {exc.code}: {detail[:160]}") from exc
    if not payload.get("success"):
        raise RuntimeError("Cloudflare API request failed")
    return payload.get("result")


def raw_value(token: str, path: str) -> bytes:
    req = urllib.request.Request(
        API_ROOT + path,
        headers={"Authorization": f"Bearer {token}", "Accept": "application/octet-stream"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            return response.read()
    except urllib.error.HTTPError as exc:
        detail = exc.read(512).decode("utf-8", "replace")
        raise RuntimeError(f"Cloudflare raw-value HTTP {exc.code}: {detail[:160]}") from exc


def namespace_id(token: str, account: str) -> str:
    settings_paths = (
        f"/accounts/{account}/workers/scripts/neko-akile-vps-watchbot/settings",
        f"/accounts/{account}/workers/services/neko-akile-vps-watchbot/environments/production",
    )
    settings = None
    for path in settings_paths:
        try:
            settings = api(token, "GET", path)
            if isinstance(settings, dict):
                break
        except RuntimeError:
            continue
    if not isinstance(settings, dict):
        raise RuntimeError("Worker settings unavailable")
    bindings = settings.get("bindings") or settings.get("script", {}).get("bindings") or []
    for binding in bindings:
        if binding.get("name") == "STATE_KV":
            value = binding.get("namespace_id") or binding.get("id")
            if value:
                return str(value)
    raise RuntimeError("STATE_KV binding unavailable")


def value_path(account: str, namespace: str, key: str) -> str:
    return f"/accounts/{account}/storage/kv/namespaces/{namespace}/values/{urllib.parse.quote(key, safe='')}"


def get_values(token: str, account: str, namespace: str) -> dict[str, bytes]:
    return {key: raw_value(token, value_path(account, namespace, key)) for key in KV_KEYS}


def put_value(token: str, account: str, namespace: str, key: str, value: bytes) -> None:
    api(token, "PUT", value_path(account, namespace, key), value)


def digest(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def decode_b64(value: bytes) -> str:
    raw = value.strip()
    decoded = base64.b64decode(raw + b"=" * ((4 - len(raw) % 4) % 4))
    return decoded.decode("utf-8")


def encode_b64(value: str) -> bytes:
    return base64.b64encode(value.encode("utf-8"))


def label(line: str) -> str:
    return urllib.parse.unquote(line.rsplit("#", 1)[-1]) if "#" in line else ""


def replace_uri_host(line: str) -> tuple[str, bool]:
    if "HK3" not in label(line).upper():
        return line, False
    parsed = urllib.parse.urlsplit(line)
    if parsed.hostname not in HOSTS:
        raise RuntimeError(f"HK3 URI has unexpected host for label {label(line)!r}")
    marker = line.find("://")
    if marker < 0:
        raise RuntimeError("URI has no authority separator")
    authority_start = marker + 3
    suffix_positions = [pos for pos in (line.find("/", authority_start), line.find("?", authority_start), line.find("#", authority_start)) if pos >= 0]
    authority_end = min(suffix_positions) if suffix_positions else len(line)
    authority = line[authority_start:authority_end]
    userinfo = ""
    hostport = authority
    if "@" in authority:
        userinfo, hostport = authority.rsplit("@", 1)
        userinfo += "@"
    if hostport.startswith("["):
        close = hostport.find("]")
        if close < 0:
            raise RuntimeError("malformed IPv6 authority")
        port_suffix = hostport[close + 1:]
    else:
        colon = hostport.rfind(":")
        port_suffix = hostport[colon:] if colon >= 0 else ""
    replacement = line[:authority_start] + userinfo + DOMAIN + port_suffix + line[authority_end:]
    return replacement, True


def uri_candidate(raw: bytes) -> tuple[bytes, int]:
    decoded = decode_b64(raw)
    changed = 0
    output = []
    for line in decoded.splitlines():
        replacement, did_change = replace_uri_host(line)
        output.append(replacement)
        changed += int(did_change)
    candidate = "\n".join(output) + ("\n" if decoded.endswith(("\n", "\r")) else "")
    return encode_b64(candidate), changed


def clash_candidate(raw: bytes) -> tuple[bytes, int]:
    catalog = json.loads(raw.decode("utf-8"))
    candidate = copy.deepcopy(catalog)
    changed = 0
    for proxy in candidate.get("proxies") or []:
        name = str(proxy.get("name") or "")
        if "HK3" not in name.upper():
            continue
        host = str(proxy.get("server") or "")
        if host not in HOSTS:
            raise RuntimeError(f"HK3 Clash proxy has unexpected host for name {name!r}")
        proxy["server"] = DOMAIN
        changed += 1
    return json.dumps(candidate, ensure_ascii=False, separators=(",", ":")).encode("utf-8"), changed


def candidate_values(values: dict[str, bytes]) -> tuple[dict[str, bytes], dict[str, int]]:
    result = dict(values)
    changes: dict[str, int] = {}
    for fmt in URI_FORMATS:
        key = f"sub:payload:{fmt}"
        result[key], changes[key] = uri_candidate(values[key])
    result["sub:payload:clash"], changes["sub:payload:clash"] = clash_candidate(values["sub:payload:clash"])
    changes["sub:keys"] = 0
    if any(not count for key, count in changes.items() if key != "sub:keys"):
        raise RuntimeError(f"candidate changed no HK3 endpoints: {changes}")
    return result, changes


def validate_candidate(before: dict[str, bytes], after: dict[str, bytes]) -> None:
    if before["sub:keys"] != after["sub:keys"]:
        raise RuntimeError("sub:keys changed; refusing domain canary")
    for fmt in URI_FORMATS:
        old_lines = decode_b64(before[f"sub:payload:{fmt}"]).splitlines()
        new_lines = decode_b64(after[f"sub:payload:{fmt}"]).splitlines()
        if len(old_lines) != len(new_lines):
            raise RuntimeError(f"{fmt} line count changed")
        for old, new in zip(old_lines, new_lines):
            if "HK3" in label(old).upper():
                old_u, new_u = urllib.parse.urlsplit(old), urllib.parse.urlsplit(new)
                if new_u.hostname != DOMAIN or old_u.hostname not in HOSTS:
                    raise RuntimeError(f"{fmt} HK3 host invariant failed")
                if (new_u.scheme, new_u.username, new_u.password, new_u.port, new_u.query, new_u.fragment) != (old_u.scheme, old_u.username, old_u.password, old_u.port, old_u.query, old_u.fragment):
                    raise RuntimeError(f"{fmt} HK3 credential/query invariant failed")
            elif old != new:
                raise RuntimeError(f"{fmt} non-HK3 line changed")
    old_catalog = json.loads(before["sub:payload:clash"])
    new_catalog = json.loads(after["sub:payload:clash"])
    old_proxies = old_catalog.get("proxies") or []
    new_proxies = new_catalog.get("proxies") or []
    if len(old_proxies) != len(new_proxies):
        raise RuntimeError("Clash proxy count changed")
    for old, new in zip(old_proxies, new_proxies):
        if "HK3" in str(old.get("name") or "").upper():
            restored = dict(new)
            restored["server"] = old.get("server")
            if restored != old or new.get("server") != DOMAIN:
                raise RuntimeError("Clash HK3 proxy invariant failed")
        elif old != new:
            raise RuntimeError("Clash non-HK3 proxy changed")


def write_backup(path: pathlib.Path, values: dict[str, bytes], candidates: dict[str, bytes]) -> None:
    path.mkdir(mode=0o700, parents=True, exist_ok=False)
    manifest = {"keys": {}, "created_at": dt.datetime.now(dt.timezone.utc).isoformat()}
    for key in KV_KEYS:
        filename = "value-" + key.replace(":", "_")
        (path / filename).write_bytes(values[key])
        os.chmod(path / filename, 0o600)
        manifest["keys"][key] = {
            "file": filename,
            "before_sha256": digest(values[key]),
            "before_bytes": len(values[key]),
            "candidate_sha256": digest(candidates[key]),
            "candidate_bytes": len(candidates[key]),
        }
    (path / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    os.chmod(path / "manifest.json", 0o600)


def apply(token: str, account: str, namespace: str) -> None:
    before = get_values(token, account, namespace)
    after, changes = candidate_values(before)
    validate_candidate(before, after)
    backup = BACKUP_ROOT / ("neko-vpn-backup-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-hk3-subscription-domain")
    write_backup(backup, before, after)
    applied: list[str] = []
    try:
        for key in KV_KEYS:
            if before[key] == after[key]:
                continue
            put_value(token, account, namespace, key, after[key])
            applied.append(key)
        readback = get_values(token, account, namespace)
        validate_candidate(before, readback)
    except Exception:
        for key in reversed(applied):
            current = get_values(token, account, namespace)[key]
            if current != after[key]:
                raise RuntimeError(f"rollback refused because {key} changed after apply failure")
            put_value(token, account, namespace, key, before[key])
        raise
    print(json.dumps({"status": "HK3_SUBSCRIPTION_DOMAIN_CANARY_APPLIED", "record": DOMAIN, "changes": changes, "backup": str(backup), "changed_keys": applied}, sort_keys=True))


def rollback(token: str, account: str, namespace: str, backup: pathlib.Path) -> None:
    manifest = json.loads((backup / "manifest.json").read_text())
    current = get_values(token, account, namespace)
    for key in KV_KEYS:
        expected = manifest["keys"][key]["candidate_sha256"]
        if digest(current[key]) != expected:
            raise RuntimeError(f"rollback refused because current {key} is not the recorded candidate")
    for key in KV_KEYS:
        old = (backup / manifest["keys"][key]["file"]).read_bytes()
        put_value(token, account, namespace, key, old)
    print(json.dumps({"status": "HK3_SUBSCRIPTION_DOMAIN_CANARY_ROLLBACK_OK", "backup": str(backup)}, sort_keys=True))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("dry-run", "apply", "rollback"))
    parser.add_argument("--backup", type=pathlib.Path)
    args = parser.parse_args()
    env = load_env()
    account = env["CLOUDFLARE_ACCOUNT_ID"]
    token = env["CLOUDFLARE_API_TOKEN"]
    namespace = namespace_id(token, account)
    before = get_values(token, account, namespace)
    if args.action == "dry-run":
        after, changes = candidate_values(before)
        validate_candidate(before, after)
        print(json.dumps({"status": "HK3_SUBSCRIPTION_DOMAIN_CANARY_DRY_RUN_OK", "record": DOMAIN, "changes": changes, "changed_keys": [key for key in KV_KEYS if before[key] != after[key]]}, sort_keys=True))
    elif args.action == "apply":
        apply(token, account, namespace)
    else:
        if not args.backup:
            raise SystemExit("--backup is required for rollback")
        rollback(token, account, namespace, args.backup)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
