#!/usr/bin/env bash

set -euo pipefail

# Deploys a MySQL replica on cleanapp-prod2 and initializes it using either:
# - CLONE plugin with NO RESTART, then manual container restart, or
# - Percona XtraBackup streamed seed (mode xtrabackup)
# - streamed mysqldump (fallback or when --mode dump)
# Finally enables GTID-based replication.
#
# Usage:
#   ./setup-replica.sh -s <dev|prod> [--mode <clone|dump|xtrabackup>] [--ssh-keyfile <path>]
#
# Assumes:
# - Target host is cleanapp-prod2 (35.238.248.151) with deployer SSH and docker
# - Source host is reachable on port 3306 over the VPC

OPT=""
MODE="xtrabackup"
SSH_KEYFILE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-s"|"--source")
      OPT="$2"; shift 2 ;;
    "--mode")
      MODE="$2"; shift 2 ;;
    "--ssh-keyfile")
      SSH_KEYFILE="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

if [[ -z "${OPT}" ]]; then
  echo "Usage: $0 -s <dev|prod> [--mode <clone|dump|xtrabackup>] [--ssh-keyfile <path>]"
  exit 1
fi

TARGET_HOST=35.238.248.151
REPLICA_INTERNAL_IP=10.128.0.13
TARGET_SSH_IP=${REPLICA_INTERNAL_IP}

echo "Selected mode: ${MODE}"

echo "Preparing target host and volume..."

case ${OPT} in
  dev)
    SRC_INTERNAL_IP=10.128.0.9
    SRC_HOST=34.132.121.53
    VOLUME_NAME=eko_mysql_replica_dev
    ;;
  prod)
    SRC_INTERNAL_IP=10.128.0.6
    SRC_HOST=34.122.15.16
    VOLUME_NAME=eko_mysql_replica_prod
    ;;
  *) echo "Invalid source: ${OPT}"; exit 1 ;;
esac

SECRET_SUFFIX=$(echo ${OPT} | tr '[a-z]' '[A-Z]')

DOCKER_LOCATION="us-central1-docker.pkg.dev"
DOCKER_PREFIX="${DOCKER_LOCATION}/cleanup-mysql-v2/cleanapp-docker-repo"
BASE_DB_IMAGE="${DOCKER_PREFIX}/cleanapp-db-image:live"
DB_DOCKER_IMAGE="${BASE_DB_IMAGE}"

# Secrets
MYSQL_ROOT_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_ROOT_PASSWORD_${SECRET_SUFFIX}" | tr -d '\r')
MYSQL_APP_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_APP_PASSWORD_${SECRET_SUFFIX}" | tr -d '\r')
MYSQL_READER_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_READER_PASSWORD_${SECRET_SUFFIX}" | tr -d '\r')
MYSQL_REPL_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_REPL_PASSWORD_${SECRET_SUFFIX}" | tr -d '\r')
MYSQL_CLONE_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_CLONE_PASSWORD_${SECRET_SUFFIX}" | tr -d '\r')

keyflag=( )
if [[ -n "${SSH_KEYFILE}" ]]; then keyflag=( -i "${SSH_KEYFILE}" ); fi

echo "Logging into Artifact Registry on target host..."
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'ACCESS_TOKEN=\$(gcloud auth print-access-token); echo \"\${ACCESS_TOKEN}\" | docker login -u oauth2accesstoken --password-stdin https://${DOCKER_LOCATION}'"

echo "Pulling DB image on target host..."
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker pull ${DB_DOCKER_IMAGE}"

echo "Creating replica .env and docker-compose.yml on target host..."
ENV_CONTENT=$(printf "MYSQL_ROOT_PASSWORD=%s\nMYSQL_APP_PASSWORD=%s\nMYSQL_READER_PASSWORD=%s\n" \
  "${MYSQL_ROOT_PASSWORD}" "${MYSQL_APP_PASSWORD}" "${MYSQL_READER_PASSWORD}")
printf "%s" "${ENV_CONTENT}" | ssh "${keyflag[@]}" deployer@${TARGET_HOST} "cat > ~/.env.replica"

