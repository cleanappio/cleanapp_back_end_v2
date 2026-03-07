#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/platform_blueprint/tests/ci/ingest-v1/docker-compose.yml"

dc() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

cleanup() {
  dc down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_http_200() {
  local url="$1"
  local tries="${2:-120}"
  local delay="${3:-2}"
  for _ in $(seq 1 "$tries"); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done
  return 1
}

mysql_query() {
  local sql="$1"
  dc exec -T mysql mysql -uroot -proot cleanapp -N -e "$sql"
}

json_get() {
  local expr="$1"
  python3 - "$expr" <<'PY'
import json
import sys

expr = sys.argv[1]
obj = json.loads(sys.stdin.read())
value = eval(expr, {"obj": obj})
if isinstance(value, bool):
    print("true" if value else "false")
elif value is None:
    print("")
else:
    print(value)
PY
}

echo "== bring up stack =="
dc up -d --build

echo "== wait services =="
wait_http_200 "http://localhost:18082/health" 180 2
wait_http_200 "http://localhost:18080/api/v3/health" 180 2

echo "== register cleanapp wire agent =="
reg_resp="$(
  curl -fsS --max-time 10 -H "content-type: application/json" \
    -d '{"name":"ci-cleanapp-wire","owner_type":"openclaw"}' \
    "http://localhost:18082/api/v1/agents/register"
)"
api_key="$(json_get 'obj["api_key"]' <<<"$reg_resp")"
agent_id="$(json_get 'obj["fetcher_id"]' <<<"$reg_resp")"
echo "agent_id=$agent_id"

echo "== agent me =="
me_resp="$(
  curl -fsS --max-time 10 -H "Authorization: Bearer ${api_key}" \
    "http://localhost:18082/api/v1/agents/me"
)"
me_id="$(json_get 'obj["agent_id"]' <<<"$me_resp")"
if [[ "$me_id" != "$agent_id" ]]; then
  echo "agent_id mismatch: register=$agent_id me=$me_id" >&2
  exit 1
fi

echo "== submit 1 cleanapp wire report =="
source_id="wire-src-$(date +%s)-$RANDOM"
payload="$(python3 - <<PY
import json
print(json.dumps({
  "schema_version": "cleanapp-wire.v1",
  "submission_id": "subm-ci-first",
  "source_id": "$source_id",
  "submitted_at": "2026-03-07T12:00:00Z",
  "observed_at": "2026-03-07T11:59:00Z",
  "agent": {
    "agent_id": "$agent_id",
    "agent_type": "scraper",
    "operator_type": "openclaw",
    "auth_method": "api_key_signature",
    "software_version": "1.0.0"
  },
  "provenance": {
    "generation_method": "llm_assisted_extraction",
    "upstream_sources": [{"kind": "url", "value": "https://example.com/posts/42"}]
  },
  "report": {
    "domain": "digital",
    "problem_type": "spam",
    "title": "CI CleanApp Wire report",
    "description": "CI generated report to validate CleanApp Wire intake.",
    "confidence": 0.84,
    "location": {"lat": 47.36, "lng": 8.55},
    "evidence_bundle": [{"type": "url", "uri": "https://example.com/posts/42"}]
  },
  "delivery": {"requested_lane": "auto"}
}))
PY
)"
submit_resp="$(
  curl -fsS --max-time 20 -H "content-type: application/json" -H "Authorization: Bearer ${api_key}" \
    -d "$payload" \
    "http://localhost:18082/api/v1/agent-reports:submit"
)"
receipt_id="$(json_get 'obj["receipt_id"]' <<<"$submit_resp")"
report_id="$(json_get 'obj["report_id"]' <<<"$submit_resp")"
lane="$(json_get 'obj["lane"]' <<<"$submit_resp")"
status="$(json_get 'obj["status"]' <<<"$submit_resp")"
if [[ "$lane" != "quarantine" || "$status" != "quarantined" ]]; then
  echo "expected quarantine receipt, got lane=$lane status=$status" >&2
  exit 1
fi
echo "receipt_id=$receipt_id report_id=$report_id"

echo "== verify receipt lookup =="
receipt_resp="$(
  curl -fsS --max-time 10 -H "Authorization: Bearer ${api_key}" \
    "http://localhost:18082/api/v1/agent-reports/receipts/${receipt_id}"
)"
lookup_receipt_id="$(json_get 'obj["receipt_id"]' <<<"$receipt_resp")"
if [[ "$lookup_receipt_id" != "$receipt_id" ]]; then
  echo "receipt lookup mismatch: expected=$receipt_id got=$lookup_receipt_id" >&2
  exit 1
