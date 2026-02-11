# Observability (Prometheus + Alertmanager)

This blueprint installs a minimal Prometheus stack on the prod VM, bound to `127.0.0.1` only.

Goals:
- Provide metrics visibility for key services (start with `report-analyze-pipeline`).
- Add alert routing via Alertmanager webhook.

Install:
- `HOST=deployer@<prod-ip> platform_blueprint/ops/observability/install_prod_observability.sh`

Access (from the VM):
- Prometheus UI: `http://127.0.0.1:9090`
- Alertmanager UI: `http://127.0.0.1:9194`

Optional outbound notifications:
- Set env var on VM before install/restart:
  - `export CLEANAPP_ALERT_WEBHOOK_URL='https://<your-webhook-endpoint>'`
- If unset, Alertmanager uses a localhost no-op URL and no external notification is sent.