COMPOSE_CONTENT=$(cat <<'YML'
version: '3'

services:
  cleanapp_db:
    container_name: cleanapp_db
    image: ${DB_IMAGE}
    restart: unless-stopped
    command:
      - --server-id=1013
      - --log-bin=mysql-bin
      - --binlog_format=ROW
      - --enforce_gtid_consistency=ON
      - --gtid_mode=ON
      - --log_replica_updates=ON
    environment:
      - MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD}
      - MYSQL_APP_PASSWORD=${MYSQL_APP_PASSWORD}
      - MYSQL_READER_PASSWORD=${MYSQL_READER_PASSWORD}
    volumes:
      - mysql:/var/lib/mysql
    ports:
      - 3306:3306

volumes:
  mysql:
    name: ${VOLUME_NAME}
    external: true
YML
)
printf "%s" "${COMPOSE_CONTENT}" | sed "s|\${DB_IMAGE}|${DB_DOCKER_IMAGE}|g; s|\${VOLUME_NAME}|${VOLUME_NAME}|g" | ssh "${keyflag[@]}" deployer@${TARGET_HOST} "cat > ~/docker-compose.replica.yml"

echo "Ensuring dedicated external volume exists (${VOLUME_NAME})..."
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'docker volume inspect ${VOLUME_NAME} >/dev/null 2>&1 || docker volume create ${VOLUME_NAME} >/dev/null'"

## clone and dump modes removed; xtrabackup is the supported path

if [[ "${MODE}" == "xtrabackup" ]]; then
  echo "Using Percona XtraBackup streaming seed..."
  TOTAL_START_TS=$(date +%s)
  XBK_PARALLEL=1

  echo "Stopping MySQL on replica (if running) and wiping existing datadir..."
  ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker stop cleanapp_db >/dev/null 2>&1 || true"
  ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker run --rm --mount source=${VOLUME_NAME},target=/var/lib/mysql alpine sh -lc 'set -x; rm -rf /var/lib/mysql/* && mkdir -p /var/lib/mysql/seed && chown -R root:root /var/lib/mysql'"

  echo "Starting donor stream (this can take hours)..."
  STREAM_START_TS=$(date +%s)
REMOTE_SCRIPT=$(cat <<EOF
#!/usr/bin/env bash
set -euo pipefail
set -x
KEYFILE=\$(mktemp -p /tmp .xtrabackup_key.XXXXXX)
trap 'rm -f "\${KEYFILE}"' EXIT
umask 077
gcloud secrets versions access 1 --secret="XTRABACKUP_SSH_KEY_${SECRET_SUFFIX}" > "\${KEYFILE}"
chmod 600 "\${KEYFILE}"

docker run --rm --network host --mount source=eko_mysql,target=/var/lib/mysql,ro -u 0:0 percona/percona-xtrabackup:8.0 sh -lc "xtrabackup --backup --datadir=/var/lib/mysql --stream=xbstream --parallel=${XBK_PARALLEL} --host=127.0.0.1 --port=3306 --tmpdir=/tmp --user=root --password='${MYSQL_ROOT_PASSWORD}' 2> /tmp/xtrabackup.log" \
  | ssh -i "\${KEYFILE}" -o StrictHostKeyChecking=no deployer@${REPLICA_INTERNAL_IP}
EOF
)
  ssh "${keyflag[@]}" deployer@${SRC_HOST} "cat > /tmp/run_xtrabackup_stream.sh <<'EOS'
