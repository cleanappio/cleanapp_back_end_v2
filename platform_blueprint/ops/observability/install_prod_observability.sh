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

remote_dir_q="$(printf "%q" "$REMOTE_DIR")"
ssh "$HOST" "REMOTE_DIR=${remote_dir_q} bash -s" <<'__REMOTE__'
set -euo pipefail
rabbit_user="$(sudo -n docker inspect cleanapp_rabbitmq --format '{{range .Config.Env}}{{println .}}{{end}}' | awk -F= '$1=="RABBITMQ_DEFAULT_USER"{print substr($0,index($0,"=")+1); exit}')"
rabbit_pass="$(sudo -n docker inspect cleanapp_rabbitmq --format '{{range .Config.Env}}{{println .}}{{end}}' | awk -F= '$1=="RABBITMQ_DEFAULT_PASS"{print substr($0,index($0,"=")+1); exit}')"
if [[ -z "${rabbit_user}" || -z "${rabbit_pass}" ]]; then
  echo "failed to detect RabbitMQ credentials from cleanapp_rabbitmq"
  exit 1
fi
cat >"${REMOTE_DIR}/rabbitmq-exporter.env" <<EOF
RABBIT_URL=http://cleanapp_rabbitmq:15672
RABBIT_USER=${rabbit_user}
RABBIT_PASSWORD=${rabbit_pass}
PUBLISH_PORT=9419
RABBIT_EXPORTERS=overview,queue
EOF
chmod 600 "${REMOTE_DIR}/rabbitmq-exporter.env"
cd "${REMOTE_DIR}"
sudo -n docker compose -f docker-compose.observability.yml pull
sudo -n docker compose -f docker-compose.observability.yml up -d
curl -fsS -X POST --max-time 5 http://127.0.0.1:9090/-/reload >/dev/null || true
sudo -n docker ps --filter name=cleanapp_prometheus --filter name=cleanapp_alertmanager --filter name=cleanapp_rabbitmq_exporter --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
__REMOTE__

echo "Done."