fi

echo "== verify source status lookup =="
status_resp="$(
  curl -fsS --max-time 10 -H "Authorization: Bearer ${api_key}" \
    "http://localhost:18082/api/v1/agent-reports/status/${source_id}"
)"
status_lane="$(json_get 'obj["lane"]' <<<"$status_resp")"
if [[ "$status_lane" != "quarantine" ]]; then
  echo "expected status lane quarantine, got $status_lane" >&2
  exit 1
fi

echo "== verify raw + receipt persistence =="
wire_count="$(mysql_query "SELECT COUNT(*) FROM wire_submissions_raw WHERE fetcher_id='${agent_id}' AND source_id='${source_id}';")"
receipt_count="$(mysql_query "SELECT COUNT(*) FROM wire_submission_receipts WHERE fetcher_id='${agent_id}' AND source_id='${source_id}';")"
if [[ "${wire_count:-0}" != "1" || "${receipt_count:-0}" != "1" ]]; then
  echo "wire persistence missing: submissions=${wire_count:-0} receipts=${receipt_count:-0}" >&2
  exit 1
fi

echo "== verify report_raw visibility=shadow =="
vis="$(mysql_query "SELECT visibility FROM report_raw WHERE report_seq=${report_id};")"
if [[ "${vis:-}" != "shadow" ]]; then
  echo "expected report_raw.visibility=shadow, got: ${vis:-<empty>}" >&2
  exit 1
fi

echo "== wait for report_analysis row =="
for _ in $(seq 1 120); do
  n="$(mysql_query "SELECT COUNT(*) FROM report_analysis WHERE seq=${report_id};")"
  if [[ "${n:-0}" != "0" ]]; then
    break
  fi
  sleep 2
done
n="$(mysql_query "SELECT COUNT(*) FROM report_analysis WHERE seq=${report_id};")"
if [[ "${n:-0}" == "0" ]]; then
  echo "analysis did not complete in time (seq=$report_id)" >&2
  dc logs --no-color analyzer || true
  exit 1
fi

echo "== idempotent replay should not conflict on transport-field changes =="
replay_payload="$(python3 - <<PY
import json
print(json.dumps({
  "schema_version": "cleanapp-wire.v1",
  "submission_id": "subm-ci-retry",
  "source_id": "$source_id",
  "submitted_at": "2026-03-07T12:05:00Z",
  "observed_at": "2026-03-07T11:59:00Z",
  "agent": {
    "agent_id": "$agent_id",
    "agent_type": "scraper",
    "operator_type": "openclaw",
    "auth_method": "api_key_signature",
    "software_version": "1.0.0"
  },
  "provenance": {
    "generation_method": "llm_assisted_extraction",
    "upstream_sources": [{"kind": "url", "value": "https://example.com/posts/42"}]
  },
  "report": {
    "domain": "digital",
    "problem_type": "spam",
    "title": "CI CleanApp Wire report",
    "description": "CI generated report to validate CleanApp Wire intake.",
    "confidence": 0.84,
    "location": {"lat": 47.36, "lng": 8.55},
    "evidence_bundle": [{"type": "url", "uri": "https://example.com/posts/42"}]
  },
  "delivery": {"requested_lane": "auto"}
}))
PY
)"
replay_resp="$(
  curl -fsS --max-time 20 -H "content-type: application/json" -H "Authorization: Bearer ${api_key}" \
    -d "$replay_payload" \
    "http://localhost:18082/api/v1/agent-reports:submit"
)"
replay_flag="$(json_get 'obj["idempotency_replay"]' <<<"$replay_resp")"
replay_report_id="$(json_get 'obj["report_id"]' <<<"$replay_resp")"
if [[ "$replay_flag" != "true" || "$replay_report_id" != "$report_id" ]]; then
  echo "expected idempotent replay on retry, got replay=$replay_flag report_id=$replay_report_id" >&2
  exit 1
fi

echo "== reputation endpoint =="
rep_resp="$(
  curl -fsS --max-time 10 -H "Authorization: Bearer ${api_key}" \
    "http://localhost:18082/api/v1/agents/reputation/${agent_id}"
)"
rep_agent="$(json_get 'obj["agent_id"]' <<<"$rep_resp")"
if [[ "$rep_agent" != "$agent_id" ]]; then
  echo "reputation lookup mismatch: expected=$agent_id got=$rep_agent" >&2
  exit 1
fi

echo "OK"
