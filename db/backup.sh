#!/bin/bash
# =============================================================================
# CleanApp Database Backup Script
# Enterprise-grade backup with additive validation and anomaly detection
# =============================================================================
#
# Features:
#   - Full database backup via mysqldump
#   - Additive validation (row count + file size)
#   - Anomaly detection and preservation
#   - Lock file protection (prevents concurrent runs)
#   - Structured logging
#   - GCS storage with intelligent retention
#
# Usage:
#   ./backup.sh -e <dev|prod> [--force] [--dry-run]
#
# Options:
#   -e, --env       Environment (dev or prod) - REQUIRED
#   --force         Skip additive validation (use for first backup)
#   --dry-run       Run without uploading or deleting
#
# =============================================================================

set -euo pipefail

# -----------------------------------------------------------------------------
# Configuration
# -----------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_DIR="/tmp/cleanapp_backup"
LOCK_FILE="/tmp/cleanapp-backup.lock"
LOG_FILE="/var/log/cleanapp-backup.log"

# Validation thresholds
ROW_THRESHOLD_PERCENT=95    # New must be >= 95% of previous
SIZE_THRESHOLD_PERCENT=90   # New must be >= 90% of previous

# -----------------------------------------------------------------------------
# Logging
# -----------------------------------------------------------------------------
log() {
    local level="$1"
    shift
    local message="$*"
    local timestamp=$(date -Iseconds)
    echo "${timestamp} [${level}] ${message}" | tee -a "$LOG_FILE"
}

log_info()  { log "INFO" "$@"; }
log_warn()  { log "WARN" "$@"; }
log_error() { log "ERROR" "$@"; }

# -----------------------------------------------------------------------------
# Cleanup function
# -----------------------------------------------------------------------------
cleanup() {
    local exit_code=$?
    log_info "Cleaning up..."
    rm -f "$LOCK_FILE" 2>/dev/null || true
    rm -rf "$BACKUP_DIR" 2>/dev/null || true
    if [ $exit_code -ne 0 ]; then
        log_error "Backup failed with exit code $exit_code"
    fi
    exit $exit_code
}
trap cleanup EXIT

# -----------------------------------------------------------------------------
# Parse arguments
# -----------------------------------------------------------------------------
ENV=""
FORCE=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--env)
            ENV="$2"
            shift 2
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 -e <dev|prod> [--force] [--dry-run]"
            exit 1
            ;;
    esac
done

if [ -z "$ENV" ]; then
    echo "Usage: $0 -e <dev|prod> [--force] [--dry-run]"
    exit 1
fi

# -----------------------------------------------------------------------------
# Environment setup
# -----------------------------------------------------------------------------
SECRET_SUFFIX=$(echo "$ENV" | tr '[:lower:]' '[:upper:]')
GCS_BUCKET="gs://cleanapp_mysql_backup_${ENV}"

log_info "========================================"
log_info "Starting backup for environment: $ENV"
log_info "Force mode: $FORCE"
log_info "Dry run: $DRY_RUN"
log_info "========================================"

# -----------------------------------------------------------------------------
# Lock file check
# -----------------------------------------------------------------------------
if [ -f "$LOCK_FILE" ]; then
    PID=$(cat "$LOCK_FILE" 2>/dev/null || echo "")
    if [ -n "$PID" ] && ps -p "$PID" > /dev/null 2>&1; then
        log_error "Backup already running (PID $PID). Exiting."
        exit 1
    else
        log_warn "Stale lock file found, removing"
        rm -f "$LOCK_FILE"
    fi
fi
echo $$ > "$LOCK_FILE"
log_info "Lock acquired (PID $$)"

# -----------------------------------------------------------------------------
# Get MySQL credentials
# -----------------------------------------------------------------------------
log_info "Fetching MySQL credentials from Secret Manager..."
MYSQL_ROOT_PASSWORD=$(gcloud secrets versions access latest --secret="MYSQL_ROOT_PASSWORD_${SECRET_SUFFIX}" 2>/dev/null) || {
    log_error "Failed to fetch MySQL password from Secret Manager"
    exit 1
}
MYSQL_USER="root"
MYSQL_PASSWORD="$MYSQL_ROOT_PASSWORD"

