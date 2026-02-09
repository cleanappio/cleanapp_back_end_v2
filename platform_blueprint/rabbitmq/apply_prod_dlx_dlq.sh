#!/usr/bin/env bash
set -euo pipefail

# Creates a DLX + per-queue DLQs and applies dead-letter policies.
#
# Secrets-safe:
# - Does not require knowing existing RabbitMQ credentials.
# - Creates a short-lived admin user via rabbitmqctl, uses it for the management API,
#   then deletes the user.
#
# Intended to run on the prod VM.

RABBIT_CONTAINER="${RABBIT_CONTAINER:-cleanapp_rabbitmq}"
VHOST="${VHOST:-/}"
DLX_EXCHANGE="${DLX_EXCHANGE:-cleanapp-dlx}"

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

tmp_user="codex_dlx_setup_$(date +%s)"
tmp_pass="$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')"
auth="${tmp_user}:${tmp_pass}"

cleanup() {
  # Best-effort cleanup of the temporary user.
  sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl delete_user "${tmp_user}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[dlq] creating temporary RabbitMQ admin user: ${tmp_user}"
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl add_user "${tmp_user}" "${tmp_pass}" >/dev/null
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl set_user_tags "${tmp_user}" administrator >/dev/null
sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl set_permissions -p "${VHOST}" "${tmp_user}" ".*" ".*" ".*" >/dev/null

echo "[dlq] ensuring DLX exchange: ${DLX_EXCHANGE}"
curl -fsS -u "${auth}" \
  -H "content-type: application/json" \
  -X PUT "${API_BASE}/exchanges/${VHOST_URLENC}/${DLX_EXCHANGE}" \
  -d "{\"type\":\"direct\",\"durable\":true,\"auto_delete\":false,\"internal\":false,\"arguments\":{}}" \
  >/dev/null

for q in "${QUEUES[@]}"; do
  dlq="${q}.dlq"
  rk="${dlq}"

  echo "[dlq] ensuring DLQ queue: ${dlq}"
  curl -fsS -u "${auth}" \
    -H "content-type: application/json" \
    -X PUT "${API_BASE}/queues/${VHOST_URLENC}/${dlq}" \
    -d "{\"durable\":true,\"auto_delete\":false,\"arguments\":{\"x-queue-type\":\"classic\"}}" \
    >/dev/null

  echo "[dlq] binding ${DLX_EXCHANGE} -> ${dlq} (rk=${rk})"
  curl -fsS -u "${auth}" \
    -H "content-type: application/json" \
    -X POST "${API_BASE}/bindings/${VHOST_URLENC}/e/${DLX_EXCHANGE}/q/${dlq}" \
    -d "{\"routing_key\":\"${rk}\",\"arguments\":{}}" \
    >/dev/null

  echo "[dlq] applying policy for ${q} -> ${DLX_EXCHANGE} (${rk})"
  sudo docker exec "${RABBIT_CONTAINER}" rabbitmqctl set_policy \
    -p "${VHOST}" \
    "dlx-${q}" \
    "^${q}$" \
    "{\"dead-letter-exchange\":\"${DLX_EXCHANGE}\",\"dead-letter-routing-key\":\"${rk}\"}" \
    --apply-to queues \
    >/dev/null
done

echo "[dlq] done"
