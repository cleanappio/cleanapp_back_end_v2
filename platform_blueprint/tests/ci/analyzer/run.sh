#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/platform_blueprint/tests/ci/analyzer/docker-compose.yml"

dc() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

RABBITMQ_MGMT_USER="${RABBITMQ_MGMT_USER:-}"
RABBITMQ_MGMT_PASSWORD="${RABBITMQ_MGMT_PASSWORD:-}"
if [[ -z "$RABBITMQ_MGMT_USER" || -z "$RABBITMQ_MGMT_PASSWORD" ]]; then
  echo "missing RABBITMQ_MGMT_USER / RABBITMQ_MGMT_PASSWORD (required for RabbitMQ mgmt API publish in CI)" >&2
  exit 2
fi
NETRC_FILE="$(mktemp)"
chmod 600 "$NETRC_FILE"
printf "machine localhost login %s password %s\n" "$RABBITMQ_MGMT_USER" "$RABBITMQ_MGMT_PASSWORD" >"$NETRC_FILE"

cleanup() {
  rm -f "$NETRC_FILE" >/dev/null 2>&1 || true
  dc down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== bring up stack =="
dc up -d --build

echo "== wait for analyzer health =="
for _ in $(seq 1 90); do
  if curl -fsS --max-time 2 "http://localhost:18080/api/v3/health" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS --max-time 5 "http://localhost:18080/api/v3/health" >/dev/null

echo "== wait for rabbitmq mgmt =="
for _ in $(seq 1 60); do
  if curl -fsS --max-time 2 --netrc-file "$NETRC_FILE" "http://localhost:15672/api/overview" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

ANALYSED_TAP_QUEUE="ci-report-analysed-${RANDOM}-${RANDOM}"

echo "== create temporary tap queue for report.analysed =="
curl -fsS --max-time 10 --netrc-file "$NETRC_FILE" -H "content-type: application/json" -X PUT \
  -d '{"auto_delete":true,"durable":false,"arguments":{}}' \
  "http://localhost:15672/api/queues/%2F/${ANALYSED_TAP_QUEUE}" >/dev/null
curl -fsS --max-time 10 --netrc-file "$NETRC_FILE" -H "content-type: application/json" -X POST \
  -d '{"routing_key":"report.analysed","arguments":{}}' \
  "http://localhost:15672/api/bindings/%2F/e/cleanapp/q/${ANALYSED_TAP_QUEUE}" >/dev/null

echo "== insert report into mysql =="
PNG_B64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5+1WQAAAAASUVORK5CYII="
DESC="CI golden path report"
SEQ="$(
  dc exec -T mysql mysql -uroot -proot cleanapp -N -e \
    "INSERT INTO reports (id, team, latitude, longitude, image, action_id, description) VALUES ('ci', 1, 47.36, 8.55, FROM_BASE64('${PNG_B64}'), 'ci', '${DESC}'); SELECT LAST_INSERT_ID();"
)"
if [[ -z "$SEQ" ]]; then
  echo "failed to insert report (no seq returned)" >&2
  exit 1
fi
echo "seq=$SEQ"

echo "== publish to rabbitmq exchange =="
PAYLOAD_JSON="$(python3 - <<PY
import json
print(json.dumps({"seq": int("$SEQ"), "description": "$DESC"}))
PY
)"
PAYLOAD_B64="$(printf '%s' "$PAYLOAD_JSON" | base64 | tr -d '\n')"
PUBLISH_BODY="$(printf '{"properties":{},"routing_key":"report.raw","payload":"%s","payload_encoding":"base64"}' "$PAYLOAD_B64")"

PUBLISH_RESP="$(curl -fsS --max-time 10 --netrc-file "$NETRC_FILE" -H "content-type: application/json" \
  -d "$PUBLISH_BODY" \
  "http://localhost:15672/api/exchanges/%2F/cleanapp/publish")"
echo "$PUBLISH_RESP" | grep -q '"routed":true'

echo "== wait for report_analysis row =="
for _ in $(seq 1 120); do
  n="$(
    dc exec -T mysql mysql -uroot -proot cleanapp -N -e \
      "SELECT COUNT(*) FROM report_analysis WHERE seq=${SEQ} AND language='en';"
  )"
  if [[ "${n:-0}" != "0" ]]; then
    break
  fi
  sleep 2
done

n="$(
  dc exec -T mysql mysql -uroot -proot cleanapp -N -e \
    "SELECT COUNT(*) FROM report_analysis WHERE seq=${SEQ} AND language='en';"
)"
if [[ "${n:-0}" == "0" ]]; then
  echo "analysis did not complete in time (seq=$SEQ)" >&2
  dc logs --no-color analyzer || true
  exit 1
fi

echo "== wait for report.analysed message =="
seen_analysed=0
for _ in $(seq 1 60); do
  get_resp="$(curl -fsS --max-time 10 --netrc-file "$NETRC_FILE" -H "content-type: application/json" -X POST \
    -d '{"count":10,"ackmode":"ack_requeue_false","encoding":"auto","truncate":50000}' \
    "http://localhost:15672/api/queues/%2F/${ANALYSED_TAP_QUEUE}/get")"

  seen_analysed="$(python3 - "$SEQ" "$get_resp" <<'PY'
import json
import sys

seq = int(sys.argv[1])
raw = sys.argv[2].strip()
msgs = json.loads(raw) if raw else []

def has_seq(payload_text: str, seq: int) -> bool:
    compact = payload_text.replace(" ", "")
    if f'"seq":{seq}' in compact:
        return True
    try:
        obj = json.loads(payload_text)
        return isinstance(obj, dict) and int(obj.get("seq", -1)) == seq
    except Exception:
        return False

for m in msgs:
    payload = m.get("payload")
    if payload is None:
        continue
    if isinstance(payload, (dict, list)):
        payload_text = json.dumps(payload)
    else:
        payload_text = str(payload)
    if has_seq(payload_text, seq):
        print("1")
        sys.exit(0)

print("0")
PY
)"

  if [[ "$seen_analysed" == "1" ]]; then
    break
  fi
  sleep 2
done

if [[ "$seen_analysed" != "1" ]]; then
  echo "did not observe report.analysed for seq=$SEQ" >&2
  dc logs --no-color analyzer || true
  exit 1
fi

echo "== basic endpoints =="
curl -fsS --max-time 5 "http://localhost:18080/api/v3/analysis/${SEQ}" >/dev/null
curl -fsS --max-time 5 "http://localhost:18080/metrics" >/dev/null

echo "OK"