${REMOTE_SCRIPT}
EOS
chmod +x /tmp/run_xtrabackup_stream.sh && /tmp/run_xtrabackup_stream.sh"
  STREAM_END_TS=$(date +%s)
  STREAM_SECS=$((STREAM_END_TS-STREAM_START_TS))
  echo "XtraBackup stream duration: ${STREAM_SECS}s"

  echo "Preparing backup on replica..."
  PREPARE_START_TS=$(date +%s)
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker run --rm --network host --mount source=${VOLUME_NAME},target=/var/lib/mysql -u 0:0 percona/percona-xtrabackup:8.0 xtrabackup --prepare --target-dir=/var/lib/mysql/seed --parallel=${XBK_PARALLEL}"
  PREPARE_END_TS=$(date +%s)
  PREPARE_SECS=$((PREPARE_END_TS-PREPARE_START_TS))
  echo "Prepare duration: ${PREPARE_SECS}s"

  echo "Moving prepared seed into datadir and fixing ownership..."
  MOVE_START_TS=$(date +%s)
  ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker run --rm --mount source=${VOLUME_NAME},target=/var/lib/mysql alpine sh -lc 'set -e; if [ -d /var/lib/mysql/seed ]; then find /var/lib/mysql/seed -mindepth 1 -maxdepth 1 -exec mv -t /var/lib/mysql {} +; rmdir /var/lib/mysql/seed || true; fi; chown -R 999:999 /var/lib/mysql'"
  MOVE_END_TS=$(date +%s)
  MOVE_SECS=$((MOVE_END_TS-MOVE_START_TS))
  echo "Move duration: ${MOVE_SECS}s"

  echo "Starting MySQL on replica..."
  RESTART_START_TS=$(date +%s)
  ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker compose -f ~/docker-compose.replica.yml --env-file ~/.env.replica up -d cleanapp_db"

  echo "Waiting for MySQL to come back after datadir swap..."
  ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'for i in \$(seq 1 300); do if docker exec cleanapp_db mysqladmin ping -h127.0.0.1 -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --silent; then exit 0; fi; sleep 2; done; exit 1'"
  RESTART_END_TS=$(date +%s)
  RESTART_SECS=$((RESTART_END_TS-RESTART_START_TS))
  echo "MySQL restart wait duration: ${RESTART_SECS}s"
  TOTAL_END_TS=$(date +%s)
  TOTAL_SECS=$((TOTAL_END_TS-TOTAL_START_TS))
  echo "Total xtrabackup flow duration: ${TOTAL_SECS}s"
fi

if [[ "${MODE}" == "dump" ]]; then
  echo "Using mysqldump-based initial sync..."
  ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'until docker exec cleanapp_db mysqladmin ping -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --silent; do sleep 2; done'"
  DUMP_CMD="docker exec cleanapp_db mysqldump -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --single-transaction --routines --events --triggers --set-gtid-purged=ON --databases cleanapp"
  IMPORT_CMD="docker exec -i cleanapp_db mysql -uroot -p\"${MYSQL_ROOT_PASSWORD}\""
  if [[ -n "${SSH_KEYFILE}" ]]; then
    ssh -i "${SSH_KEYFILE}" deployer@${SRC_HOST} "${DUMP_CMD}" | ssh -i "${SSH_KEYFILE}" deployer@${TARGET_HOST} "${IMPORT_CMD}"
  else
    ssh deployer@${SRC_HOST} "${DUMP_CMD}" | ssh deployer@${TARGET_HOST} "${IMPORT_CMD}"
  fi
fi

echo "Configuring GTID replication on replica..."
CHANGE_STMT=$(cat <<SQL
CHANGE REPLICATION SOURCE TO
  SOURCE_HOST='${SRC_INTERNAL_IP}',
  SOURCE_PORT=3306,
  SOURCE_USER='replicator',
  SOURCE_PASSWORD='${MYSQL_REPL_PASSWORD}',
  SOURCE_AUTO_POSITION=1,
  GET_SOURCE_PUBLIC_KEY=1;
SQL
)

ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker exec -i cleanapp_db mysql -h127.0.0.1 -uroot -p\"${MYSQL_ROOT_PASSWORD}\" -e \"${CHANGE_STMT} START REPLICA;\""

echo "Replication status:"
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "docker exec -i cleanapp_db mysql -h127.0.0.1 -uroot -p\"${MYSQL_ROOT_PASSWORD}\" -e \"SHOW REPLICA STATUS\\G\" | sed -n '1,120p'"

echo "Replica deployment and replication configured successfully (source: ${OPT}, mode: ${MODE})."

