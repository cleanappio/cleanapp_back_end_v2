#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/platform_blueprint/tests/ci/pipeline/docker-compose.yml"

dc() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

RABBITMQ_MGMT_USER="${RABBITMQ_MGMT_USER:-guest}"
RABBITMQ_MGMT_PASSWORD="${RABBITMQ_MGMT_PASSWORD:-guest}"

NETRC_FILE="$(mktemp)"
chmod 600 "$NETRC_FILE"
printf "machine localhost login %s password %s\n" "$RABBITMQ_MGMT_USER" "$RABBITMQ_MGMT_PASSWORD" >"$NETRC_FILE"

cleanup() {
  rm -f "$NETRC_FILE" >/dev/null 2>&1 || true
  dc down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_http_200() {
  local url="$1"
  local tries="${2:-90}"
  local delay="${3:-2}"
  for _ in $(seq 1 "$tries"); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done
  return 1
}

mysql_count() {
  local sql="$1"
  dc exec -T mysql mysql -uroot -proot cleanapp -N -e "$sql"
}

rabbit_publish_raw() {
  local seq="$1"
  local desc="$2"
  local tags_json="$3"
  local payload_json payload_b64 body
  payload_json="$(python3 - <<PY
import json
print(json.dumps({"seq": int("$seq"), "description": "$desc", "tags": $tags_json}))
PY
)"
  payload_b64="$(printf '%s' "$payload_json" | base64 | tr -d '\n')"
  body="$(printf '{"properties":{},"routing_key":"report.raw","payload":"%s","payload_encoding":"base64"}' "$payload_b64")"
  local resp
  resp="$(curl -fsS --max-time 10 --netrc-file "$NETRC_FILE" -H "content-type: application/json" \
    -d "$body" "http://localhost:15672/api/exchanges/%2F/cleanapp/publish")"
  echo "$resp" | grep -q '"routed":true'
}

queue_consumers() {
  local queue="$1"
  curl -fsS --max-time 10 --netrc-file "$NETRC_FILE" "http://localhost:15672/api/queues/%2F/${queue}" \
    | python3 -c 'import json,sys; print(int(json.load(sys.stdin).get("consumers",0)))'
}

renderer_count() {
  curl -fsS --max-time 5 "http://localhost:19093/stats" \
    | python3 -c 'import json,sys; print(int(json.load(sys.stdin)["total_physical_reports"]["count"]))'
}

insert_report() {
  local desc="$1"
  local png_b64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5+1WQAAAAASUVORK5CYII="
  dc exec -T mysql mysql -uroot -proot cleanapp -N -e \
    "INSERT INTO reports (id, team, latitude, longitude, image, action_id, description) VALUES ('ci-pipeline', 1, 47.36, 8.55, FROM_BASE64('${png_b64}'), 'ci', '${desc}'); SELECT LAST_INSERT_ID();"
}

wait_for_sql_nonzero() {
  local sql="$1"
  local label="$2"
  for _ in $(seq 1 120); do
    local n
    n="$(mysql_count "$sql")"
    if [[ "${n:-0}" != "0" ]]; then
      return 0
    fi
    sleep 2
  done
  echo "timeout waiting for ${label}" >&2
  return 1
}

echo "== bring up stack =="
dc up -d --build

echo "== wait services =="
wait_http_200 "http://localhost:18080/api/v3/health"
wait_http_200 "http://localhost:19098/health"
wait_http_200 "http://localhost:19093/health"
wait_http_200 "http://localhost:15672/api/overview"

echo "== baseline renderer count =="
base_count="$(renderer_count)"
echo "renderer_count_base=$base_count"

desc1="CI pipeline report one"
seq1="$(insert_report "$desc1")"
echo "seq1=$seq1"
rabbit_publish_raw "$seq1" "$desc1" '["ci-tag-alpha","ci-tag-beta"]'

wait_for_sql_nonzero "SELECT COUNT(*) FROM report_analysis WHERE seq=${seq1} AND language='en';" "analysis row seq=${seq1}"
wait_for_sql_nonzero "SELECT COUNT(*) FROM report_tags WHERE report_seq=${seq1};" "tags row seq=${seq1}"

for _ in $(seq 1 90); do
  now_count="$(renderer_count)"
  if [[ "$now_count" -ge $((base_count + 1)) ]]; then
    break
  fi
  sleep 2
done

now_count="$(renderer_count)"
if [[ "$now_count" -lt $((base_count + 1)) ]]; then
  echo "renderer did not reflect new analysed report (base=$base_count now=$now_count)" >&2
  dc logs --no-color renderer || true
  exit 1
fi

echo "== broker restart resilience =="
dc restart rabbitmq >/dev/null
wait_http_200 "http://localhost:15672/api/overview" 90 2

for _ in $(seq 1 90); do
  a="$(queue_consumers report-analyze || echo 0)"
  t="$(queue_consumers report-tags || echo 0)"
  r="$(queue_consumers report-renderer || echo 0)"
  if [[ "$a" -ge 1 && "$t" -ge 1 && "$r" -ge 1 ]]; then
    break
  fi
  sleep 2
done

a="$(queue_consumers report-analyze || echo 0)"
t="$(queue_consumers report-tags || echo 0)"
r="$(queue_consumers report-renderer || echo 0)"
if [[ "$a" -lt 1 || "$t" -lt 1 || "$r" -lt 1 ]]; then
  echo "consumers did not recover after broker restart: analyze=$a tags=$t renderer=$r" >&2
  dc logs --no-color analyzer tags renderer rabbitmq || true
  exit 1
fi

desc2="CI pipeline report two"
seq2="$(insert_report "$desc2")"
echo "seq2=$seq2"
rabbit_publish_raw "$seq2" "$desc2" '["ci-tag-gamma"]'
wait_for_sql_nonzero "SELECT COUNT(*) FROM report_analysis WHERE seq=${seq2} AND language='en';" "analysis row seq=${seq2}"
wait_for_sql_nonzero "SELECT COUNT(*) FROM report_tags WHERE report_seq=${seq2};" "tags row seq=${seq2}"

curl -fsS --max-time 5 "http://localhost:18080/api/v3/health" \
  | python3 -c 'import json,sys; h=json.load(sys.stdin); assert h.get("rabbitmq_connected") is True, h; print("rabbitmq_connected=true")'

echo "OK"
