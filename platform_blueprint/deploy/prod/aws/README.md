# AWS EPC role registration

This package adds only `cleanapp_epc_pusher` to the exact migrated production runtime. It does not build/pull images, migrate data, provision credentials, restart existing roles, or change systemd units. The worker must already support `EPC_ENV_FILE`.

The three runtime helpers were copied from production on 2026-11-13; `production-helper-provenance.json` records their original hashes. Existing `backup_recovery.py`, `runtime_migration.py`, and `integrity_manifest.py` remain installed unchanged. Registration refuses helper files that differ from either the captured source or this reviewed package.

`runtime_roles.py` admits the exact original 31 roles or those roles plus EPC. Boot preserves the legacy set for rollback. Native isolation requires its snapshot to match the currently declared role set, so a legacy snapshot cannot omit EPC after registration. Weekly backup captures the declared container identities rather than every running Docker container, so unrelated preview containers neither enter the recovery set nor break its count guard. Missing/stopped production roles still fail backup.

Before registration, load the reviewed linux/amd64 image into Docker and provision `/opt/cleanapp/private/epc-pusher.env` as an owner-only regular file. The adapter never reads it into terminal output; it mounts the file read-only at `/run/cleanapp/epc-pusher.env` and explicitly includes the host path in backup configuration scope. Provision literal `NAME=value` entries using the worker's approved environment whitelist; do not use shell quoting or substitutions. Set the intended dispatch and namespace values explicitly.

Upload this public-code package to a private staging directory on the admitted AWS production host. Default invocation is a zero-mutation plan:

```sh
python3 register_epc.py --image-id sha256:IMAGE_CONFIG_DIGEST
```

The production operator applies it with `--execute`. Execution requires root, an Amazon Linux host, the existing ACTIVE marker, and absence of ISOLATED. The adapter acquires the existing exclusive deployment/backup lock; snapshots the private baseline, helpers, and backup scope; installs the reviewed helpers; adds one pinned role; validates resolved Compose identities; and runs `up -d --no-deps --no-build --pull never cleanapp_epc_pusher`. It confirms every original container ID remains unchanged. Exact replay validates the existing registration without recreating EPC.

The returned private receipt directory contains metadata backups, hashes, and existence/hash guards for both CI image pin files. The adapter never restores or rewrites those pin files. Rollback is also a plan by default:

```sh
python3 register_epc.py --rollback /opt/cleanapp/deploy/aws-deploy/epc-RECEIPT
```

Add `--execute` to stop/remove only the matching EPC container and restore the saved files. Rollback validates all original backup bytes and requires both CI pin files to match their recorded existence and hashes before stopping EPC or restoring any file. It refuses stale receipts after any pin change or later runtime metadata change. A completed rollback replay is a no-op only while every target still matches its original state and no EPC container exists; a later registration makes the old receipt stale. This completed no-op permits unrelated CI pin updates because it performs no mutations. Startup failures automatically attempt the same rollback. Database/history imports and the protected environment file are preserved.

After apply, the operator should verify sustained worker health, imported history/progress, namespace/dispatch controls, boot's plan output, canonical role capture, and a fresh verified recovery set. A successful registration alone is not evidence of an external Blockscan delivery or a verified full backup.

Local tests use temporary files and mocked Docker; no runtime starts:

```sh
python3 -m unittest discover -s platform_blueprint/deploy/prod/aws/tests -v
```
