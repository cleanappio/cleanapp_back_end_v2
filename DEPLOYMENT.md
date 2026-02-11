# CleanApp Production Deployment Guide

## Overview

This document describes the production deployment configuration for CleanApp services, including port mappings, environment variables, and common troubleshooting steps.

## Production Server

- **IP**: 34.122.15.16
- **User**: deployer
- **Network**: deployer_default (Docker bridge network)

## Service Port Mappings

| Service | Container Port | Host Port | Nginx Proxy |
|---------|----------------|-----------|-------------|
| cleanapp_service | 8080 | 8079 | api.cleanapp.io:443 → 127.0.0.1:8079 |
| cleanapp_report_listener | 8080 | 9081 | live.cleanapp.io/api/v3/* |
| cleanapp_report_listener_v4 | 8080 | 9097 | live.cleanapp.io/api/v4/* |
| cleanapp_customer_service | 8080 | 9080 | api.cleanapp.io:443 → 127.0.0.1:9080 |
| cleanapp_db | 3306 | 3306 | Internal only |
| cleanapp_frontend | 3000 | 3001 | cleanapp.io:443 |
| cleanapp_auth_service | 8080 | 9084 | auth.cleanapp.io |
| cleanapp_areas_service | 8080 | 9086 | areas.cleanapp.io |
| cleanapp_report_processor | 8080 | 9087 | processing.cleanapp.io |

> **IMPORTANT**: Port 8080 on the host is used by nginx for HTTP listener. Do NOT map any container to host port 8080 directly.

## Nginx Configuration

Nginx configs are stored in `/etc/nginx/conf.d/`. Key files:

- `apicleanapp.conf` - Routes for api.cleanapp.io
- `livecleanapp.conf` - Routes for live.cleanapp.io (report-listener APIs)
- `cleanapp.conf` - Routes for cleanapp.io (frontend)

After modifying nginx config:
```bash
sudo nginx -t && sudo systemctl reload nginx
```

## Manual Container Deployment

When docker-compose fails (e.g., port conflicts), deploy containers manually:

### Report Listener
```bash
sudo docker stop cleanapp_report_listener
sudo docker rm cleanapp_report_listener
sudo docker pull us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-listener-image:prod
sudo docker run -d --name cleanapp_report_listener \
  --network deployer_default \
  -p 9081:8080 \
  -e DB_HOST=cleanapp_db \
  -e DB_PORT=3306 \
  -e DB_USER=server \
  -e "DB_PASSWORD=$MYSQL_APP_PASSWORD" \
  -e DB_NAME=cleanapp \
  us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-report-listener-image:prod
```

### CleanApp Service (Mobile Backend)
```bash
sudo docker stop cleanapp_service
sudo docker rm cleanapp_service
# Note: Uses port 8079 to avoid conflict with nginx on 8080
sudo docker run -d --name cleanapp_service \
  --network deployer_default \
  -p 8079:8080 \
  -e "MYSQL_APP_PASSWORD=$MYSQL_APP_PASSWORD" \
  -e "ETH_NETWORK_URL_MAIN=https://sepolia.base.org" \
  -e "KITN_PRIVATE_KEY_MAIN=$KITN_PRIVATE_KEY_MAIN" \
  -e "CONTRACT_ADDRESS_MAIN=0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA" \
  us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-service-image:prod
```

## Common Issues & Fixes

### 1. "Address already in use" Port Conflict

Check what's using the port:
```bash
sudo netstat -tlnp | grep <PORT>
sudo docker ps | grep <PORT>
```

Either stop the conflicting service or use a different port.

### 2. Slow Database Queries (COUNT DISTINCT)

The `cleanapp_service` has a `/valid-reports-count` endpoint that runs slow COUNT(DISTINCT) queries on 1.25M rows. This can overwhelm the database.

**Temporary fix**: Kill slow queries and restart affected services:
```bash
PASS=$(grep MYSQL_APP_PASSWORD /home/deployer/.env | cut -d= -f2)
for pid in $(sudo docker exec cleanapp_db mysql -u server -p"$PASS" cleanapp -sNe \
  "SELECT id FROM information_schema.processlist WHERE info LIKE '%COUNT(DISTINCT%'"); do
  sudo docker exec cleanapp_db mysql -u server -p"$PASS" cleanapp -e "KILL $pid"
done
```

**Permanent fix**: The frontend now uses static counters (v1.0.403+) to avoid calling this endpoint.

### 3. Service Health Check "unhealthy"

Check logs for the service:
```bash
sudo docker logs cleanapp_report_listener --tail 50
```

Common causes:
- Database connection timeout (port 3306 not reachable)
- RabbitMQ connection failure (can be ignored, just a warning)
- Slow startup queries blocking health check

### 4. 502 Bad Gateway

Nginx can't reach the backend. Check:
1. Is the container running? `sudo docker ps | grep <service>`
2. Is it on the correct port? Check host port in `docker ps` output
3. Does nginx point to the right port? Check `/etc/nginx/conf.d/*.conf`

## Image Versioning

Images are stored in Google Artifact Registry:
```
us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/
```

The `:prod` tag is used for production. To rollback:
```bash
# List available versions
gcloud artifacts docker images list \
  us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-<service>-image \
  --include-tags

# Re-tag a previous version as :prod
gcloud artifacts docker tags add \
  us-central1-docker.pkg.dev/.../cleanapp-<service>-image:<VERSION> \
  us-central1-docker.pkg.dev/.../cleanapp-<service>-image:prod
```

## Environment Variables

Environment variables are stored in `/home/deployer/.env`. Load them before running commands:
```bash
source /home/deployer/.env
# Or for individual vars:
PASS=$(grep MYSQL_APP_PASSWORD /home/deployer/.env | cut -d= -f2)
```

**Never commit .env files or print passwords in logs.**

Also avoid passing passwords on the command line (they can show up in `ps` output). Prefer piping
secrets via stdin into a short-lived file *inside* the container when you need to run `mysql`.

## Quick Health Check

```bash
# Check all services
sudo docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# Check API health
curl -s https://live.cleanapp.io/api/v3/reports/health | jq

# Check database connections
PASS="$(gcloud secrets versions access latest --secret=MYSQL_APP_PASSWORD_PROD)"
printf '%s' "$PASS" | sudo docker exec -i cleanapp_db sh -lc '
  set -euo pipefail
  pwfile="$(mktemp)"
  cat >"$pwfile"
  chmod 600 "$pwfile"
  MYSQL_PWD="$(cat "$pwfile")" mysql -u server cleanapp -e "SELECT COUNT(*) as connections FROM information_schema.processlist;"
  rm -f "$pwfile"
'
```

## Database Backups (Prod)

CleanApp stores full MySQL backups in GCS:
- Prod bucket: `gs://cleanapp_mysql_backup_prod`
- Current object key (versioned): `gs://cleanapp_mysql_backup_prod/current/cleanapp_all.sql.gz`
- Weekly pins (kept ~30 weeks): `gs://cleanapp_mysql_backup_prod/weekly/<ISO_WEEK>/cleanapp_all.sql.gz`

### Restore (One-Liner)

Restore the current backup into the running prod DB container:

```bash
PASS="$(gcloud secrets versions access latest --secret=MYSQL_ROOT_PASSWORD_PROD)"
printf '%s' "$PASS" | sudo docker exec -i cleanapp_db sh -lc 'cat > /tmp/.restore_pw && chmod 600 /tmp/.restore_pw' && \
  gsutil cat gs://cleanapp_mysql_backup_prod/current/cleanapp_all.sql.gz | gunzip -c | sudo docker exec -i cleanapp_db sh -lc 'MYSQL_PWD="$(cat /tmp/.restore_pw)" exec mysql -uroot' && \
  sudo docker exec cleanapp_db sh -lc 'rm -f /tmp/.restore_pw'
```

### Restore Drill (Recommended)

To prove backups are restorable, run a restore drill into a scratch MySQL container on the prod VM:

```bash
HOST=deployer@34.122.15.16 ./platform_blueprint/ops/db_backup/restore_drill_prod_vm.sh
```

Notes:
- The drill compares restored row counts vs backup metadata with a small tolerance (default `0.2%`)
  to avoid false negatives during high-write windows.
- Override with `ROW_COUNT_TOLERANCE_PCT=<pct>` when needed.
