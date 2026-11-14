# Observability (Prometheus + Alertmanager)

This blueprint installs a minimal Prometheus stack on the prod VM, bound to `127.0.0.1` only.

Goals:
- Provide metrics visibility for key services (start with `report-analyze-pipeline`).
- Add alert routing via Alertmanager webhook.
- Alert on missing RabbitMQ consumers/backlog via `rabbitmq_exporter`.
- Scrape both bulk and human analyzers; alert on unavailable/disconnected human
  workers, missing human queues, and work held without successful completions.
- Alert when the machine analyzer scrape target is down for 2 minutes, even if
  the RabbitMQ exporter cannot report queue depth or consumer counts.
- Alert on a waiting agent report in `report-analysis-queue` with zero or missing
  consumers for 2 minutes. A separate warning covers a visible queue whose ready
  or consumer gauge is omitted, so absent telemetry is not treated as zero work.
- The VM watchdog also reads human queue ready/unacknowledged/consumer counts and
  detects persistent backlog. Some exporter versions omit these gauges, so the
  passive watchdog is required alongside Prometheus.

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

The queue rules use the deployed kbudde v1.0.0 exporter schema, not RabbitMQ's
native Prometheus plugin. Its [queue gauges and label definitions](https://github.com/kbudde/rabbitmq_exporter/blob/1859734d4197cd752f7de06d3a8eb49f13431368/exporter_queue.go#L16-L32)
define `rabbitmq_queue_messages_ready` and `rabbitmq_queue_consumers` with `queue`,
`vhost`, and `cluster` labels; its [exporter fixture](https://github.com/kbudde/rabbitmq_exporter/blob/1859734d4197cd752f7de06d3a8eb49f13431368/exporter_test.go#L110)
checks the emitted ready-message metric. The collector emits a gauge only when
the management API includes that field. If management statistics are disabled,
the telemetry warning can fire while the backlog alarm cannot establish a count.

Run the rule fixtures with `promtool` v2.52.0 (matching the Prometheus image):

```sh
promtool check rules platform_blueprint/ops/observability/prometheus/alerts.yml
alert_test_dir=$(mktemp -d)
cp platform_blueprint/ops/observability/prometheus/alerts.yml "$alert_test_dir/alerts.yml"
cp platform_blueprint/tests/ci/ops/human_alerts.test.yml "$alert_test_dir/human_alerts.test.yml"
promtool test rules "$alert_test_dir/human_alerts.test.yml"
rm -r "$alert_test_dir"
```
