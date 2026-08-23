# sing-box read-only Node API adapter

This package is the first compatibility slice for existing sing-box-only nodes.
It intentionally exposes only the subset needed for a Master health/inventory
canary:

```text
GET  <base>/healthz
GET  <base>/panel/api/server/status
GET  <base>/panel/api/server/capabilities
GET  <base>/panel/api/inbounds/list
GET  <base>/panel/api/traffic/snapshot  (when V2Ray API is configured)
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

When `SINGBOX_ADAPTER_V2RAY_API` points to a loopback V2Ray API and the
sing-box config has `experimental.v2ray_api.stats.enabled=true`, the adapter
also exposes a read-only snapshot. Every inbound user must have a stable
`name`, and that name must be present in `stats.users`; every inbound tag must
be present in `stats.inbounds`.

The snapshot queries `QueryStats` with reset disabled and returns both user
counters and physical inbound counters. It never calls the V2Ray reset API.
The capability becomes `mode=traffic-readonly` and `perClientTraffic=true`, but
`clientCrud=false` and `clientEnable=false` remain false; the Master must not
enable this Node as a full managed node.

All requests require `Authorization: Bearer ***`. The default command wiring rejects
mutations with HTTP 405. The library has an explicit, non-default managed-client
enable seam, but `cmd/singbox-adapter` does not construct it yet; enabling that seam
requires a later production canary with a canonical managed-state file, atomic
config rollback, sing-box validation, reload, and health callbacks. The adapter
never exposes user UUID/password fields in API responses.

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
| `SINGBOX_ADAPTER_V2RAY_API` | empty; disables traffic-readonly mode |
| `SINGBOX_ADAPTER_MANAGED` | `false`; managed writes are disabled by default |
| `SINGBOX_ADAPTER_MANAGED_STATE` | `/etc/sing-box/managed/managed-relay-state.json` when explicitly enabled |
| `SINGBOX_ADAPTER_SINGBOX_BIN` | `/usr/local/bin/sing-box` when explicitly enabled |
| `SINGBOX_ADAPTER_SINGBOX_SERVICE` | `sing-box.service` when explicitly enabled |

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

The default deployed command does not yet provide client CRUD, enable/disable,
traffic reset, or provider billing import. The library now contains an opt-in
managed canary seam with these mutation paths:

```text
POST <base>/panel/api/clients/add
POST <base>/panel/api/clients/update/<email>?inboundIds=<id>
POST <base>/panel/api/clients/<email>/detach
POST <base>/panel/api/clients/del/<email>
```

The managed seam requires an explicit canonical state store, atomic config
rollback, sing-box validation, reload callback, and health callback. It reports
`clientCrud=true` and `clientEnable=true` only when those prerequisites are
ready. With valid V2Ray API per-client stats it additionally reports
`perClientTraffic=true` and `trafficSource=sing-box-v2ray-api`, allowing the
Master managed gate to evaluate it. Node quota block/reset requests carry an
internal mutation reason so quota reset does not restore an admin-disabled user.

The default process remains read-only and does not construct this seam. Traffic
snapshot is read-only and requires the explicit V2Ray API configuration above.
Relay-to-exit identity propagation remains a separate gate.
