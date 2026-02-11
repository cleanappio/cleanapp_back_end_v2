# Prod Watchdog (Smoke + Self-Heal)

Goal: detect and auto-remediate the most common "pipeline is broken but containers are running" failures, especially:
- RabbitMQ broker restart causes queues/bindings/policies to be missing
- Consumers don't reconnect (or start) so `report.raw` stops being analyzed

This watchdog is designed to run *on the prod VM* as `deployer` via `cron`.

What it does each run:
1. Best-effort ensure RabbitMQ infra exists (exchanges/queues/bindings + DLX/DLQ + retry queues)
2. Run a local smoke:
   - core localhost health endpoints
   - RabbitMQ must-have bindings
   - report-analysis/report-tags/report-renderer consumers must be present
3. Verify backup freshness from `/home/deployer/backups/backup.log` (fails if too old)
4. Write logs and a small status file.

Alerting:
- Optional webhook support (no secrets committed). If configured, the watchdog will POST failures.
  - `CLEANAPP_WATCHDOG_WEBHOOK_URL=<https://...>` in `~/cleanapp_watchdog/secrets.env`
  - Fallback supported: `CLEANAPP_ALERT_WEBHOOK_URL=<https://...>` (shared with Alertmanager)

Backup freshness defaults:
- Max acceptable age: `30h` (`BACKUP_MAX_AGE_HOURS`)
- Log source: `/home/deployer/backups/backup.log` (`BACKUP_LOG_FILE`)

Files installed on VM:
- `~/cleanapp_watchdog/run.sh`
- `~/cleanapp_watchdog/rabbitmq_ensure.sh`
- `~/cleanapp_watchdog/smoke_local.sh`
- `~/cleanapp_watchdog/backup_freshness.sh`
- `~/cleanapp_watchdog/secrets.env` (optional, not created by default)
- `~/cleanapp_watchdog/watchdog.log`
- `~/cleanapp_watchdog/status.json`

Install/uninstall:
- `platform_blueprint/ops/watchdog/install_prod_watchdog.sh`
- `platform_blueprint/ops/watchdog/uninstall_prod_watchdog.sh`
