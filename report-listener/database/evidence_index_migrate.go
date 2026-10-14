package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

const evidenceBrandCoreIndex = "idx_ra_brand_valid_class_core"

// This explicit migration adds only an ordinary secondary covering index.
// Request both online clauses so unsupported schemas fail instead of silently
// copying/rebuilding analysis history or taking a long-lived table write lock.
// Building the index may take time; only metadata lock acquisition is capped.
func ensureEvidenceBrandCoreIndex(ctx context.Context, db *sql.DB) (retErr error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var previousTimeout int
	if err := conn.QueryRowContext(ctx, `SELECT @@SESSION.lock_wait_timeout`).Scan(&previousTimeout); err != nil {
		return fmt.Errorf("read index migration lock limit: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SET SESSION lock_wait_timeout = ?`, 5); err != nil {
		return fmt.Errorf("set index migration lock limit: %w", err)
	}
	defer func() {
		resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(resetCtx, `SET SESSION lock_wait_timeout = ?`, previousTimeout); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			retErr = errors.Join(retErr, fmt.Errorf("restore index migration lock limit: %w", err))
		}
	}()
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME,SUB_PART,NON_UNIQUE,INDEX_TYPE,IS_VISIBLE,COLLATION
		FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_analysis'
		AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX`, evidenceBrandCoreIndex)
	if err != nil {
		return fmt.Errorf("inspect evidence brand core index: %w", err)
	}
	expected := []string{"brand_name", "is_valid", "classification", "seq", "language", "created_at", "severity_level"}
	count := 0
	valid := true
	for rows.Next() {
		var column, indexType, visible, direction string
		var prefix sql.NullInt64
		var nonUnique int
		if err := rows.Scan(&column, &prefix, &nonUnique, &indexType, &visible, &direction); err != nil {
			rows.Close()
			return fmt.Errorf("scan evidence brand core index: %w", err)
		}
		if count >= len(expected) || column != expected[count] || prefix.Valid || nonUnique != 1 || indexType != "BTREE" || visible != "YES" || direction != "A" {
			valid = false
		}
		count++
	}
	iterationErr := rows.Err()
	rows.Close()
	if iterationErr != nil {
		return fmt.Errorf("read evidence brand core index: %w", iterationErr)
	}
	if count > 0 {
		if !valid || count != len(expected) {
			return fmt.Errorf("index %s has an unexpected definition", evidenceBrandCoreIndex)
		}
		return nil
	}
	_, err = conn.ExecContext(ctx, `ALTER TABLE report_analysis
		ADD INDEX idx_ra_brand_valid_class_core (brand_name,is_valid,classification,seq,language,created_at,severity_level),
		ALGORITHM=INPLACE, LOCK=NONE`)
	if err != nil {
		return fmt.Errorf("add online evidence brand core index: %w", err)
	}
	return nil
}
