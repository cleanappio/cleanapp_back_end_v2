#!/usr/bin/env bash
set -euo pipefail

# Produces a secrets-safe, public-facing JSON status summary for /uptime.
#
# Output is written atomically to:
#   /var/www/cleanapp_status/uptime.json
#
# Intended to be called from ~/cleanapp_watchdog/run.sh (cron).

OUT_DIR="${OUT_DIR:-/var/www/cleanapp_status}"
OUT_JSON="${OUT_JSON:-${OUT_DIR}/uptime.json}"

EMAIL_URL="${EMAIL_URL:-http://127.0.0.1:9089}"
DB_CONTAINER="${DB_CONTAINER:-cleanapp_db}"
RABBIT_CONTAINER="${RABBIT_CONTAINER:-cleanapp_rabbitmq}"

HTTP_TIMEOUT_SEC="${HTTP_TIMEOUT_SEC:-5}"

ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

mkdir -p /tmp/cleanapp_watchdog_public >/dev/null 2>&1 || true
checks_file="/tmp/cleanapp_watchdog_public/checks.$$"
out_tmp="/tmp/cleanapp_watchdog_public/uptime.json.$$"
trap 'rm -f "${checks_file}" "${out_tmp}" >/dev/null 2>&1 || true' EXIT

mkdir -p "$(dirname "${checks_file}")"
: >"${checks_file}"

add_check() {
  local id="$1"
  local name="$2"
  local status="$3"  # ok|warn|fail
  local details="${4:-}"
  local link="${5:-}"
  # Pipe-delimited for easy parsing. Keep details/link single-line; avoid '|'.
  printf "%s|%s|%s|%s|%s\n" "${id}" "${name}" "${status}" "${details}" "${link}" >>"${checks_file}"
}

http_code() {
  local url="$1"
  curl -sS -o /dev/null -w "%{http_code}" --max-time "${HTTP_TIMEOUT_SEC}" "${url}" 2>/dev/null || true
}

mysql_n() {
  local q="$1"
  sudo -n docker exec "${DB_CONTAINER}" sh -lc 'mysql -N -uroot -p"$MYSQL_ROOT_PASSWORD" -D cleanapp -e "$1"' sh "${q}" 2>/dev/null || true
}

safe_mkdir_outdir() {
  # Nginx runs as root-managed www-data; keep world-readable but write via sudo.
  sudo -n mkdir -p "${OUT_DIR}" >/dev/null 2>&1 || true
  sudo -n chmod 755 "${OUT_DIR}" >/dev/null 2>&1 || true
}

safe_mkdir_outdir

# --- Services (localhost health) ---
for item in \
  "api_v3|API v3 (report-listener)|http://127.0.0.1:9082/api/v3/health" \
  "api_v4|API v4 (report-listener-v4)|http://127.0.0.1:9097/api/v4/health" \
  "renderer|Report Renderer|http://127.0.0.1:9093/health" \
  "tags|Report Tags|http://127.0.0.1:9098/health" \
  "email|Email Service|${EMAIL_URL}/health"
do
  IFS="|" read -r id name url <<<"${item}"
  code="$(http_code "${url}")"
  if [[ "${code}" == "200" ]]; then
    add_check "${id}" "${name}" "ok" "http=200" "${url}"
  else
    add_check "${id}" "${name}" "fail" "http=${code:-0}" "${url}"
  fi
done

# --- RabbitMQ queues / consumers ---
queues_tsv="$(sudo -n docker exec "${RABBIT_CONTAINER}" rabbitmqctl list_queues name messages_ready messages_unacknowledged consumers --no-table-headers 2>/dev/null || true)"
if [[ -z "${queues_tsv}" ]]; then
  add_check "rabbitmq" "RabbitMQ" "fail" "rabbitmqctl_failed" ""
else
  add_check "rabbitmq" "RabbitMQ" "ok" "queues_ok" ""

  for q in report-analysis-queue report-tags-queue report-renderer-queue; do
    line="$(printf "%s\n" "${queues_tsv}" | awk -v q="${q}" '$1==q {print; exit}')"
    if [[ -z "${line}" ]]; then
      add_check "rabbit_${q}" "Queue: ${q}" "fail" "missing" ""
      continue
    fi
    # name ready unacked consumers
    ready="$(echo "${line}" | awk '{print $2}')"
    unacked="$(echo "${line}" | awk '{print $3}')"
    consumers="$(echo "${line}" | awk '{print $4}')"
    status="ok"
    if [[ "${consumers}" == "0" ]]; then
      status="fail"
    elif [[ "${ready}" != "0" ]]; then
      status="warn"
    fi
    add_check "rabbit_${q}" "Queue: ${q}" "${status}" "ready=${ready} unacked=${unacked} consumers=${consumers}" ""
  done
