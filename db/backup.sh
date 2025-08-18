#!/bin/bash

set -e

OPT=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

if [ -z "${OPT}" ]; then
  echo "Usage: ./backup.sh -e <dev|prod>"
  exit 1
fi

SECRET_SUFFIX=$(echo ${OPT} | tr '[a-z]' '[A-Z]')
MYSQL_ROOT_PASSWORD=$(gcloud secrets versions access 1 --secret="MYSQL_ROOT_PASSWORD_${SECRET_SUFFIX}")

# Config
MYSQL_USER="root"
MYSQL_PASSWORD="${MYSQL_ROOT_PASSWORD}"
BACKUP_DIR="backups/mysql"
DATE=$(date +"%Y-%m-%d_%H-%M")
RETENTION_DAYS=7

# Extract the Docker container id of the mysql container
MYSQL_CONTAINER_ID=$(docker ps -q --filter "name=cleanapp_db")
if [ -z "${MYSQL_CONTAINER_ID}" ]; then
  echo "MySQL container not found"
  exit 1
fi

# Ensure backup directory exists
mkdir -p "${BACKUP_DIR}"

BACKUP_FILE="mysql_backup_${OPT}_$DATE.sql.gz"
# Dump all databases into a compressed file
docker exec -i "${MYSQL_CONTAINER_ID}" mysqldump -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" --all-databases --single-transaction --quick --lock-tables=false \
  | gzip > "${BACKUP_DIR}/${BACKUP_FILE}"

# Put the backup into a Google cloud storage bucket
gcloud storage cp "${BACKUP_DIR}/${BACKUP_FILE}" gs://cleanapp_mysql_backup_${OPT}

# Remove successfully uploaded backup
rm "${BACKUP_DIR}/${BACKUP_FILE}"
