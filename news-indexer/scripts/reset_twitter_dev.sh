#!/usr/bin/env bash
set -euo pipefail

# Reset dev data for the Twitter flow:
# - Removes indexer twitter rows (tweets, media, analysis, cursors, submit state)
# - Removes imported reports created by the twitter submitter on dev
# - Keeps the fetcher token row intact
#
# Usage:
#   YES=1 MYSQL_HOST=127.0.0.1 MYSQL_USER=server MYSQL_PASSWORD=*** MYSQL_DB=cleanapp ./reset_twitter_dev.sh
# or:
#   ./reset_twitter_dev.sh --yes   (and rely on ~/.my.cnf or env vars)
#
# Optional envs (with defaults except password):
#   MYSQL_HOST (default: 127.0.0.1)
#   MYSQL_PORT (default: 3306)
#   MYSQL_USER (default: server)
#   MYSQL_PASSWORD (default: "")  # if empty, relies on .my.cnf or no password
#   MYSQL_DB (default: cleanapp)
#   FETCHER_ID (default: twitter_submitter_dev)
#   SOURCE_NAME (default: twitter)

YES="${YES:-0}"
if [[ "${1:-}" == "--yes" ]]; then
  YES=1
fi
if [[ "$YES" != "1" ]]; then
  echo "Refusing to run destructive reset without confirmation."
  echo "Set YES=1 env or pass --yes."
  exit 1
fi

MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-server}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
MYSQL_DB="${MYSQL_DB:-cleanapp}"
FETCHER_ID="${FETCHER_ID:-twitter_submitter_dev}"
SOURCE_NAME="${SOURCE_NAME:-twitter}"

MYSQL_OPTS=( -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u "$MYSQL_USER" )
if [[ -n "$MYSQL_PASSWORD" ]]; then
  MYSQL_OPTS+=( -p"$MYSQL_PASSWORD" )
fi

echo "Resetting dev data for Twitter flow on DB '$MYSQL_DB' (host=$MYSQL_HOST user=$MYSQL_USER) ..."
mysql "${MYSQL_OPTS[@]}" "$MYSQL_DB" <<SQL
SET @fetcher_id := '${FETCHER_ID}';
SET @src := '${SOURCE_NAME}';

-- Collect existing twitter-submitter report seqs
DROP TEMPORARY TABLE IF EXISTS tmp_twitter_seq;
CREATE TEMPORARY TABLE tmp_twitter_seq (seq INT PRIMARY KEY) ENGINE=Memory;
INSERT INTO tmp_twitter_seq (seq)
SELECT r.seq FROM reports r WHERE r.id = CONCAT('fetcher:', @fetcher_id);

-- Remove dependent analysis/details first
DELETE ra
FROM report_analysis ra
JOIN tmp_twitter_seq t ON ra.seq = t.seq;

DELETE rd
FROM report_details rd
JOIN tmp_twitter_seq t ON rd.seq = t.seq;

-- Remove external ingest mappings for this source (by source name or seqs)
DELETE ei
FROM external_ingest_index ei
WHERE ei.source = @src
   OR ei.seq IN (SELECT seq FROM tmp_twitter_seq);

-- Remove the reports themselves
DELETE r
FROM reports r
JOIN tmp_twitter_seq t ON r.seq = t.seq;

-- Wipe twitter indexer analysis
DELETE FROM indexer_twitter_analysis;

-- Capture twitter media blob hashes, then delete media first to satisfy FK
DROP TEMPORARY TABLE IF EXISTS tmp_twitter_blob_sha;
CREATE TEMPORARY TABLE tmp_twitter_blob_sha (sha256 BINARY(32) PRIMARY KEY) ENGINE=Memory;
INSERT IGNORE INTO tmp_twitter_blob_sha (sha256)
SELECT DISTINCT m.sha256 FROM indexer_twitter_media m;

-- Wipe twitter indexer media and then tweets/cursors/state
DELETE FROM indexer_twitter_media;
DELETE FROM indexer_twitter_tweet;
DELETE FROM indexer_twitter_cursor;
DELETE FROM indexer_twitter_submit_state;

-- Now remove the captured blobs (parents) safely
DELETE b
FROM indexer_media_blob b
JOIN tmp_twitter_blob_sha t ON b.sha256 = t.sha256;

-- Done; temp table auto-drops at session end
SQL

echo "Reset complete."