fi

# --- Backup freshness (log-based) ---
backup_log="/home/deployer/backups/backup.log"
backup_last_ts=""
backup_age_hours=""
if [[ -f "${backup_log}" ]]; then
  last_complete_line="$(grep -E "INFO backup complete env=prod" "${backup_log}" | tail -n 1 || true)"
  if [[ -n "${last_complete_line}" ]]; then
    backup_last_ts="$(echo "${last_complete_line}" | awk '{print $1}')"
    last_epoch="$(date -u -d "${backup_last_ts}" +%s 2>/dev/null || true)"
    now_epoch="$(date -u +%s)"
    if [[ -n "${last_epoch}" ]]; then
      age_seconds=$((now_epoch - last_epoch))
      backup_age_hours="$((age_seconds / 3600))"
    fi
  fi
fi
if [[ -z "${backup_last_ts}" || -z "${backup_age_hours}" ]]; then
  add_check "backup" "DB Backup" "fail" "missing_or_unparseable" ""
else
  # Warn at 24h, fail at 30h (matches backup_freshness.sh default).
  if [[ "${backup_age_hours}" -gt 30 ]]; then
    add_check "backup" "DB Backup" "fail" "age_hours=${backup_age_hours} last=${backup_last_ts}" ""
  elif [[ "${backup_age_hours}" -gt 24 ]]; then
    add_check "backup" "DB Backup" "warn" "age_hours=${backup_age_hours} last=${backup_last_ts}" ""
  else
    add_check "backup" "DB Backup" "ok" "age_hours=${backup_age_hours} last=${backup_last_ts}" ""
  fi
fi

# --- Pipeline liveness (light DB-derived markers) ---
last_ingested="$(mysql_n "SET time_zone='+00:00'; SELECT DATE_FORMAT(ts, '%Y-%m-%dT%H:%i:%sZ') FROM reports ORDER BY seq DESC LIMIT 1;")"
if [[ -n "${last_ingested}" ]]; then
  add_check "ingest" "Ingest (latest report)" "ok" "last_ts=${last_ingested}" ""
else
  add_check "ingest" "Ingest (latest report)" "fail" "db_query_failed" ""
fi

last_analyzed="$(mysql_n "SET time_zone='+00:00'; SELECT DATE_FORMAT(r.ts, '%Y-%m-%dT%H:%i:%sZ') FROM report_analysis ra JOIN reports r ON r.seq=ra.seq ORDER BY ra.seq DESC LIMIT 1;")"
if [[ -n "${last_analyzed}" ]]; then
  add_check "analyze" "Analyze (latest analyzed)" "ok" "last_ts=${last_analyzed}" ""
else
  add_check "analyze" "Analyze (latest analyzed)" "warn" "missing_or_db_query_failed" ""
fi

# --- Email activity (secrets-safe counters) ---
stale_min="$(mysql_n "SET time_zone='+00:00'; SELECT IFNULL(TIMESTAMPDIFF(MINUTE, MAX(created_at), UTC_TIMESTAMP()), 999999) FROM sent_reports_emails;")"
due_retries="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM email_report_retry WHERE next_attempt_at <= UTC_TIMESTAMP();")"
brandless_physical_with_inferred="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM report_analysis ra JOIN reports r ON r.seq=ra.seq LEFT JOIN sent_reports_emails s ON s.seq=ra.seq WHERE ra.language='en' AND ra.is_valid=1 AND ra.classification='physical' AND ra.brand_name='' AND ra.inferred_contact_emails IS NOT NULL AND ra.inferred_contact_emails<>'' AND s.seq IS NULL;")"
retry_stale_min="$(mysql_n "SET time_zone='+00:00'; SELECT IFNULL(TIMESTAMPDIFF(MINUTE, MAX(updated_at), UTC_TIMESTAMP()), 999999) FROM email_report_retry;")"
lookup_stale_min="$(mysql_n "SET time_zone='+00:00'; SELECT IFNULL(TIMESTAMPDIFF(MINUTE, MAX(updated_at), UTC_TIMESTAMP()), 999999) FROM physical_contact_lookup_state;")"

recipients_1h="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM email_recipient_history WHERE last_email_sent_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 HOUR);")"
recipients_24h="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM email_recipient_history WHERE last_email_sent_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR);")"
processed_1h="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM sent_reports_emails WHERE created_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 HOUR);")"
processed_24h="$(mysql_n "SET time_zone='+00:00'; SELECT COUNT(*) FROM sent_reports_emails WHERE created_at >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 24 HOUR);")"

