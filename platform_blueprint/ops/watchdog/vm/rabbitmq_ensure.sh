#!/usr/bin/env bash
set -euo pipefail

RABBIT_CONTAINER="${RABBIT_CONTAINER:-cleanapp_rabbitmq}"
API_BASE="${API_BASE:-http://127.0.0.1:15672/api}"

get_rabbit_env() {
  local k="$1"
  sudo docker inspect "${RABBIT_CONTAINER}" --format "{{range .Config.Env}}{{println .}}{{end}}" \
    | awk -F= -v key="$k" '$1==key{print substr($0, index($0,"=")+1); exit}'
}

USER="$(get_rabbit_env RABBITMQ_DEFAULT_USER)"
PASS="$(get_rabbit_env RABBITMQ_DEFAULT_PASS)"

if [[ -z "${USER}" || -z "${PASS}" ]]; then
  echo "rabbitmq_ensure: could not read rabbit credentials from container env" >&2
  exit 1
fi

curl_json() {
  local method="$1"
  local url="$2"
  local data="${3:-}"
  if [[ -n "${data}" ]]; then
    curl -fsS -u "${USER}:${PASS}" -H "content-type: application/json" -X "${method}" "${url}" -d "${data}"
  else
    curl -fsS -u "${USER}:${PASS}" -H "content-type: application/json" -X "${method}" "${url}"
  fi
}

exists() {
  local url="$1"
  curl -fsS -u "${USER}:${PASS}" "${url}" >/dev/null 2>&1
}

ensure_exchange() {
  local name="$1"
  local typ="$2"
  local url="${API_BASE}/exchanges/%2F/${name}"
  if exists "${url}"; then
    return 0
  fi
  echo "rabbitmq_ensure: creating exchange=${name} type=${typ}"
  curl_json PUT "${url}" "{\"type\":\"${typ}\",\"durable\":true,\"auto_delete\":false,\"internal\":false,\"arguments\":{}}" >/dev/null
}

ensure_queue() {
  local name="$1"
  local args_json="${2:-{}}"  # JSON object
  local url="${API_BASE}/queues/%2F/${name}"
  if exists "${url}"; then
    return 0
  fi
  echo "rabbitmq_ensure: creating queue=${name}"
  curl_json PUT "${url}" "{\"durable\":true,\"auto_delete\":false,\"arguments\":${args_json}}" >/dev/null
}

ensure_binding() {
  local exchange="$1"
  local queue="$2"
  local routing_key="$3"

  local list_url="${API_BASE}/bindings/%2F/e/${exchange}/q/${queue}"
  if curl -fsS -u "${USER}:${PASS}" "${list_url}" | grep -q "\"routing_key\":\"${routing_key}\""; then
    return 0
  fi

  echo "rabbitmq_ensure: creating binding exchange=${exchange} queue=${queue} routing_key=${routing_key}"
  local url="${API_BASE}/bindings/%2F/e/${exchange}/q/${queue}"
  curl_json POST "${url}" "{\"routing_key\":\"${routing_key}\"}" >/dev/null
}

ensure_policy_dlx() {
  local policy_name="$1"
  local queue_name="$2"
  local dlq_name="$3"

  local url="${API_BASE}/policies/%2F/${policy_name}"
  if exists "${url}"; then
    return 0
  fi

  echo "rabbitmq_ensure: creating policy=${policy_name} for queue=${queue_name} -> dlq=${dlq_name}"
  curl_json PUT "${url}" \
    "{\"pattern\":\"^${queue_name}$\",\"apply-to\":\"queues\",\"definition\":{\"dead-letter-exchange\":\"cleanapp-dlx\",\"dead-letter-routing-key\":\"${dlq_name}\"},\"priority\":0}" >/dev/null
}

ensure_retry_for_queue() {
  local q="$1"
  local retry_exchange="cleanapp-retry.${q}"
  local retry_queue="${q}.retry"

  ensure_exchange "${retry_exchange}" "topic"
  ensure_queue "${retry_queue}" "{\"x-message-ttl\":30000,\"x-dead-letter-exchange\":\"cleanapp-exchange\",\"x-queue-type\":\"classic\"}"
  ensure_binding "${retry_exchange}" "${retry_queue}" "#"
}

# Core exchanges
ensure_exchange "cleanapp-exchange" "direct"
ensure_exchange "cleanapp-dlx" "direct"

# Queues + DLQs + policies + bindings
ensure_queue "report-analysis-queue" "{\"x-queue-type\":\"classic\"}"
ensure_queue "report-analysis-queue.dlq" "{\"x-queue-type\":\"classic\"}"
ensure_policy_dlx "dlx-report-analysis-queue" "report-analysis-queue" "report-analysis-queue.dlq"
ensure_binding "cleanapp-exchange" "report-analysis-queue" "report.raw"
ensure_retry_for_queue "report-analysis-queue"

ensure_queue "report-tags-queue" "{\"x-queue-type\":\"classic\"}"
ensure_queue "report-tags-queue.dlq" "{\"x-queue-type\":\"classic\"}"
ensure_policy_dlx "dlx-report-tags-queue" "report-tags-queue" "report-tags-queue.dlq"
ensure_binding "cleanapp-exchange" "report-tags-queue" "report.raw"
ensure_retry_for_queue "report-tags-queue"

ensure_queue "report-renderer-queue" "{\"x-queue-type\":\"classic\"}"
ensure_queue "report-renderer-queue.dlq" "{\"x-queue-type\":\"classic\"}"
ensure_policy_dlx "dlx-report-renderer-queue" "report-renderer-queue" "report-renderer-queue.dlq"
ensure_binding "cleanapp-exchange" "report-renderer-queue" "report.analysed"
ensure_retry_for_queue "report-renderer-queue"

ensure_queue "twitter-reply-queue" "{\"x-queue-type\":\"classic\"}"
ensure_queue "twitter-reply-queue.dlq" "{\"x-queue-type\":\"classic\"}"
ensure_policy_dlx "dlx-twitter-reply-queue" "twitter-reply-queue" "twitter-reply-queue.dlq"
ensure_binding "cleanapp-exchange" "twitter-reply-queue" "twitter.reply"
ensure_retry_for_queue "twitter-reply-queue"

