# Observability (Prometheus + Alertmanager)

This blueprint installs a minimal Prometheus stack on the prod VM, bound to `127.0.0.1` only.

Goals:
- Provide metrics visibility for key services (start with `report-analyze-pipeline`).
- Add alert routing via Alertmanager webhook.
- Alert on missing RabbitMQ consumers/backlog via `rabbitmq_exporter`.

Install:
- `HOST=deployer@<prod-ip> platform_blueprint/ops/observability/install_prod_observability.sh`

Access (from the VM):
- Prometheus UI: `http://127.0.0.1:9090`
- Alertmanager UI: `http://127.0.0.1:9194`
- RabbitMQ exporter metrics: `http://127.0.0.1:9419/metrics`

Optional outbound notifications:
- Set env var on VM before install/restart:
  - `export CLEANAPP_ALERT_WEBHOOK_URL='https://<your-webhook-endpoint>'`
- If unset, Alertmanager uses a localhost no-op URL and no external notification is sent.

Notes:
- `install_prod_observability.sh` reads RabbitMQ credentials directly from the running
  `cleanapp_rabbitmq` container and writes `rabbitmq-exporter.env` on the VM.
- Watchdog can share the same webhook by setting `CLEANAPP_ALERT_WEBHOOK_URL`; it
  will use that as fallback if `CLEANAPP_WATCHDOG_WEBHOOK_URL` is not set.
