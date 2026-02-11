#!/usr/bin/env bash
# Restore drill: restore the latest backup object into a scratch MySQL container and
# compare row counts against the backup metadata.
#
# WARNING: This can take a long time and consume significant disk IO/space.
set -euo pipefail

HOST="${HOST:-deployer@34.122.15.16}"
ENV_NAME="${ENV_NAME:-prod}"

if [[ "${ENV_NAME}" != "prod" && "${ENV_NAME}" != "dev" ]]; then
  echo "ENV_NAME must be prod|dev" >&2
  exit 2
fi

bucket="gs://cleanapp_mysql_backup_${ENV_NAME}"
obj_sql="${bucket}/current/cleanapp_all.sql.gz"
obj_meta="${bucket}/current/cleanapp_all.metadata.json"

ssh "$HOST" "ENV_NAME='${ENV_NAME}' OBJ_SQL='${obj_sql}' OBJ_META='${obj_meta}' bash -s" <<'REMOTE'
set -euo pipefail

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing $1" >&2; exit 1; }; }
need gsutil
need sudo
need docker
need python3

if ! sudo -n true 2>/dev/null; then
  echo "sudo requires password on VM; cannot run restore drill" >&2
  exit 1
fi

env_name="${ENV_NAME}"
obj_sql="${OBJ_SQL}"
obj_meta="${OBJ_META}"

echo "== restore drill: env=${env_name} obj=${obj_sql} =="

if ! gsutil -q stat "${obj_sql}"; then
  echo "backup object missing: ${obj_sql}" >&2
  exit 2
fi
if ! gsutil -q stat "${obj_meta}"; then
  echo "metadata object missing: ${obj_meta}" >&2
  exit 2
fi

expected="$(gsutil cat "${obj_meta}")"
exp_reports="$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("row_counts",{}).get("reports",0))' <<<"$expected")"
exp_analysis="$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("row_counts",{}).get("report_analysis",0))' <<<"$expected")"
echo "expected counts: reports=${exp_reports} report_analysis=${exp_analysis}"

ts="$(date -u +%Y%m%dT%H%M%SZ)"
name="cleanapp_db_restore_drill_${ts}"
vol="eko_mysql_restore_drill_${ts}"

root_pw="$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
envfile="/tmp/${name}.env"

echo "== create scratch mysql container =="
sudo docker volume create "${vol}" >/dev/null
umask 077
printf 'MYSQL_ROOT_PASSWORD=%s\n' "${root_pw}" >"${envfile}"

sudo docker run -d --name "${name}" --env-file "${envfile}" \
  -p 127.0.0.1:3307:3306 \
  -v "${vol}":/var/lib/mysql \
  mysql:8.0 \
  --default-authentication-plugin=mysql_native_password \
  --innodb_flush_log_at_trx_commit=2 \
  --sync_binlog=0 >/dev/null

cleanup() {
  echo "== cleanup =="
  sudo docker rm -f "${name}" >/dev/null 2>&1 || true
  sudo docker volume rm "${vol}" >/dev/null 2>&1 || true
  rm -f "${envfile}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== wait for mysql ready =="
for i in $(seq 1 120); do
  # Use container env var to avoid putting secrets in host-visible process args.
  if sudo docker exec "${name}" sh -lc 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e "SELECT 1" >/dev/null 2>&1'; then
    break
  fi
  sleep 2
  if [[ "$i" -eq 120 ]]; then
    echo "mysql did not become ready" >&2
    exit 1
  fi
done

echo "== stream restore (this can take a long time) =="
gsutil cat "${obj_sql}" | gunzip -c | sudo docker exec -i "${name}" sh -lc 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD"'

echo "== verify counts =="
got_reports="$(sudo docker exec "${name}" sh -lc 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -e \"SELECT COUNT(*) FROM cleanapp.reports\" 2>/dev/null' | tr -d '\r' | tail -n 1)"
got_analysis="$(sudo docker exec "${name}" sh -lc 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -e \"SELECT COUNT(*) FROM cleanapp.report_analysis\" 2>/dev/null' | tr -d '\r' | tail -n 1)"

echo "restored counts: reports=${got_reports} report_analysis=${got_analysis}"

if [[ "${got_reports}" != "${exp_reports}" || "${got_analysis}" != "${exp_analysis}" ]]; then
  echo "ERROR restore drill mismatch vs metadata" >&2
  exit 3
fi

echo "OK restore drill: counts match metadata"
REMOTE
