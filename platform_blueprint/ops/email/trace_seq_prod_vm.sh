#!/usr/bin/env bash
set -euo pipefail

# Trace one report seq end-to-end on a VM (DB state + worker/service logs).
#
# Usage:
#   HOST=deployer@34.122.15.16 ./platform_blueprint/ops/email/trace_seq_prod_vm.sh 12345
#   SEQ=12345 ./platform_blueprint/ops/email/trace_seq_prod_vm.sh
#
# Output intentionally redacts email addresses from logs.

HOST="${HOST:-deployer@34.122.15.16}"
SEQ="${SEQ:-${1:-}}"

if [[ -z "${SEQ}" ]]; then
  echo "usage: SEQ=<seq> $0  (or pass seq as first arg)" >&2
  exit 2
fi

ssh "${HOST}" "SEQ='${SEQ}' bash -s" <<'REMOTE'
set -euo pipefail

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing $1" >&2; exit 1; }; }
need sudo
need docker

if ! sudo -n true 2>/dev/null; then
  echo "sudo requires password on VM; cannot trace" >&2
  exit 1
fi

seq="${SEQ}"
db="${DB_CONTAINER:-cleanapp_db}"

get_db_env() {
  local k="$1"
  sudo -n docker inspect "${db}" --format '{{range .Config.Env}}{{println .}}{{end}}' \
    | awk -F= -v key="$k" '$1==key{print substr($0, index($0,"=")+1); exit}'
}

root_pw="$(get_db_env MYSQL_ROOT_PASSWORD || true)"
if [[ -z "${root_pw}" ]]; then
  echo "trace: could not read MYSQL_ROOT_PASSWORD from ${db} container env" >&2
  exit 1
fi

mysql_q() {
  local q="$1"
  sudo -n docker exec -e MYSQL_PWD="${root_pw}" "${db}" mysql -uroot -D cleanapp -e "${q}"
}

mysql_val() {
  local q="$1"
  sudo -n docker exec -e MYSQL_PWD="${root_pw}" "${db}" mysql -uroot -D cleanapp -N -s -e "${q}" 2>/dev/null | head -n 1 || true
}

mask_emails() {
  sed -E 's/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}/[redacted-email]/g'
}

echo "== trace seq=${seq} =="
echo

brand_name="$(mysql_val "SELECT brand_name FROM report_analysis WHERE seq=${seq} AND language='en' LIMIT 1;")"
brand_display="$(mysql_val "SELECT COALESCE(NULLIF(brand_display_name,''), brand_name) FROM report_analysis WHERE seq=${seq} AND language='en' LIMIT 1;")"
processed_at="$(mysql_val "SELECT DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s') FROM sent_reports_emails WHERE seq=${seq} LIMIT 1;")"

echo "== reports =="
mysql_q "SELECT seq,id,ts,latitude,longitude FROM reports WHERE seq=${seq} LIMIT 1;"
echo

echo "== report_analysis (summary) =="
mysql_q "SELECT seq,is_valid,classification,language,brand_name,brand_display_name,severity_level,CHAR_LENGTH(inferred_contact_emails) AS inferred_len,LEFT(inferred_contact_emails,64) AS inferred_preview FROM report_analysis WHERE seq=${seq} LIMIT 1;" | mask_emails
echo

echo "== email_report_retry =="
mysql_q "SELECT seq,reason,next_attempt_at,retry_count,created_at,updated_at FROM email_report_retry WHERE seq=${seq} LIMIT 5;"
echo

echo "== sent_reports_emails (processed marker) =="
mysql_q "SELECT seq,created_at FROM sent_reports_emails WHERE seq=${seq} LIMIT 5;"
echo

