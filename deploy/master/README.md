# HK3 Master control-plane upgrade

`hk3_master_control_plane_upgrade.sh` is a rollback-capable binary-only upgrade
helper for the HK3 forked 3x-ui Master. It must be run through the saved
`hk3-vps` SSH session-manager profile, never with credentials placed in command
arguments.

The helper:

- verifies the GitHub archive SHA-256, inner manifest, `SOURCE_COMMIT`, and binary version before stopping the Master;
- stops only `x-ui.service`; `sing-box-hk3.service` and `singbox-adapter.service` remain running;
- creates a consistent SQLite snapshot with Python's SQLite Backup API after all Master writers stop, then runs `quick_check`;
- preserves the existing HK3-customized systemd unit, environment, database directory, node-token key, and credentials;
- installs only the new Master binary;
- verifies panel health, Node id `4` remains disabled, the sing-box config hash is unchanged, and sing-box/adapter service states are unchanged;
- on failure restores the binary, unit, environment, machine-local `/etc/x-ui` files, the consistent SQLite snapshot, and the prior Master systemd state.

The helper never changes the sing-box data-plane configuration, DNS,
Subscription, firewall, HK→JP AI route, or any JP1* relay node.

Example release invocation:

```bash
bash deploy/master/hk3_master_control_plane_upgrade.sh \
  https://github.com/ExploitH/3x-ui/releases/download/hk3-master-fe0c9eb0/hk3-master-fe0c9eb0.tar.gz \
  a9df4d1037ae31220f628e1efc5a61c89ed5679eebd55ce5b93b1b4680d68399 \
  fe0c9eb035266d21941e3becf6f12d4b9a9e3ce4 \
  dev+fe0c9eb0
```

Do not enable Node id `4` as part of this upgrade. Enabling it requires a later
traffic-source and quota-mapping canary with separate verification.
