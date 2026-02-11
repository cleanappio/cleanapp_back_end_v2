# CleanApp Optimization Status (Rolling)

This is the rolling task/status tracker for the “big upgrade push” workstream.
Last updated: 2026-02-11 (UTC).

## 1) Network Hardening + Surface Area Reduction

Status: **Mostly complete**

Done:
- Internal prod ports are bound to `127.0.0.1` for most backend services.
- Removed unused world-open firewall rules (`allow-3000`, `allow-8090`, `allow-8091`).
- Removed matching `allow-*` instance tags from dev/prod2.

Remaining:
- Final review/closure plan for legacy `:8080` exposure path after client migration.

## 2) Deterministic Deploys (Digest-Pinned)

Status: **Complete (operationalized)**

Done:
- Prod deploy blueprint is captured/redacted under `platform_blueprint/deploy/prod/`.
- VM helper in place and used: `platform_blueprint/deploy/prod/vm/deploy_with_digests.sh`.
- `docker-compose.digests.current.yml` on prod is valid YAML (no escaped newline artifact) and passes `docker compose config`.
- Version drift closed for key services via pinned rollout (`/version` endpoints aligned by git SHA).

## 3) RabbitMQ Pipeline Reliability

Status: **Complete (core path)**

Done:
- Rust consumers: bounded concurrency + ack-after-success + reconnect hardening.
- Go consumers: same hardening applied on active paths.
- DLQ + retry topology present in prod (`cleanapp-dlx`, `*.dlq`, `*.retry`).
- Analyzer reconnect and watchdog self-heal prevent silent post-restart pipeline stalls.

## 4) Observability -> Alerting

Status: **In progress (strong partial)**

Done:
- Prometheus + Alertmanager installed on prod (localhost-only).
- Analyzer `/metrics` live and scraped.
- RabbitMQ exporter added and scraped.
- Alert rules active for analyzer disconnect and queue-missing/retry-surge signals.
- Watchdog now supports shared webhook fallback (`CLEANAPP_ALERT_WEBHOOK_URL`).

Remaining:
- Wire real external webhook destination in prod (`CLEANAPP_ALERT_WEBHOOK_URL`) and test end-to-end delivery with a synthetic alert.

## 5) Integration Harness / Regression Gate

Status: **In progress (advanced)**

Done:
- Analyzer golden-path CI workflow is passing.
- New full pipeline CI workflow added:
  - `platform_blueprint/tests/ci/pipeline/run.sh`
  - `.github/workflows/pipeline_regression.yml`
  - Validates analysis + tags + renderer side effects and RabbitMQ restart resilience.

Remaining:
- Keep `pipeline-regression` green on `main` and tune runtime/flakiness as needed.

## 6) Backup Hardening + Restore Confidence

Status: **Mostly complete**

Done:
- PR #115 merged (backup script + schedule + metadata + docs).
- Daily backup cron active on prod (`/home/deployer/backup.sh -e prod`).
- Watchdog verifies backup freshness from `/home/deployer/backups/backup.log`.
- Restore drill script improved for realistic online-backup drift tolerance:
  - `platform_blueprint/ops/db_backup/restore_drill_prod_vm.sh`
  - `ROW_COUNT_TOLERANCE_PCT` (default `0.2%`)
- Restore drill result captured:
  - `xray/prod/2026-02-11/restore_drill_result.md`

Remaining:
- Optional: run another full timed drill during a low-write window to reduce count drift even further.