if [[ -n "${brand_name}" ]]; then
  echo "== brand context =="
  echo "brand_name=${brand_name}"
  echo "brand_display=${brand_display}"
  if [[ -n "${processed_at}" ]]; then
    echo "processed_at=${processed_at}"
  fi
  echo

  echo "== brand_emails (known, redacted) =="
  mysql_q "SELECT brand_name,CONCAT(LEFT(email_address,2),'***@',SUBSTRING_INDEX(email_address,'@',-1)) AS email_redacted,create_timestamp FROM brand_emails WHERE brand_name='${brand_name}' ORDER BY create_timestamp DESC LIMIT 20;"
  echo

  echo "== brand_email_throttle (recent, redacted) =="
  if [[ -n "${processed_at}" ]]; then
    # Match the send that likely caused this report to be marked processed.
    mysql_q "SELECT brand_name,CONCAT(LEFT(email,2),'***@',SUBSTRING_INDEX(email,'@',-1)) AS email_redacted,last_sent_at,email_count FROM brand_email_throttle WHERE brand_name='${brand_name}' AND last_sent_at BETWEEN DATE_SUB('${processed_at}', INTERVAL 10 MINUTE) AND DATE_ADD('${processed_at}', INTERVAL 10 MINUTE) ORDER BY last_sent_at DESC;"
  else
    mysql_q "SELECT brand_name,CONCAT(LEFT(email,2),'***@',SUBSTRING_INDEX(email,'@',-1)) AS email_redacted,last_sent_at,email_count FROM brand_email_throttle WHERE brand_name='${brand_name}' ORDER BY last_sent_at DESC LIMIT 20;"
  fi
  echo
fi

echo "== physical_contact_lookup_state =="
mysql_q "SELECT seq,status,attempt_count,next_attempt_at,claimed_at,claimed_by,selected_by_version,selected_reason,CHAR_LENGTH(selected_emails) AS selected_len,LEFT(selected_emails,64) AS selected_preview FROM physical_contact_lookup_state WHERE seq=${seq} LIMIT 5;" | mask_emails
echo

echo "== physical_contact_candidates (by source_type) =="
mysql_q "SELECT source_type,COUNT(*) AS n,MAX(updated_at) AS last_updated FROM physical_contact_candidates WHERE seq=${seq} GROUP BY source_type ORDER BY n DESC;"
echo

echo "== physical_contact_candidates (top 10 redacted) =="
mysql_q "SELECT CONCAT(LEFT(email,2),'***@',SUBSTRING_INDEX(email,'@',-1)) AS email_redacted,area_id,source_type,confidence,updated_at FROM physical_contact_candidates WHERE seq=${seq} ORDER BY confidence DESC,updated_at DESC LIMIT 10;"
echo

echo "== containers =="
sudo -n docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}' | egrep '(^NAME|cleanapp_email_fetcher|cleanapp_email_service)' || true
echo

email_svc="$(sudo -n docker ps --format '{{.Names}}' | grep -E '^cleanapp_email_service' | head -n 1 || true)"
if [[ -n "${email_svc}" ]]; then
  echo "== email-service logs (last 24h, seq=${seq}) =="
  sudo -n docker logs --since 24h "${email_svc}" 2>/dev/null | grep -E "Report[[:space:]]+${seq}\\b" | tail -n 120 | mask_emails || true
  echo

  if [[ -n "${brand_name}" ]]; then
    echo "== email-service logs (last 24h, brand=${brand_name}) =="
    sudo -n docker logs --since 24h "${email_svc}" 2>/dev/null | grep -F "Brand ${brand_name}:" | tail -n 160 | mask_emails || true
    echo
  fi
fi

fetcher_svc="cleanapp_email_fetcher"
if sudo -n docker ps --format '{{.Names}}' | grep -qx "${fetcher_svc}"; then
  echo "== email-fetcher logs (last 24h, seq=${seq}) =="
  sudo -n docker logs --since 24h "${fetcher_svc}" 2>/dev/null | grep -E "seq=${seq}\\b" | tail -n 120 | mask_emails || true
  echo
fi

echo "== done =="
REMOTE
