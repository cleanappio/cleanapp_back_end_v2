#!/usr/bin/env bash
set -euo pipefail

# Quick prod email pipeline stats (DB-derived, secrets-safe).
#
# Usage:
#   HOST=deployer@34.122.15.16 ./platform_blueprint/ops/email/stats_prod_vm.sh
#
# Output does not print any API keys or raw email addresses.

HOST="${HOST:-deployer@34.122.15.16}"
DB_CONTAINER="${DB_CONTAINER:-cleanapp_db}"

ssh "${HOST}" "DB_CONTAINER='${DB_CONTAINER}' bash -s" <<'REMOTE'
set -euo pipefail

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing $1" >&2; exit 1; }; }
need sudo
need docker

if ! sudo -n true 2>/dev/null; then
  echo "sudo requires password on VM; cannot run stats" >&2
  exit 1
fi

db="${DB_CONTAINER}"

get_db_env() {
  local k="$1"
  sudo -n docker inspect "${db}" --format '{{range .Config.Env}}{{println .}}{{end}}' \
    | awk -F= -v key="$k" '$1==key{print substr($0, index($0,"=")+1); exit}'
}

root_pw="$(get_db_env MYSQL_ROOT_PASSWORD || true)"
if [[ -z "${root_pw}" ]]; then
  echo "stats: could not read MYSQL_ROOT_PASSWORD from ${db} container env" >&2
  exit 1
fi

mysql_n() {
  local q="$1"
  sudo -n docker exec -e MYSQL_PWD="${root_pw}" "${db}" mysql -uroot -D cleanapp -N -s -e "${q}"
}

echo "== Email Pipeline Stats (last 24h) =="
echo "time_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo

echo "-- sends (derived from throttle table) --"
recipients_sent_24h="$(mysql_n "SELECT COUNT(*) FROM brand_email_throttle WHERE last_sent_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR);")"
unique_recipients_24h="$(mysql_n "SELECT COUNT(DISTINCT email) FROM brand_email_throttle WHERE last_sent_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR);")"
brands_sent_24h="$(mysql_n "SELECT COUNT(DISTINCT brand_name) FROM brand_email_throttle WHERE last_sent_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR);")"
echo "recipients_sent_24h=${recipients_sent_24h}"
echo "unique_recipients_24h=${unique_recipients_24h}"
echo "brands_sent_24h=${brands_sent_24h}"
echo

echo "-- report processing markers --"
reports_processed_24h="$(mysql_n "SELECT COUNT(*) FROM sent_reports_emails WHERE created_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR);")"
reports_processed_1h="$(mysql_n "SELECT COUNT(*) FROM sent_reports_emails WHERE created_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 HOUR);")"
echo "reports_processed_24h=${reports_processed_24h}"
echo "reports_processed_1h=${reports_processed_1h}"
echo

echo "-- retry queue --"
retries_total="$(mysql_n "SELECT COUNT(*) FROM email_report_retry;")"
retries_due_now="$(mysql_n "SELECT COUNT(*) FROM email_report_retry WHERE next_attempt_at <= UTC_TIMESTAMP();")"
echo "retries_total=${retries_total}"
echo "retries_due_now=${retries_due_now}"
echo
echo "retry_reasons_top10:"
sudo -n docker exec -e MYSQL_PWD="${root_pw}" "${db}" mysql -uroot -D cleanapp -e \
  "SELECT reason, COUNT(*) AS n FROM email_report_retry GROUP BY reason ORDER BY n DESC LIMIT 10;"
echo

echo "-- containers --"
sudo -n docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}' | egrep '(^NAME|cleanapp_email_fetcher|cleanapp_email_service)' || true
echo

echo "== done =="
REMOTE
