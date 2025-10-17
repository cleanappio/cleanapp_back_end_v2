#!/usr/bin/env bash

set -euo pipefail

# Prepare a source MySQL (dev/prod) to act as a replication donor and CLONE donor.
# - Enables GTID + binlog via docker-compose override and restarts DB container
# - Installs CLONE plugin
# - Creates 'replicator' and 'cloner' users scoped to the replica host
#
# Usage:
#   ./prepare-source-for-replication.sh -s <dev|prod> [--ssh-keyfile <path>]
#
# Requirements on the source host:
#   - Existing cleanapp docker-compose deployment with service 'cleanapp_db'
#   - Deployer user with docker permissions and SSH access

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

case ${OPT} in
  dev)
    SRC_HOST=34.132.121.53
    SRC_INTERNAL_IP=10.128.0.9
    SERVER_ID=1009
    ;;
  prod)
    SRC_HOST=34.122.15.16
    SRC_INTERNAL_IP=10.128.0.6
    SERVER_ID=1006
    ;;
  *) echo "Invalid source: ${OPT}"; exit 1 ;;
esac

SECRET_SUFFIX=$(echo ${OPT} | tr '[a-z]' '[A-Z]')
REPL_SECRET_NAME="MYSQL_REPL_PASSWORD_${SECRET_SUFFIX}"
CLONE_SECRET_NAME="MYSQL_CLONE_PASSWORD_${SECRET_SUFFIX}"

# Fetch secrets locally so we can pass into remote commands safely
MYSQL_ROOT_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_ROOT_PASSWORD_${SECRET_SUFFIX}" | tr -d '\r')
MYSQL_REPL_PASSWORD=$(gcloud secrets versions access 1 --secret="${REPL_SECRET_NAME}" | tr -d '\r')
MYSQL_CLONE_PASSWORD=$(gcloud secrets versions access 1 --secret="${CLONE_SECRET_NAME}" | tr -d '\r')

OVERRIDE_FILE_CONTENT=$(cat <<EOF
version: '3'

services:
  cleanapp_db:
    command:
      - --server-id=${SERVER_ID}
      - --log-bin=mysql-bin
      - --binlog_format=ROW
      - --enforce_gtid_consistency=ON
      - --gtid_mode=ON
      - --log_replica_updates=ON
EOF
)

create_override_and_restart() {
  local keyflag=( )
  if [[ -n "${SSH_KEYFILE}" ]]; then keyflag=( -i "${SSH_KEYFILE}" ); fi

  # Upload override
  echo "Creating compose override on source host ${SRC_HOST}..."
  printf "%s" "${OVERRIDE_FILE_CONTENT}" | ssh "${keyflag[@]}" deployer@${SRC_HOST} "cat > ~/docker-compose.replication.override.yml"

  # Restart DB with override merged
  echo "Restarting cleanapp_db with replication settings..."
  ssh "${keyflag[@]}" deployer@${SRC_HOST} "set -e; docker compose -f docker-compose.yml -f docker-compose.replication.override.yml up -d cleanapp_db"
}

configure_source_mysql() {
  local keyflag=( )
  if [[ -n "${SSH_KEYFILE}" ]]; then keyflag=( -i "${SSH_KEYFILE}" ); fi

  # Wait for MySQL to be ready
  ssh "${keyflag[@]}" deployer@${SRC_HOST} "bash -lc 'until docker exec cleanapp_db mysqladmin ping -uroot -p\"${MYSQL_ROOT_PASSWORD}\" --silent; do sleep 2; done'"

  echo "Installing CLONE plugin on source (idempotent)..."
  ssh "${keyflag[@]}" deployer@${SRC_HOST} "docker exec -i cleanapp_db mysql -uroot -p\"${MYSQL_ROOT_PASSWORD}\" -e \"INSTALL PLUGIN clone SONAME 'mysql_clone.so';\" || true"

  echo "Creating/refreshing donor users on source..."
  ssh "${keyflag[@]}" deployer@${SRC_HOST} "docker exec -i cleanapp_db mysql -uroot -p\"${MYSQL_ROOT_PASSWORD}\" -e \"\\
    CREATE USER IF NOT EXISTS 'replicator'@'10.128.0.13' IDENTIFIED BY '${MYSQL_REPL_PASSWORD}';\\n\\
    GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'replicator'@'10.128.0.13';\\n\\
    CREATE USER IF NOT EXISTS 'cloner'@'10.128.0.13' IDENTIFIED BY '${MYSQL_CLONE_PASSWORD}';\\n\\
    GRANT BACKUP_ADMIN ON *.* TO 'cloner'@'10.128.0.13';\\n\\
    FLUSH PRIVILEGES;\\n\""

  echo "Verifying GTID/binlog state on source..."
  ssh "${keyflag[@]}" deployer@${SRC_HOST} "docker exec -i cleanapp_db mysql -uroot -p\"${MYSQL_ROOT_PASSWORD}\" -e \"SHOW VARIABLES LIKE 'gtid_mode'; SHOW VARIABLES LIKE 'log_bin';\""
}

create_override_and_restart
configure_source_mysql

echo "Source ${OPT} prepared for CLONE + GTID replication. Donor: ${SRC_INTERNAL_IP}"

