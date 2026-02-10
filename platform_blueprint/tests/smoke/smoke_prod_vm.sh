#!/usr/bin/env bash
set -euo pipefail

# Smoke tests from inside the prod VM:
# - hit localhost-bound container endpoints (bypasses DNS + external routing)
# - validate RabbitMQ invariants (queues, DLX/DLQ policies)
#
# Run from your laptop:
#   HOST=deployer@<prod-vm> ./platform_blueprint/tests/smoke/smoke_prod_vm.sh
#
# Optional:
#   RUN_SLOW=1  (includes slower v4 endpoints that can take ~30-60s)

HOST="${HOST:-}"
RUN_SLOW="${RUN_SLOW:-0}"
VM_HTTP_TIMEOUT="${VM_HTTP_TIMEOUT:-10}"
SLOW_TIMEOUT="${SLOW_TIMEOUT:-70}"

if [[ -z "${HOST}" ]]; then
  echo "usage: HOST=deployer@<prod-vm> $0" >&2
  exit 2
fi

echo "[smoke] prod vm: ${HOST}"

ssh "${HOST}" "RUN_SLOW=${RUN_SLOW} VM_HTTP_TIMEOUT=${VM_HTTP_TIMEOUT} SLOW_TIMEOUT=${SLOW_TIMEOUT} bash -lc '
set -euo pipefail

req() {
  local url=\"\$1\"
  local timeout=\"\$2\"
  local code
  code=\"\$(curl -sS -o /dev/null -w \"%{http_code}\" --max-time \"\${timeout}\" \"\${url}\" || true)\"
  printf \"%s\t%s\n\" \"\${url}\" \"\${code}\"
  [[ \"\${code}\" == \"200\" ]]
}

echo \"== localhost services ==\"
req \"http://127.0.0.1:9093/health\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9093/version\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9098/health\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9098/version\" \"\${VM_HTTP_TIMEOUT}\"

echo
echo \"== report-listener-v4 (localhost) ==\"
req \"http://127.0.0.1:9097/api/v4/health\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9097/api/v4/version\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9097/api/v4/openapi.json\" \"\${VM_HTTP_TIMEOUT}\"

echo
echo \"== v4 contract checks (quick) ==\"
req \"http://127.0.0.1:9097/api/v4/brands/summary?classification=trash&lang=en\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9097/api/v4/reports/by-brand?brand_name=CleanApp&n=1\" \"\${VM_HTTP_TIMEOUT}\"

echo
echo \"== report-analyze-pipeline (localhost) ==\"
req \"http://127.0.0.1:9082/version\" \"\${VM_HTTP_TIMEOUT}\"
req \"http://127.0.0.1:9082/api/v3/health\" \"\${VM_HTTP_TIMEOUT}\"
curl -fsS --max-time \"\${VM_HTTP_TIMEOUT}\" http://127.0.0.1:9082/api/v3/health | grep -F \"\\\"rabbitmq_connected\\\":true\" >/dev/null

if [[ \"\${RUN_SLOW}\" == \"1\" ]]; then
  echo
  echo \"== v4 contract checks (slow) ==\"
  req \"http://127.0.0.1:9097/api/v4/reports/points?classification=trash\" \"\${SLOW_TIMEOUT}\"
fi

echo
echo \"== rabbitmq: exchanges ==\"
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_exchanges name type | egrep \"^(name|cleanapp-exchange|cleanapp-dlx)\\b\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_exchanges name type | egrep \"^(name|cleanapp-exchange|cleanapp-dlx)\\b\"

echo
echo \"== rabbitmq: queues (incl dlq) ==\"
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name messages_ready messages_unacknowledged consumers \\
  | egrep \"^(name|(report-(analysis|renderer|tags)-queue|twitter-reply-queue)(\\.dlq)?\\b)\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name messages_ready messages_unacknowledged consumers \\
  | egrep \"^(name|(report-(analysis|renderer|tags)-queue|twitter-reply-queue)(\\.dlq)?\\b)\"

echo
echo \"== rabbitmq: must-have bindings ==\"
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_bindings source_name destination_name destination_kind routing_key \\
  | egrep \"^cleanapp-exchange\\s+report-analysis-queue\\s+queue\\s+report\\.raw$\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_bindings source_name destination_name destination_kind routing_key \\
  | egrep \"^cleanapp-exchange\\s+report-analysis-queue\\s+queue\\s+report\\.raw$\"

echo
echo \"== rabbitmq: report-analysis consumer must be present ==\"
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name consumers --no-table-headers \\
  | egrep \"^report-analysis-queue[[:space:]]+[1-9]\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name consumers --no-table-headers \\
  | egrep \"^(report-analysis-queue|report-renderer-queue|report-tags-queue)[[:space:]]+\"

echo
echo \"== rabbitmq: dlx policies ==\"
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_policies -p / | grep -F \"dlx-report-tags-queue\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_policies -p / | grep -F \"dlx-report-renderer-queue\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_policies -p / | grep -F \"dlx-twitter-reply-queue\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_policies -p / | grep -F \"dlx-report-analysis-queue\" >/dev/null
sudo docker exec cleanapp_rabbitmq rabbitmqctl list_policies -p / | sed -n \"1,120p\"

echo
echo \"[smoke] OK\"
'"
