# sing-box read-only Node API adapter

This package is the first compatibility slice for existing sing-box-only nodes.
It intentionally exposes only the subset needed for a Master health/inventory
canary:

```text
GET  <base>/healthz
GET  <base>/panel/api/server/status
GET  <base>/panel/api/server/capabilities
GET  <base>/panel/api/inbounds/list
```

The capability endpoint returns an explicit gate for the current adapter mode:

```json
{
  "mode": "readonly",
  "config": true,
  "inboundInventory": true,
  "clientCrud": false,
  "clientEnable": false,
  "perClientTraffic": false,
  "trafficReset": false,
  "clientIp": false,
  "relayIdentity": false
}
```

The Master must not enable a node while `perClientTraffic` is false.

All requests require `Authorization: Bearer <token>`. Any mutation is rejected
with HTTP 405. The adapter never writes the sing-box config, never restarts
sing-box, and never exposes user UUID/password fields.

## Build

```bash
go build -trimpath -o /usr/local/bin/singbox-adapter ./cmd/singbox-adapter
```

## Configuration

Environment variables:

| Variable | Default |
|---|---|
| `SINGBOX_ADAPTER_CONFIG` | `/etc/sing-box/config.json` |
| `SINGBOX_ADAPTER_TOKEN_FILE` | `/etc/sing-box/adapter.token` |
| `SINGBOX_ADAPTER_LISTEN` | `127.0.0.1:23854` |
| `SINGBOX_ADAPTER_BASE_PATH` | `/adapter/` |

The token file must be mode `0600` or stricter. The adapter reloads the JSON
configuration for each inbound-list request and rejects malformed configs,
missing tags, duplicate tags, invalid ports, or stable inbound-ID collisions.

## HK3 canary deployment boundary

The included systemd unit is intended for the HK3 local canary only. It is
loopback-only and reads `/etc/sing-box/config.json` from the existing HK3
sing-box direct node. It must not be installed on JP1* relay nodes.

Before starting it on HK3:

1. Back up the unit/env/token paths.
2. Record the existing sing-box config SHA-256 and service state.
3. Generate a fresh random token at mode `0600`.
4. Start only `singbox-adapter.service`.
5. Verify `/healthz`, `/panel/api/server/status`, and `/panel/api/inbounds/list`
   through a local SSH tunnel or from the HK3 Master.
6. Verify the existing sing-box config hash, listeners, and service state are
   unchanged.
7. Roll back by stopping/disabling only `singbox-adapter.service` and removing
   its files.

This slice does not yet provide client CRUD, enable/disable, traffic counters,
reset, or provider billing import. Those are deliberately separate gates so a
read-only Master↔Node canary cannot mutate production data-plane state.
