package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// FetcherV1 represents a fetcher (submitter) for the v1 public ingest surface.
// It reuses the legacy `fetchers` table (token_hash/active) but treats status/tier/caps as canonical.
type FetcherV1 struct {
	FetcherID         string
	Name              string
	OwnerType         string
	Status            string
	Tier              int
	ReputationScore   int
	DailyCapItems     int
	PerMinuteCapItems int
	LastSeenAt        sql.NullTime
}

// FetcherKeyV1 represents a hashed API key for a fetcher.
type FetcherKeyV1 struct {
	KeyID             string
	FetcherID         string
	KeyPrefix         string
	KeyHash           string
	Status            string
	Scopes            []string
	CreatedAt         time.Time
	LastUsedAt        sql.NullTime
	PerMinuteCapItems sql.NullInt64
	DailyCapItems     sql.NullInt64
}

func parseScopesJSON(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InsertFetcherV1 inserts a new fetcher row. tokenHash is retained for legacy v3 bulk_ingest compatibility.
func (d *Database) InsertFetcherV1(ctx context.Context, fetcherID, name, ownerType string, tokenHash []byte) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO fetchers (
			fetcher_id, name, token_hash, scopes, active,
			owner_type, status, tier, reputation_score, daily_cap_items, per_minute_cap_items,
			last_seen_at
		) VALUES (?, ?, ?, NULL, TRUE, ?, 'active', 0, 50, 200, 20, NOW())
	`, fetcherID, name, tokenHash, ownerType)
	if err != nil {
		return fmt.Errorf("insert fetcher: %w", err)
	}
	return nil
}

// InsertFetcherKeyV1 inserts a new fetcher key row (hash-only storage).
func (d *Database) InsertFetcherKeyV1(ctx context.Context, keyID, fetcherID, keyPrefix, keyHash string, scopes []string) error {
	scopesJSON, _ := json.Marshal(scopes)
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO fetcher_keys (
			key_id, fetcher_id, key_prefix, key_hash, status, scopes
		) VALUES (?, ?, ?, ?, 'active', ?)
	`, keyID, fetcherID, keyPrefix, keyHash, scopesJSON)
	if err != nil {
		return fmt.Errorf("insert fetcher key: %w", err)
	}
	return nil
}

// GetFetcherKeyAndFetcherV1 looks up a key and its parent fetcher in a single query.
func (d *Database) GetFetcherKeyAndFetcherV1(ctx context.Context, keyID string) (*FetcherKeyV1, *FetcherV1, error) {
	var (
		k          FetcherKeyV1
		f          FetcherV1
		scopesJSON []byte
	)

	err := d.db.QueryRowContext(ctx, `
		SELECT
			k.key_id, k.fetcher_id, k.key_prefix, k.key_hash, k.status, k.scopes, k.created_at, k.last_used_at,
			k.per_minute_cap_items, k.daily_cap_items,
			f.name, f.owner_type, f.status, f.tier, f.reputation_score, f.daily_cap_items, f.per_minute_cap_items, f.last_seen_at
		FROM fetcher_keys k
		INNER JOIN fetchers f ON f.fetcher_id = k.fetcher_id
		WHERE k.key_id = ?
	`, keyID).Scan(
		&k.KeyID,
		&k.FetcherID,
		&k.KeyPrefix,
		&k.KeyHash,
		&k.Status,
		&scopesJSON,
		&k.CreatedAt,
		&k.LastUsedAt,
		&k.PerMinuteCapItems,
		&k.DailyCapItems,
		&f.Name,
		&f.OwnerType,
		&f.Status,
		&f.Tier,
		&f.ReputationScore,
		&f.DailyCapItems,
		&f.PerMinuteCapItems,
		&f.LastSeenAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, sql.ErrNoRows
		}
		return nil, nil, fmt.Errorf("lookup key: %w", err)
	}

	scopes, err := parseScopesJSON(scopesJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("parse scopes: %w", err)
	}
	k.Scopes = scopes

	f.FetcherID = k.FetcherID
	return &k, &f, nil
}

// TouchFetcherKeyV1 updates last-used/last-seen timestamps (best-effort).
func (d *Database) TouchFetcherKeyV1(ctx context.Context, fetcherID, keyID string) {
	_, _ = d.db.ExecContext(ctx, `UPDATE fetcher_keys SET last_used_at = NOW() WHERE key_id = ?`, keyID)
	_, _ = d.db.ExecContext(ctx, `UPDATE fetchers SET last_seen_at = NOW() WHERE fetcher_id = ?`, fetcherID)
}

// GetUsageV1 returns the current minute/day usage buckets for quota introspection.
func (d *Database) GetUsageV1(ctx context.Context, fetcherID, keyID string, now time.Time) (minuteUsed int, dayUsed int, err error) {
	minBucket := now.UTC().Truncate(time.Minute)
	dayBucket := now.UTC().Truncate(24 * time.Hour)

	// Minute
	_ = d.db.QueryRowContext(ctx, `
		SELECT items FROM fetcher_usage_minute
		WHERE fetcher_id = ? AND key_id = ? AND bucket_minute = ?
	`, fetcherID, keyID, minBucket).Scan(&minuteUsed)
	// Day
	_ = d.db.QueryRowContext(ctx, `
		SELECT items FROM fetcher_usage_daily
		WHERE fetcher_id = ? AND key_id = ? AND bucket_date = ?
	`, fetcherID, keyID, dayBucket).Scan(&dayUsed)

	return minuteUsed, dayUsed, nil
}