# -----------------------------------------------------------------------------
# Find MySQL container
# -----------------------------------------------------------------------------
MYSQL_CONTAINER_ID=$(docker ps -q --filter "name=cleanapp_db")
if [ -z "$MYSQL_CONTAINER_ID" ]; then
    log_error "MySQL container not found"
    exit 1
fi
log_info "MySQL container found: $MYSQL_CONTAINER_ID"

# -----------------------------------------------------------------------------
# Create backup
# -----------------------------------------------------------------------------
mkdir -p "$BACKUP_DIR"
TIMESTAMP=$(date +"%Y-%m-%d_%H-%M-%S")
BACKUP_FILE="mysql_backup_${ENV}_${TIMESTAMP}.sql"
GZ_FILE="${BACKUP_FILE}.gz"

log_info "Starting mysqldump..."
DUMP_START=$(date +%s)

docker exec -i "$MYSQL_CONTAINER_ID" mysqldump \
    -u "$MYSQL_USER" \
    -p"$MYSQL_PASSWORD" \
    --all-databases \
    --single-transaction \
    --quick \
    --lock-tables=false \
    > "${BACKUP_DIR}/${BACKUP_FILE}" 2>/dev/null || {
    log_error "mysqldump failed"
    exit 1
}

DUMP_END=$(date +%s)
DUMP_DURATION=$((DUMP_END - DUMP_START))
log_info "mysqldump completed in ${DUMP_DURATION}s"

# -----------------------------------------------------------------------------
# Compress backup
# -----------------------------------------------------------------------------
log_info "Compressing backup..."
gzip "${BACKUP_DIR}/${BACKUP_FILE}"
GZ_PATH="${BACKUP_DIR}/${GZ_FILE}"

# Get file size
if [[ "$OSTYPE" == "darwin"* ]]; then
    NEW_SIZE=$(stat -f%z "$GZ_PATH")
else
    NEW_SIZE=$(stat -c%s "$GZ_PATH")
fi
NEW_SIZE_MB=$(echo "scale=2; $NEW_SIZE / 1024 / 1024" | bc)
log_info "Compressed size: ${NEW_SIZE_MB} MB"

# -----------------------------------------------------------------------------
# Count rows (approximate from INSERT statements)
# -----------------------------------------------------------------------------
log_info "Counting rows in backup..."
NEW_ROWS=$(zcat "$GZ_PATH" | grep -c "^INSERT INTO" || echo 0)
log_info "Total INSERT statements: $NEW_ROWS"

# -----------------------------------------------------------------------------
# Fetch previous metadata
# -----------------------------------------------------------------------------
PREV_METADATA="${BACKUP_DIR}/prev_metadata.json"
ANOMALY=false
ANOMALY_REASON=""

if gsutil -q stat "${GCS_BUCKET}/metadata.json" 2>/dev/null; then
    log_info "Fetching previous backup metadata..."
    gsutil cp "${GCS_BUCKET}/metadata.json" "$PREV_METADATA" 2>/dev/null || {
        log_warn "Failed to fetch metadata, treating as first backup"
        FORCE=true
    }
else
    log_info "No previous metadata found, treating as first backup"
    FORCE=true
fi

# -----------------------------------------------------------------------------
# Additive validation
# -----------------------------------------------------------------------------
if [ "$FORCE" = false ] && [ -f "$PREV_METADATA" ]; then
    PREV_ROWS=$(jq -r '.total_rows // 0' "$PREV_METADATA")
    PREV_SIZE=$(jq -r '.file_size_bytes // 0' "$PREV_METADATA")
    PREV_FILE=$(jq -r '.backup_file // ""' "$PREV_METADATA")
    
    log_info "Previous backup: $PREV_FILE"
    log_info "Previous rows: $PREV_ROWS, Current rows: $NEW_ROWS"
    log_info "Previous size: $PREV_SIZE bytes, Current size: $NEW_SIZE bytes"
    
    # Row count check
    if [ "$PREV_ROWS" -gt 0 ]; then
        ROW_PERCENT=$((NEW_ROWS * 100 / PREV_ROWS))
        if [ "$ROW_PERCENT" -lt "$ROW_THRESHOLD_PERCENT" ]; then
            ANOMALY=true
            ANOMALY_REASON="Row count decreased: $PREV_ROWS → $NEW_ROWS (-$((100 - ROW_PERCENT))%)"
            log_warn "ANOMALY DETECTED: $ANOMALY_REASON"
        else
            log_info "Row count check PASSED ($ROW_PERCENT% of previous)"
        fi
    fi
    
    # Size check
    if [ "$PREV_SIZE" -gt 0 ]; then
        SIZE_PERCENT=$((NEW_SIZE * 100 / PREV_SIZE))
        if [ "$SIZE_PERCENT" -lt "$SIZE_THRESHOLD_PERCENT" ]; then
            ANOMALY=true
            if [ -n "$ANOMALY_REASON" ]; then
                ANOMALY_REASON="${ANOMALY_REASON}; "
            fi
            ANOMALY_REASON="${ANOMALY_REASON}File size decreased: $PREV_SIZE → $NEW_SIZE bytes (-$((100 - SIZE_PERCENT))%)"
            log_warn "ANOMALY DETECTED: File size decreased"
        else
            log_info "Size check PASSED ($SIZE_PERCENT% of previous)"
        fi
    fi