email_status="ok"
email_details="stale_min=${stale_min:-} retry_stale_min=${retry_stale_min:-} lookup_stale_min=${lookup_stale_min:-} due_retries=${due_retries:-} brandless_pending=${brandless_physical_with_inferred:-} recipients_1h=${recipients_1h:-} recipients_24h=${recipients_24h:-}"
if [[ -z "${stale_min}" ]]; then
  email_status="warn"
  email_details="missing_metrics"
else
  work_due=0
  if [[ "${due_retries:-0}" -ge 1 || "${brandless_physical_with_inferred:-0}" -ge 1 ]]; then
    work_due=1
  fi
  # Treat "activity" as degraded (warn) when the sender hasn't processed a report recently but work is due.
  # Only mark "fail" (Down) when *all* progress markers are stale for a long window.
  if [[ "${work_due}" -eq 1 && "${stale_min:-999999}" -gt 30 ]]; then
    email_status="warn"
  fi
  if [[ "${work_due}" -eq 1 \
     && "${stale_min:-999999}" -gt 120 \
     && "${retry_stale_min:-999999}" -gt 120 \
     && "${lookup_stale_min:-999999}" -gt 120 ]]; then
    email_status="fail"
  fi
fi
add_check "email_activity" "Email Pipeline (activity)" "${email_status}" "${email_details}" ""

# Export metrics for the JSON payload (python formats/escapes safely).
export METRIC_BACKUP_LAST_TS="${backup_last_ts}"
export METRIC_BACKUP_AGE_HOURS="${backup_age_hours}"
export METRIC_LAST_INGESTED_TS="${last_ingested}"
export METRIC_LAST_ANALYZED_TS="${last_analyzed}"
export METRIC_EMAIL_STALE_MIN="${stale_min}"
export METRIC_EMAIL_DUE_RETRIES="${due_retries}"
export METRIC_EMAIL_BRANDLESS_PENDING="${brandless_physical_with_inferred}"
export METRIC_EMAIL_RECIPIENTS_1H="${recipients_1h}"
export METRIC_EMAIL_RECIPIENTS_24H="${recipients_24h}"
export METRIC_EMAIL_REPORTS_PROCESSED_1H="${processed_1h}"
export METRIC_EMAIL_REPORTS_PROCESSED_24H="${processed_24h}"

# --- Build JSON (python does safe escaping/encoding) ---
python3 - "${checks_file}" <<'PY' >"${out_tmp}"
import json
import os
import sys
from datetime import datetime, timezone

checks_path = sys.argv[1]

def env_int(k: str):
    v = os.environ.get(k, "")
    try:
        return int(v)
    except Exception:
        return None

def now_utc_iso():
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

checks = []
with open(checks_path, "r", encoding="utf-8") as f:
    for raw in f:
        raw = raw.rstrip("\n")
        if not raw:
            continue
        parts = raw.split("|", 4)
        if len(parts) != 5:
            continue
        cid, name, status, details, link = parts
        checks.append(
            {
                "id": cid,
                "name": name,
                "status": status,
                "details": details,
                "link": link or None,
            }
        )

overall = "ok"
if any(c["status"] == "fail" for c in checks):
    overall = "down"
elif any(c["status"] == "warn" for c in checks):
    overall = "degraded"

payload = {
    "generated_at": now_utc_iso(),
    "overall_status": overall,
    "checks": checks,
    "metrics": {
        "backup_last_complete": os.environ.get("METRIC_BACKUP_LAST_TS") or None,
        "backup_age_hours": env_int("METRIC_BACKUP_AGE_HOURS"),
        "last_report_ingested": os.environ.get("METRIC_LAST_INGESTED_TS") or None,
        "last_report_analyzed": os.environ.get("METRIC_LAST_ANALYZED_TS") or None,
        "email": {
            "stale_min": env_int("METRIC_EMAIL_STALE_MIN"),
            "due_retries": env_int("METRIC_EMAIL_DUE_RETRIES"),
            "brandless_pending_with_inferred": env_int("METRIC_EMAIL_BRANDLESS_PENDING"),
            "recipients_sent_1h": env_int("METRIC_EMAIL_RECIPIENTS_1H"),
            "recipients_sent_24h": env_int("METRIC_EMAIL_RECIPIENTS_24H"),
            "reports_processed_1h": env_int("METRIC_EMAIL_REPORTS_PROCESSED_1H"),
            "reports_processed_24h": env_int("METRIC_EMAIL_REPORTS_PROCESSED_24H"),
        },
    },
    "notes": {
        "refresh_seconds": 300,
        "source": "vm_watchdog",
    },
}
print(json.dumps(payload, separators=(",", ":"), ensure_ascii=False))
PY

sudo -n install -m 0644 "${out_tmp}" "${OUT_JSON}" >/dev/null 2>&1 || sudo -n cp -f "${out_tmp}" "${OUT_JSON}"

echo "[watchdog public] wrote ${OUT_JSON} at ${ts}"
