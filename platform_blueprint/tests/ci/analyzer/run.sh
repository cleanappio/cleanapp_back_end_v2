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
PAYLOAD="{\"seq\":${SEQ},\"description\":\"${DESC}\"}"
PUBLISH_BODY="{\"properties\":{},\"routing_key\":\"report.raw\",\"payload\":\"${PAYLOAD}\",\"payload_encoding\":\"string\"}"
curl -fsS --max-time 5 --netrc-file "$NETRC_FILE" -H "content-type: application/json" \
  -d "$PUBLISH_BODY" \
  "http://localhost:15672/api/exchanges/%2F/cleanapp/publish" \
  | grep -q '"routed":true'

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

echo "== basic endpoints =="
curl -fsS --max-time 5 "http://localhost:18080/api/v3/analysis/${SEQ}" >/dev/null
curl -fsS --max-time 5 "http://localhost:18080/metrics" >/dev/null

echo "OK"