else
    log_info "Skipping validation (force mode or first backup)"
fi

# -----------------------------------------------------------------------------
# Upload to GCS
# -----------------------------------------------------------------------------
if [ "$DRY_RUN" = true ]; then
    log_info "[DRY RUN] Would upload to GCS"
else
    if [ "$ANOMALY" = true ]; then
        # Upload as anomaly, keep previous
        DEST_PATH="${GCS_BUCKET}/anomalies/${GZ_FILE%.gz}_ANOMALY.sql.gz"
        log_warn "Uploading as ANOMALY backup (previous preserved)"
        gsutil cp "$GZ_PATH" "$DEST_PATH" || {
            log_error "Failed to upload anomaly backup"
            exit 1
        }
        log_warn "=========================================="
        log_warn "ANOMALY BACKUP SAVED"
        log_warn "Reason: $ANOMALY_REASON"
        log_warn "Location: $DEST_PATH"
        log_warn "ACTION REQUIRED: Investigate before next backup"
        log_warn "=========================================="
    else
        # Normal backup - upload to current/, delete previous
        DEST_PATH="${GCS_BUCKET}/current/${GZ_FILE}"
        log_info "Uploading to current backup location..."
        gsutil cp "$GZ_PATH" "$DEST_PATH" || {
            log_error "Failed to upload backup"
            exit 1
        }
        
        # Delete previous backup (only after successful upload)
        if [ -f "$PREV_METADATA" ]; then
            PREV_FILE=$(jq -r '.backup_file // ""' "$PREV_METADATA")
            if [ -n "$PREV_FILE" ] && gsutil -q stat "${GCS_BUCKET}/current/${PREV_FILE}" 2>/dev/null; then
                log_info "Deleting previous backup: $PREV_FILE"
                gsutil rm "${GCS_BUCKET}/current/${PREV_FILE}" || log_warn "Failed to delete previous backup"
            fi
        fi
        log_info "Backup uploaded successfully: $DEST_PATH"
    fi
    
    # Update metadata
    log_info "Updating metadata..."
    cat > "${BACKUP_DIR}/metadata.json" << EOF
{
    "timestamp": "$(date -Iseconds)",
    "environment": "$ENV",
    "backup_file": "$GZ_FILE",
    "file_size_bytes": $NEW_SIZE,
    "total_rows": $NEW_ROWS,
    "dump_duration_seconds": $DUMP_DURATION,
    "is_anomaly": $ANOMALY,
    "anomaly_reason": "$ANOMALY_REASON"
}
EOF
    gsutil cp "${BACKUP_DIR}/metadata.json" "${GCS_BUCKET}/metadata.json" || {
        log_warn "Failed to update metadata"
    }
fi

# -----------------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------------
log_info "========================================"
log_info "Backup completed successfully"
log_info "  Environment: $ENV"
log_info "  File: $GZ_FILE"
log_info "  Size: ${NEW_SIZE_MB} MB"
log_info "  Rows: $NEW_ROWS"
log_info "  Duration: ${DUMP_DURATION}s"
if [ "$ANOMALY" = true ]; then
    log_warn "  Status: ANOMALY (investigation required)"
else
    log_info "  Status: SUCCESS"
fi
log_info "========================================"

exit 0
