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

rabbit_json() {
  local method="$1"
  local url="$2"
  local data="${3:-}"
  local user="${RABBITMQ_MGMT_USER:-guest}"
  local pass="${RABBITMQ_MGMT_PASSWORD:-guest}"
  local netrc="${TMPDIR:-/tmp}/.rmq_netrc_$$"
  printf "machine localhost login %s password %s\\n" "$user" "$pass" >"$netrc"
  chmod 600 "$netrc"
  if [[ -n "$data" ]]; then
    curl -fsS --max-time 10 --netrc-file "$netrc" -H "content-type: application/json" -X "$method" "$url" -d "$data"
  else
    curl -fsS --max-time 10 --netrc-file "$netrc" -H "content-type: application/json" -X "$method" "$url"
  fi
}

mysql_query() {
  local sql="$1"
  dc exec -T mysql mysql -uroot -proot cleanapp -N -e "$sql"
}

echo "== bring up stack =="
dc up -d --build

echo "== wait services =="
wait_http_200 "http://localhost:18082/health" 180 2
wait_http_200 "http://localhost:18080/api/v3/health" 180 2

echo "== register fetcher =="
reg_resp="$(
  curl -fsS --max-time 10 -H "content-type: application/json" \
    -d '{"name":"ci-ingest-v1","owner_type":"openclaw"}' \
    "http://localhost:18082/v1/fetchers/register"
)"
api_key="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["api_key"])' <<<"$reg_resp")"
fetcher_id="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["fetcher_id"])' <<<"$reg_resp")"
echo "fetcher_id=$fetcher_id"

echo "== fetcher me =="
me_resp="$(
  curl -fsS --max-time 10 -H "Authorization: Bearer ${api_key}" \
    "http://localhost:18082/v1/fetchers/me"
)"
me_id="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["fetcher_id"])' <<<"$me_resp")"
if [[ "$me_id" != "$fetcher_id" ]]; then
  echo "fetcher_id mismatch: register=$fetcher_id me=$me_id" >&2
  exit 1
fi

echo "== bulk ingest 1 item (quarantine) =="
source_id="ci-src-$(date +%s)-$RANDOM"
bulk_body="$(python3 - <<PY
import json
print(json.dumps({
  "items": [{
    "source_id": "$source_id",
    "title": "CI v1 quarantine ingest",
    "description": "CI v1 quarantine ingest report",
    "lat": 47.36,
    "lng": 8.55,
    "collected_at": "2026-02-14T00:00:00Z",
    "agent_id": "ci",
    "agent_version": "1.0",
    "source_type": "web",
  }]
}))
PY
)"
bulk_resp="$(
  curl -fsS --max-time 20 -H "content-type: application/json" -H "Authorization: Bearer ${api_key}" \
    -d "$bulk_body" \
    "http://localhost:18082/v1/reports:bulkIngest"
)"
seq="$(python3 -c 'import json,sys; obj=json.loads(sys.stdin.read()); items=obj.get("items") or []; assert items, "no items in response"; print(int(items[0]["report_seq"]))' <<<"$bulk_resp")"
echo "seq=$seq"

echo "== verify report_raw visibility=shadow =="
vis="$(mysql_query "SELECT visibility FROM report_raw WHERE report_seq=${seq};")"
if [[ "${vis:-}" != "shadow" ]]; then
  echo "expected report_raw.visibility=shadow, got: ${vis:-<empty>}" >&2
  exit 1
fi

echo "== wait for report_analysis row =="
for _ in $(seq 1 120); do
  n="$(mysql_query "SELECT COUNT(*) FROM report_analysis WHERE seq=${seq};")"
  if [[ "${n:-0}" != "0" ]]; then
    break
  fi
  sleep 2
done
n="$(mysql_query "SELECT COUNT(*) FROM report_analysis WHERE seq=${seq};")"
if [[ "${n:-0}" == "0" ]]; then
  echo "analysis did not complete in time (seq=$seq)" >&2
  dc logs --no-color analyzer || true
  exit 1
fi

echo "== verify quarantine is not public =="
code="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 10 "http://localhost:18082/api/v3/reports/by-seq?seq=${seq}" || true)"
if [[ "$code" != "404" ]]; then
  echo "expected 404 for quarantined seq=$seq, got http=$code" >&2
  exit 1
fi

echo "== bind temp queue to report.analysed =="
qname="ci_test_report_analysed_$$"
rabbit_json PUT "http://localhost:15672/api/queues/%2F/${qname}" '{"durable":false,"auto_delete":true,"arguments":{}}' >/dev/null
rabbit_json POST "http://localhost:15672/api/bindings/%2F/e/cleanapp/q/${qname}" '{"routing_key":"report.analysed"}' >/dev/null

echo "== promote to public and verify visible =="
curl -fsS --max-time 10 -H "content-type: application/json" \
  -H "X-Internal-Admin-Token: ci-admin-token" \
  -d '{"visibility":"public","trust_level":"verified"}' \
  "http://localhost:18082/internal/reports/${seq}/promote" >/dev/null

code="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 10 "http://localhost:18082/api/v3/reports/by-seq?seq=${seq}" || true)"
if [[ "$code" != "200" ]]; then
  echo "expected 200 after promotion seq=$seq, got http=$code" >&2
  exit 1
fi

echo "== verify promotion published report.analysed =="
for _ in $(seq 1 30); do
  msgs="$(rabbit_json GET "http://localhost:15672/api/queues/%2F/${qname}" | python3 -c 'import json,sys; print(int(json.loads(sys.stdin.read()).get("messages",0)))')"
  if [[ "${msgs:-0}" != "0" ]]; then
    break
  fi
  sleep 1
done
msgs="$(rabbit_json GET "http://localhost:15672/api/queues/%2F/${qname}" | python3 -c 'import json,sys; print(int(json.loads(sys.stdin.read()).get("messages",0)))')"
if [[ "${msgs:-0}" == "0" ]]; then
  echo "expected report.analysed publish after promotion (seq=$seq), but queue stayed empty" >&2
  dc logs --no-color report_listener || true
  dc logs --no-color analyzer || true
  exit 1
fi

echo "OK"
