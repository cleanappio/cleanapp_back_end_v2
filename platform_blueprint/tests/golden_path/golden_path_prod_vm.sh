#!/usr/bin/env bash
set -euo pipefail

# Golden path check from inside the prod VM:
# - ensures report ingestion -> analysis is functioning
# - if a report looks stuck (older than MIN_AGE_MIN with no analysis), it republishes its seq to report.raw
# - validates analysis appears in MySQL
#
# Run from your laptop:
#   HOST=deployer@<prod-vm> ./platform_blueprint/tests/golden_path/golden_path_prod_vm.sh
#
# Tunables:
#   WINDOW_MIN=240        (lookback window for candidate reports)
#   MIN_AGE_MIN=10        (only treat as stuck if older than this)
#   WAIT_SEC=300          (time to wait for analysis after requeue)
#   REQUEUE_LIMIT=1       (max number of stuck reports to requeue per run)

HOST="${HOST:-}"
WINDOW_MIN="${WINDOW_MIN:-240}"
MIN_AGE_MIN="${MIN_AGE_MIN:-10}"
WAIT_SEC="${WAIT_SEC:-300}"
REQUEUE_LIMIT="${REQUEUE_LIMIT:-1}"

if [[ -z "${HOST}" ]]; then
  echo "usage: HOST=deployer@<prod-vm> $0" >&2
  exit 2
fi

echo "[golden] prod vm: ${HOST}"

q_window="$(printf "%q" "${WINDOW_MIN}")"
q_min_age="$(printf "%q" "${MIN_AGE_MIN}")"
q_wait="$(printf "%q" "${WAIT_SEC}")"
q_limit="$(printf "%q" "${REQUEUE_LIMIT}")"

ssh "${HOST}" "WINDOW_MIN=${q_window} MIN_AGE_MIN=${q_min_age} WAIT_SEC=${q_wait} REQUEUE_LIMIT=${q_limit} bash -s" <<'__REMOTE__'
set -euo pipefail

WINDOW_MIN="${WINDOW_MIN:-240}"
MIN_AGE_MIN="${MIN_AGE_MIN:-10}"
WAIT_SEC="${WAIT_SEC:-300}"
REQUEUE_LIMIT="${REQUEUE_LIMIT:-1}"

mysql_q() {
  local q="$1"
  sudo docker exec cleanapp_db sh -lc 'mysql -N -uroot -p"$MYSQL_ROOT_PASSWORD" -D cleanapp -e "$1"' sh "$q" 2>/dev/null
}

rabbit_user() {
  sudo docker inspect cleanapp_rabbitmq --format "{{range .Config.Env}}{{println .}}{{end}}" \
    | awk -F= '$1=="RABBITMQ_DEFAULT_USER"{print substr($0, index($0,"=")+1); exit}'
}

rabbit_pass() {
  sudo docker inspect cleanapp_rabbitmq --format "{{range .Config.Env}}{{println .}}{{end}}" \
    | awk -F= '$1=="RABBITMQ_DEFAULT_PASS"{print substr($0, index($0,"=")+1); exit}'
}

rabbit_publish_report_raw_seq() {
  local seq="$1"
  local user pass
  user="$(rabbit_user)"
  pass="$(rabbit_pass)"
  if [[ -z "${user}" || -z "${pass}" ]]; then
    echo "[golden] ERROR: could not read rabbitmq creds from container env" >&2
    return 1
  fi
  curl -fsS -u "${user}:${pass}" -H "content-type: application/json" \
    -X POST "http://127.0.0.1:15672/api/exchanges/%2F/cleanapp-exchange/publish" \
    -d "{\"properties\":{\"delivery_mode\":2},\"routing_key\":\"report.raw\",\"payload\":\"{\\\"seq\\\":${seq}}\",\"payload_encoding\":\"string\"}" >/dev/null
}

echo "== golden: find stuck reports (window=${WINDOW_MIN}min min_age=${MIN_AGE_MIN}min) =="
stuck_seqs="$(mysql_q "SET time_zone='+00:00'; SELECT r.seq FROM reports r LEFT JOIN report_analysis ra ON ra.seq=r.seq WHERE r.ts >= (UTC_TIMESTAMP() - INTERVAL ${WINDOW_MIN} MINUTE) AND r.ts <= (UTC_TIMESTAMP() - INTERVAL ${MIN_AGE_MIN} MINUTE) AND ra.seq IS NULL ORDER BY r.ts ASC LIMIT ${REQUEUE_LIMIT};")"

if [[ -z "${stuck_seqs}" ]]; then
  echo "[golden] OK: no stuck reports detected"
  exit 0
fi

echo "[golden] detected stuck report seq(s):"
echo "${stuck_seqs}" | sed -n "1,10p"

for seq in ${stuck_seqs}; do
  echo
  echo "== golden: requeue seq=${seq} to report.raw =="
  rabbit_publish_report_raw_seq "${seq}"
  echo "[golden] published seq=${seq} to report.raw"

  echo "== golden: wait for analysis row (seq=${seq}) =="
  deadline=$(( $(date +%s) + WAIT_SEC ))
  while :; do
    cnt="$(mysql_q "SELECT COUNT(*) FROM report_analysis WHERE seq=${seq};")"
    if [[ "${cnt:-0}" -ge 1 ]]; then
      ts="$(mysql_q "SET time_zone='+00:00'; SELECT DATE_FORMAT(MAX(updated_at), '%Y-%m-%dT%H:%i:%sZ') FROM report_analysis WHERE seq=${seq};")"
      echo "[golden] OK: analysis present (rows=${cnt} updated_at=${ts})"
      break
    fi
    now=$(date +%s)
    if [[ "${now}" -ge "${deadline}" ]]; then
      echo "[golden] FAIL: analysis did not appear for seq=${seq} within ${WAIT_SEC}s" >&2
      echo "[golden] diagnostics:" >&2
      sudo docker exec cleanapp_rabbitmq rabbitmqctl list_queues name messages_ready messages_unacknowledged consumers | egrep "^(name|report-analysis-queue)" >&2 || true
      curl -fsS --max-time 3 http://127.0.0.1:9082/api/v3/health >&2 || true
      exit 1
    fi
    sleep 5
  done
done

echo
echo "[golden] OK"
__REMOTE__