// ConsumeQuotaV1 atomically checks and increments per-minute and daily quotas.
// caps <= 0 are treated as "unlimited".
func (d *Database) ConsumeQuotaV1(ctx context.Context, fetcherID, keyID string, now time.Time, items int, perMinuteCap int, dailyCap int) error {
	if items <= 0 {
		return nil
	}

	minBucket := now.UTC().Truncate(time.Minute)
	dayBucket := now.UTC().Truncate(24 * time.Hour)

	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	ensureRow := func(q string, args ...interface{}) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}

	// Ensure rows exist for locking.
	if err := ensureRow(`INSERT INTO fetcher_usage_minute (fetcher_id, key_id, bucket_minute, items) VALUES (?, ?, ?, 0)
		ON DUPLICATE KEY UPDATE items = items`, fetcherID, keyID, minBucket); err != nil {
		return fmt.Errorf("ensure minute bucket: %w", err)
	}
	if err := ensureRow(`INSERT INTO fetcher_usage_daily (fetcher_id, key_id, bucket_date, items) VALUES (?, ?, ?, 0)
		ON DUPLICATE KEY UPDATE items = items`, fetcherID, keyID, dayBucket); err != nil {
		return fmt.Errorf("ensure day bucket: %w", err)
	}

	var minUsed, dayUsed int
	if err := tx.QueryRowContext(ctx, `SELECT items FROM fetcher_usage_minute WHERE fetcher_id=? AND key_id=? AND bucket_minute=? FOR UPDATE`,
		fetcherID, keyID, minBucket).Scan(&minUsed); err != nil {
		return fmt.Errorf("lock minute bucket: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT items FROM fetcher_usage_daily WHERE fetcher_id=? AND key_id=? AND bucket_date=? FOR UPDATE`,
		fetcherID, keyID, dayBucket).Scan(&dayUsed); err != nil {
		return fmt.Errorf("lock day bucket: %w", err)
	}

	if perMinuteCap > 0 && minUsed+items > perMinuteCap {
		return fmt.Errorf("per-minute quota exceeded")
	}
	if dailyCap > 0 && dayUsed+items > dailyCap {
		return fmt.Errorf("daily quota exceeded")
	}

	if _, err := tx.ExecContext(ctx, `UPDATE fetcher_usage_minute SET items = items + ? WHERE fetcher_id=? AND key_id=? AND bucket_minute=?`,
		items, fetcherID, keyID, minBucket); err != nil {
		return fmt.Errorf("inc minute bucket: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fetcher_usage_daily SET items = items + ? WHERE fetcher_id=? AND key_id=? AND bucket_date=?`,
		items, fetcherID, keyID, dayBucket); err != nil {
		return fmt.Errorf("inc day bucket: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quota tx: %w", err)
	}
	return nil
}

// GetExistingReportSeqsV1 returns any existing report_seq for (fetcher_id, source_id) pairs.
func (d *Database) GetExistingReportSeqsV1(ctx context.Context, fetcherID string, sourceIDs []string) (map[string]int, error) {
	out := make(map[string]int)
	if len(sourceIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, 0, len(sourceIDs))
	args := make([]interface{}, 0, len(sourceIDs)+1)
	args = append(args, fetcherID)
	for range sourceIDs {
		placeholders = append(placeholders, "?")
	}
	for _, s := range sourceIDs {
		args = append(args, s)
	}
	q := fmt.Sprintf(`
		SELECT source_id, report_seq
		FROM report_raw
		WHERE fetcher_id = ? AND source_id IN (%s)
	`, join(placeholders, ","))

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query existing report_raw: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID string
		var seq int
		if err := rows.Scan(&sourceID, &seq); err != nil {
			return nil, err
		}
		out[sourceID] = seq
	}
	return out, rows.Err()
}

// InsertIngestionAuditV1 appends an audit row (best-effort; errors are returned for callers that want to enforce).
func (d *Database) InsertIngestionAuditV1(ctx context.Context, fetcherID, keyID, endpoint string, itemsSubmitted, itemsAccepted, itemsRejected int, rejectReasonsJSON []byte, latencyMs int, remoteIP, userAgent, requestID string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO ingestion_audit (
			fetcher_id, key_id, endpoint,
			items_submitted, items_accepted, items_rejected, reject_reasons,
			latency_ms, remote_ip, user_agent, request_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, nullableStr(fetcherID), nullableStr(keyID), endpoint, itemsSubmitted, itemsAccepted, itemsRejected, nullableBytes(rejectReasonsJSON), latencyMs, nullableStr(remoteIP), nullableStr(userAgent), nullableStr(requestID))
	if err != nil {
		return fmt.Errorf("insert ingestion_audit: %w", err)
	}
	return nil
}

func nullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// join is a tiny helper to avoid importing strings just for Join in this file.
func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	n := 0
	for i := range parts {
		n += len(parts[i])
		if i > 0 {
			n += len(sep)
		}
	}
	b := make([]byte, 0, n)
	for i := range parts {
		if i > 0 {
			b = append(b, sep...)
		}
		b = append(b, parts[i]...)
	}
	return string(b)
}
