#!/usr/bin/env bash
set -euo pipefail

# VM-local email pipeline health check.
#
# Fails if:
# - email-service is unreachable, OR
# - there is work due (retry rows due now OR brandless physical with inferred contacts waiting),
#   but sent_reports_emails hasn't advanced in too long.
#
# This is intentionally lightweight and secrets-safe.

EMAIL_URL="${EMAIL_URL:-http://127.0.0.1:9089}"
DB_CONTAINER="${DB_CONTAINER:-cleanapp_db}"

# If work is pending, warn above WARN_STALE_MIN and only fail above FAIL_STALE_MIN.
# "Fail" should mean the email pipeline is truly stuck (no progress anywhere), not just slow.
WARN_STALE_MIN="${WARN_STALE_MIN:-30}"
FAIL_STALE_MIN="${FAIL_STALE_MIN:-120}"

req() {
  local url="$1"
  local timeout="${2:-5}"
  local code
  code="$(curl -sS -o /dev/null -w "%{http_code}" --max-time "${timeout}" "${url}" || true)"
  printf "%s\t%s\n" "${url}" "${code}"
  [[ "${code}" == "200" ]]
}

mysql_n() {
  local q="$1"
  sudo docker exec "${DB_CONTAINER}" sh -lc 'mysql -N -uroot -p"$MYSQL_ROOT_PASSWORD" -D cleanapp -e "$1"' sh "${q}" 2>/dev/null
}

echo "== email-service =="
req "${EMAIL_URL}/health" 5

echo
echo "== email pipeline db checks =="

# Minutes since last processed marker; if table is empty, treat as very stale.
stale_min="$(mysql_n "SET time_zone='+00:00'; SELECT IFNULL(TIMESTAMPDIFF(MINUTE, MAX(created_at), UTC_TIMESTAMP()), 999999) FROM sent_reports_emails;")"
# Minutes since any retry row changed. Used to avoid false negatives when the sender is actively rescheduling.
retry_stale_min="$(mysql_n "SET time_zone='+00:00'; SELECT IFNULL(TIMESTAMPDIFF(MINUTE, MAX(updated_at), UTC_TIMESTAMP()), 999999) FROM email_report_retry;")"
# Minutes since the physical contact discovery state changed. Used as a progress signal when send is waiting on discovery.
lookup_stale_min="$(mysql_n "SET time_zone='+00:00'; SELECT IFNULL(TIMESTAMPDIFF(MINUTE, MAX(updated_at), UTC_TIMESTAMP()), 999999) FROM physical_contact_lookup_state;")"

due_retries="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM email_report_retry WHERE next_attempt_at <= UTC_TIMESTAMP();")"

# Brandless physical work that should be drained by email-service (fast path: inferred contacts already present).
brandless_physical_with_inferred="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM report_analysis ra JOIN reports r ON r.seq=ra.seq LEFT JOIN sent_reports_emails s ON s.seq=ra.seq WHERE ra.language='en' AND ra.is_valid=1 AND ra.classification='physical' AND ra.brand_name='' AND ra.inferred_contact_emails IS NOT NULL AND ra.inferred_contact_emails<>'' AND s.seq IS NULL;")"

echo "stale_min=${stale_min}"
echo "retry_stale_min=${retry_stale_min}"
echo "lookup_stale_min=${lookup_stale_min}"
echo "due_retries=${due_retries}"
echo "brandless_physical_with_inferred=${brandless_physical_with_inferred}"

work_due=0
if [[ "${due_retries:-0}" -ge 1 ]]; then
  work_due=1
fi
if [[ "${brandless_physical_with_inferred:-0}" -ge 1 ]]; then
  work_due=1
fi

if [[ "${work_due}" -eq 1 && "${stale_min:-999999}" -gt "${WARN_STALE_MIN}" ]]; then
  echo "WARN: email pipeline is slow (work_due=1, stale_min=${stale_min} > WARN_STALE_MIN=${WARN_STALE_MIN})" >&2
fi

# Fail only if *all* activity markers are stale. This avoids paging on reschedule-heavy periods.
if [[ "${work_due}" -eq 1 \
   && "${stale_min:-999999}" -gt "${FAIL_STALE_MIN}" \
   && "${retry_stale_min:-999999}" -gt "${FAIL_STALE_MIN}" \
   && "${lookup_stale_min:-999999}" -gt "${FAIL_STALE_MIN}" ]]; then
  echo "FAIL: email pipeline appears stuck (work_due=1, stale_min=${stale_min}, retry_stale_min=${retry_stale_min}, lookup_stale_min=${lookup_stale_min} > FAIL_STALE_MIN=${FAIL_STALE_MIN})" >&2
  exit 1
fi

echo "[watchdog email] OK"
