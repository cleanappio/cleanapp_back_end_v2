#!/usr/bin/env bash
set -euo pipefail

db="${DB_CONTAINER:-cleanapp_db}"
root_pw="$(sudo -n docker inspect "${db}" --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | awk -F= '$1=="MYSQL_ROOT_PASSWORD"{print substr($0, index($0,"=")+1); exit}')"

mysql_n() {
  sudo -n docker exec -e MYSQL_PWD="${root_pw}" "${db}" \
    mysql -uroot -D cleanapp -N -s -e "$1"
}

for service in cleanapp_bluesky_now cleanapp_bluesky_indexer cleanapp_bluesky_analyzer cleanapp_bluesky_submitter; do
  [[ "$(sudo -n docker inspect -f '{{.State.Running}}' "${service}" 2>/dev/null || true)" == "true" ]] || {
    echo "ingestion: ${service} is not running" >&2
    exit 1
  }
done

cursor_fresh="$(mysql_n "SELECT COUNT(*) FROM indexer_bluesky_jetstream_cursor WHERE id=1 AND updated_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 20 MINUTE);")"
if [[ "${cursor_fresh}" != "1" ]]; then
  sudo -n docker restart cleanapp_bluesky_now >/dev/null
  echo "ingestion: Bluesky stream cursor stale; collector restarted" >&2
  exit 1
fi

pending="$(mysql_n "SELECT EXISTS(SELECT 1 FROM indexer_bluesky_analysis a LEFT JOIN external_ingest_index ei ON ei.source='bluesky' AND ei.external_id COLLATE utf8mb4_unicode_ci=a.uri LEFT JOIN indexer_bluesky_wire_submission ws ON ws.uri=a.uri WHERE a.is_relevant=TRUE AND ei.seq IS NULL AND ws.uri IS NULL LIMIT 1);")"
if [[ "${pending}" == "1" ]]; then
  submit_fresh="$(mysql_n "SELECT COUNT(*) FROM indexer_bluesky_wire_submission WHERE submitted_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 20 MINUTE);")"
  if [[ "${submit_fresh}" == "0" ]]; then
    sudo -n docker restart cleanapp_bluesky_submitter >/dev/null
    echo "ingestion: Bluesky backlog is pending but submissions are stale; submitter restarted" >&2
    exit 1
  fi
fi

echo "ingestion: Bluesky collectors and submitter are live"
