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

mysql_q() {
  local q="$1"
  sudo -n docker exec "${db}" sh -lc "mysql -uroot -p\"\\$MYSQL_ROOT_PASSWORD\" -D cleanapp -e \"$q\""
}

mask_emails() {
  sed -E 's/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}/[redacted-email]/g'
}

echo "== trace seq=${seq} =="
echo

echo "== reports =="
mysql_q "SELECT seq,id,ts,latitude,longitude FROM reports WHERE seq=${seq} LIMIT 1;"
echo

echo "== report_analysis (summary) =="
mysql_q "SELECT seq,is_valid,classification,language,brand_display_name,severity_level,CHAR_LENGTH(inferred_contact_emails) AS inferred_len,LEFT(inferred_contact_emails,64) AS inferred_preview FROM report_analysis WHERE seq=${seq} LIMIT 1;" | mask_emails
echo

echo "== email_report_retry =="
mysql_q "SELECT seq,reason,next_attempt_at,retry_count,created_at,updated_at FROM email_report_retry WHERE seq=${seq} LIMIT 5;"
echo

echo "== sent_reports_emails (processed marker) =="
mysql_q "SELECT seq,created_at FROM sent_reports_emails WHERE seq=${seq} LIMIT 5;"
echo

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
fi

fetcher_svc="cleanapp_email_fetcher"
if sudo -n docker ps --format '{{.Names}}' | grep -qx "${fetcher_svc}"; then
  echo "== email-fetcher logs (last 24h, seq=${seq}) =="
  sudo -n docker logs --since 24h "${fetcher_svc}" 2>/dev/null | grep -E "seq=${seq}\\b" | tail -n 120 | mask_emails || true
  echo
fi

echo "== done =="
REMOTE
