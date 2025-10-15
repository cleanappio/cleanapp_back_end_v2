#!/usr/bin/env bash

set -euo pipefail

# Deploys a MySQL replica on cleanapp-prod2 and initializes it using
# a streamed mysqldump from the selected source (dev/prod), then enables
# GTID-based replication. CLONE path is attempted first but skipped when
# unsupported by the container runtime.
#
# Usage:
#   ./setup-replica.sh -s <dev|prod> [--ssh-keyfile <path>]
#
# Assumes:
# - Target host is cleanapp-prod2 (35.238.248.151) with deployer SSH and docker
# - Source host is reachable on port 3306 over the VPC

OPT=""
SSH_KEYFILE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-s"|"--source")
      OPT="$2"; shift 2 ;;
    "--ssh-keyfile")
      SSH_KEYFILE="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

if [[ -z "${OPT}" ]]; then
  echo "Usage: $0 -s <dev|prod> [--ssh-keyfile <path>]"
  exit 1
fi

TARGET_HOST=35.238.248.151
REPLICA_INTERNAL_IP=10.128.0.13

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
DB_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-db-image:live"

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

echo "(Re)Starting replica DB container with fresh volume..."
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'docker compose -f ~/docker-compose.replica.yml --env-file ~/.env.replica up -d cleanapp_db'"

echo "Waiting for MySQL to be ready on replica..."
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'until docker exec cleanapp_db mysqladmin ping -h127.0.0.1 -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --silent; do sleep 2; done'"

# Skip CLONE for dev (containerized mysqld cannot restart under clone)
echo "Skipping CLONE and using mysqldump-based initial sync..."

# Ensure target is ready for import
ssh "${keyflag[@]}" deployer@${TARGET_HOST} "bash -lc 'until docker exec cleanapp_db mysqladmin ping -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --silent; do sleep 2; done'"

# Stream a GTID-aware dump of 'cleanapp' from source into the replica
DUMP_CMD="docker exec cleanapp_db mysqldump -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --single-transaction --routines --events --triggers --set-gtid-purged=ON --databases cleanapp"
IMPORT_CMD="docker exec -i cleanapp_db mysql -uroot -p\"${MYSQL_ROOT_PASSWORD}\""
if [[ -n "${SSH_KEYFILE}" ]]; then
  ssh -i "${SSH_KEYFILE}" deployer@${SRC_HOST} "${DUMP_CMD}" | ssh -i "${SSH_KEYFILE}" deployer@${TARGET_HOST} "${IMPORT_CMD}"
else
  ssh deployer@${SRC_HOST} "${DUMP_CMD}" | ssh deployer@${TARGET_HOST} "${IMPORT_CMD}"
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

echo "Replica deployment and replication configured successfully (source: ${OPT})."

