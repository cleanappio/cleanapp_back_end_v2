#!/usr/bin/env bash
set -euo pipefail

VM_HTTP_TIMEOUT="${VM_HTTP_TIMEOUT:-10}"

req() {
  local url="$1"
  local timeout="$2"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" --max-time "${timeout}" "${url}" || true)"
  printf "%s\t%s\n" "${url}" "${code}"
  [[ "${code}" == "200" ]]
}

echo "== localhost services =="
req "http://127.0.0.1:9093/health" "${VM_HTTP_TIMEOUT}"
req "http://127.0.0.1:9098/health" "${VM_HTTP_TIMEOUT}"
req "http://127.0.0.1:9097/api/v4/health" "${VM_HTTP_TIMEOUT}"
req "http://127.0.0.1:9082/api/v3/health" "${VM_HTTP_TIMEOUT}"
curl -fsS --max-time "${VM_HTTP_TIMEOUT}" http://127.0.0.1:9082/api/v3/health | grep -F '"rabbitmq_connected":true' >/dev/null

echo
echo "== rabbitmq must-have binding =="
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_bindings source_name destination_name destination_kind routing_key \
  | egrep "^cleanapp-exchange\\s+report-analysis-queue\\s+queue\\s+report\\.raw$" >/dev/null

echo
echo "== rabbitmq report-analysis consumer must be present =="
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name consumers --no-table-headers \
  | egrep "^report-analysis-queue[[:space:]]+[1-9]" >/dev/null

echo
echo "== rabbitmq report-tags consumer must be present =="
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name consumers --no-table-headers \
  | egrep "^report-tags-queue[[:space:]]+[1-9]" >/dev/null

echo
echo "[watchdog smoke] OK"
