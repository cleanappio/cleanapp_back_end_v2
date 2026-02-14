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
api_key="$(python3 - <<PY
import json,sys
obj=json.loads(sys.stdin.read())
print(obj["api_key"])
PY
<<<"$reg_resp")"
fetcher_id="$(python3 - <<PY
import json,sys
obj=json.loads(sys.stdin.read())
print(obj["fetcher_id"])
PY
<<<"$reg_resp")"
echo "fetcher_id=$fetcher_id"

echo "== fetcher me =="
me_resp="$(
  curl -fsS --max-time 10 -H "Authorization: Bearer ${api_key}" \
    "http://localhost:18082/v1/fetchers/me"
)"
me_id="$(python3 - <<PY
import json,sys
obj=json.loads(sys.stdin.read())
print(obj["fetcher_id"])
PY
<<<"$me_resp")"
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
seq="$(python3 - <<PY
import json,sys
obj=json.loads(sys.stdin.read())
items=obj.get("items") or []
if not items:
  raise SystemExit("no items in response")
print(int(items[0]["report_seq"]))
PY
<<<"$bulk_resp")"
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

echo "OK"

