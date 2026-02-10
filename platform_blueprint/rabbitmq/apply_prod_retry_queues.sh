#!/usr/bin/env bash
set -euo pipefail

# Creates per-queue retry exchanges and retry queues.
#
# Model:
# - On transient failure, consumers publish the message to a per-queue retry exchange:
#     exchange = "${RETRY_EXCHANGE_PREFIX}${QUEUE}"
#     routing_key = original routing key (e.g. report.raw, report.analysed, ...)
# - The retry queue "${QUEUE}.retry" holds messages for a fixed delay (TTL), then dead-letters
#   back to the main exchange with the same routing key.
#
# This avoids tight immediate requeue loops and provides bounded retry attempts (via a header),
# while keeping permanent failures routed to DLQ via existing DLX policies.
#
# Secrets-safe:
# - Does not require knowing existing RabbitMQ credentials.
# - Creates a short-lived admin user via rabbitmqctl, uses it for the management API,
#   then deletes the user.
#
# Intended to run on the prod VM.
RABBIT_CONTAINER="${RABBIT_CONTAINER:-cleanapp_rabbitmq}"
VHOST="${VHOST:-/}"
MAIN_EXCHANGE="${MAIN_EXCHANGE:-cleanapp-exchange}"

RETRY_EXCHANGE_PREFIX="${RETRY_EXCHANGE_PREFIX:-cleanapp-retry.}"
RETRY_DELAY_MS="${RETRY_DELAY_MS:-30000}"

API_BASE="${API_BASE:-http://127.0.0.1:15672/api}"
VHOST_URLENC="${VHOST_URLENC:-%2F}"

if [[ -n "${QUEUE_NAMES:-}" ]]; then
  IFS=',' read -r -a QUEUES <<<"${QUEUE_NAMES}"
else
  QUEUES=(
    "report-analysis-queue"
    "report-renderer-queue"
    "report-tags-queue"
    "twitter-reply-queue"
  )
fi

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

require curl
require python3

tmp_user="codex_retry_setup_$(date +%s)"
tmp_pass="$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')"
auth="${tmp_user}:${tmp_pass}"

cleanup() {
  sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl delete_user "${tmp_user}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[retry] creating temporary RabbitMQ admin user: ${tmp_user}"
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl add_user "${tmp_user}" "${tmp_pass}" >/dev/null
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl set_user_tags "${tmp_user}" administrator >/dev/null
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl set_permissions -p "${VHOST}" "${tmp_user}" ".*" ".*" ".*" >/dev/null

for q in "${QUEUES[@]}"; do
  retry_ex="${RETRY_EXCHANGE_PREFIX}${q}"
  retry_q="${q}.retry"

  echo "[retry] ensuring retry exchange: ${retry_ex} (topic)"
  curl -fsS -u "${auth}" \
    -H "content-type: application/json" \
    -X PUT "${API_BASE}/exchanges/${VHOST_URLENC}/${retry_ex}" \
    -d "{\"type\":\"topic\",\"durable\":true,\"auto_delete\":false,\"internal\":false,\"arguments\":{}}" \
    >/dev/null

  echo "[retry] ensuring retry queue: ${retry_q} (ttl=${RETRY_DELAY_MS}ms -> dlx=${MAIN_EXCHANGE})"
  curl -fsS -u "${auth}" \
    -H "content-type: application/json" \
    -X PUT "${API_BASE}/queues/${VHOST_URLENC}/${retry_q}" \
    -d "{\"durable\":true,\"auto_delete\":false,\"arguments\":{\"x-queue-type\":\"classic\",\"x-message-ttl\":${RETRY_DELAY_MS},\"x-dead-letter-exchange\":\"${MAIN_EXCHANGE}\"}}" \
    >/dev/null

  echo "[retry] binding ${retry_ex} -> ${retry_q} (rk=#)"
  curl -fsS -u "${auth}" \
    -H "content-type: application/json" \
    -X POST "${API_BASE}/bindings/${VHOST_URLENC}/e/${retry_ex}/q/${retry_q}" \
    -d "{\"routing_key\":\"#\",\"arguments\":{}}" \
    >/dev/null
done

echo "[retry] done"
