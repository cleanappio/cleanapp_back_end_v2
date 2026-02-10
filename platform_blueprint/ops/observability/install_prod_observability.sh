#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
REMOTE_DIR="${REMOTE_DIR:-/home/deployer/cleanapp_observability}"

echo "Installing CleanApp observability stack to $HOST:$REMOTE_DIR"

ssh "$HOST" "set -euo pipefail; mkdir -p \"$REMOTE_DIR/prometheus\""

ssh "$HOST" "cat > \"$REMOTE_DIR/docker-compose.observability.yml\"" < platform_blueprint/ops/observability/docker-compose.observability.yml
ssh "$HOST" "cat > \"$REMOTE_DIR/prometheus/prometheus.yml\"" < platform_blueprint/ops/observability/prometheus/prometheus.yml
ssh "$HOST" "cat > \"$REMOTE_DIR/prometheus/alerts.yml\"" < platform_blueprint/ops/observability/prometheus/alerts.yml

ssh "$HOST" "set -euo pipefail; cd \"$REMOTE_DIR\"; sudo -n docker compose -f docker-compose.observability.yml pull; sudo -n docker compose -f docker-compose.observability.yml up -d; sudo -n docker ps --filter name=cleanapp_prometheus --format \"table {{.Names}}\\t{{.Status}}\\t{{.Ports}}\""

echo "Done."
