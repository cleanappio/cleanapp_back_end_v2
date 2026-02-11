#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
REMOTE_DIR="${REMOTE_DIR:-/home/deployer/cleanapp_observability}"
WEBHOOK_URL="${CLEANAPP_ALERT_WEBHOOK_URL:-http://127.0.0.1:9/cleanapp-alert}"

echo "Installing CleanApp observability stack to $HOST:$REMOTE_DIR"

ssh "$HOST" "set -euo pipefail; mkdir -p \"$REMOTE_DIR/prometheus\" \"$REMOTE_DIR/alertmanager\""

ssh "$HOST" "cat > \"$REMOTE_DIR/docker-compose.observability.yml\"" < platform_blueprint/ops/observability/docker-compose.observability.yml
ssh "$HOST" "cat > \"$REMOTE_DIR/prometheus/prometheus.yml\"" < platform_blueprint/ops/observability/prometheus/prometheus.yml
ssh "$HOST" "cat > \"$REMOTE_DIR/prometheus/alerts.yml\"" < platform_blueprint/ops/observability/prometheus/alerts.yml

tmp_alert_cfg="$(mktemp)"
trap 'rm -f "$tmp_alert_cfg"' EXIT
python3 - "$WEBHOOK_URL" <<'PY' >"$tmp_alert_cfg"
import pathlib
import sys

webhook_url = sys.argv[1]
template = pathlib.Path("platform_blueprint/ops/observability/alertmanager/alertmanager.yml").read_text(encoding="utf-8")
print(template.replace("__CLEANAPP_ALERT_WEBHOOK_URL__", webhook_url), end="")
PY
ssh "$HOST" "cat > \"$REMOTE_DIR/alertmanager/alertmanager.yml\"" < "$tmp_alert_cfg"

ssh "$HOST" "set -euo pipefail; cd \"$REMOTE_DIR\"; sudo -n docker compose -f docker-compose.observability.yml pull; sudo -n docker compose -f docker-compose.observability.yml up -d; curl -fsS -X POST --max-time 5 http://127.0.0.1:9090/-/reload >/dev/null || true; sudo -n docker ps --filter name=cleanapp_prometheus --filter name=cleanapp_alertmanager --format \"table {{.Names}}\\t{{.Status}}\\t{{.Ports}}\""

echo "Done."
