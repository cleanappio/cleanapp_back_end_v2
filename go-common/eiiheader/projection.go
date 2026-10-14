// Package eiiheader maintains a transactionally synchronized, seq-clustered
// copy of external source headers. The original EII identity scan and all
// report visibility checks remain authoritative.
package eiiheader

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const Table = "external_ingest_header_projection"

const createTableSQL = `CREATE TABLE IF NOT EXISTS external_ingest_header_projection (
 seq INT NOT NULL,
 source VARCHAR(64) NOT NULL,
 external_id VARCHAR(255) NOT NULL,
 source_url VARCHAR(500) NULL,
 source_timestamp DATETIME NULL,
 created_at TIMESTAMP NULL DEFAULT NULL,
 PRIMARY KEY (seq,source,external_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

// Migrate creates only the projection and AFTER triggers. There is no parent
// ALTER, FK, implicit history backfill, or new report-parent intake constraint.
func Migrate(ctx context.Context, db *sql.DB) (retErr error) {
	conn, restore, err := projectionSession(ctx, db)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer func() { retErr = errors.Join(retErr, restore()) }()
	// Cascaded writes bypass triggers. Current EII has no FKs; fail explicitly
	// if a future schema introduces them rather than promise stale maintenance.
	var foreignKeys int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME='external_ingest_index'`).Scan(&foreignKeys); err != nil {
		return fmt.Errorf("check external source cascade contract: %w", err)
	}
	if foreignKeys != 0 {
		return fmt.Errorf("external source table has foreign keys requiring an explicit cascade maintenance design")
	}
	if err := verifyColumns(ctx, conn, "external_ingest_index"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, createTableSQL); err != nil {
		return fmt.Errorf("create external source header projection: %w", err)
	}
	if err := verifyTable(ctx, conn); err != nil {
		return err
	}
	var creationMode string
	if err := conn.QueryRowContext(ctx, `SELECT @@SESSION.sql_mode`).Scan(&creationMode); err != nil {
		return err
	}
	for _, trigger := range projectionTriggers() {
		var table, timing, event, body, mode string
		err := conn.QueryRowContext(ctx, `SELECT EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT,SQL_MODE FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?`, trigger.name).Scan(&table, &timing, &event, &body, &mode)
		if err == nil {
			if table != "external_ingest_index" || timing != "AFTER" || event != trigger.event || normalizeBody(body) != normalizeBody(trigger.body) || normalizeMode(mode) != normalizeMode(creationMode) {
				return fmt.Errorf("external source header trigger %s has an unexpected definition or SQL mode", trigger.name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check external source header trigger %s: %w", trigger.name, err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TRIGGER "+trigger.name+" AFTER "+trigger.event+" ON external_ingest_index FOR EACH ROW "+trigger.body); err != nil {
			return fmt.Errorf("create external source header trigger %s: %w", trigger.name, err)
		}
	}
	return nil
}

type projectionTrigger struct{ name, event, body string }

func projectionTriggers() []projectionTrigger {
	upsert := `INSERT INTO external_ingest_header_projection (seq,source,external_id,source_url,source_timestamp,created_at)
 VALUES (NEW.seq,NEW.source,NEW.external_id,NEW.source_url,NEW.source_timestamp,NEW.created_at)
 ON DUPLICATE KEY UPDATE source=NEW.source,external_id=NEW.external_id,source_url=NEW.source_url,source_timestamp=NEW.source_timestamp,created_at=NEW.created_at;`
	return []projectionTrigger{
		{"eii_header_after_insert", "INSERT", upsert},
		{"eii_header_after_update", "UPDATE", `BEGIN DELETE FROM external_ingest_header_projection WHERE seq=OLD.seq AND source=OLD.source AND external_id=OLD.external_id; ` + upsert + ` END`},
		{"eii_header_after_delete", "DELETE", `DELETE FROM external_ingest_header_projection WHERE seq=OLD.seq AND source=OLD.source AND external_id=OLD.external_id`},
	}
}

// The captured trigger mode permits copying preexisting zero dates even when
// a strict caller only updates another parent field. It does not change the
// caller's parent-table validation, or any global setting. Other modes remain.
func copyMode(mode string) string {
	parts := strings.Split(mode, ",")
	kept := parts[:0]
	for _, part := range parts {
		if part != "NO_ZERO_DATE" && part != "NO_ZERO_IN_DATE" && part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ",")
}

func normalizeMode(mode string) string {
	parts := strings.Split(mode, ",")
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func normalizeBody(body string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimRight(strings.TrimSpace(body), ";")), " "))
}

func projectionSession(ctx context.Context, db *sql.DB) (*sql.Conn, func() error, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	var mode string
	var metadataWait, rowWait int
	if err := conn.QueryRowContext(ctx, `SELECT @@SESSION.sql_mode,@@SESSION.lock_wait_timeout,@@SESSION.innodb_lock_wait_timeout`).Scan(&mode, &metadataWait, &rowWait); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read projection session settings: %w", err)
	}
	const settings = `SET SESSION sql_mode=?,lock_wait_timeout=?,innodb_lock_wait_timeout=?`
	if _, err := conn.ExecContext(ctx, settings, copyMode(mode), 5, 5); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		conn.Close()
		return nil, nil, fmt.Errorf("set projection session settings: %w", err)
	}
	restore := func() error {
		resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(resetCtx, settings, mode, metadataWait, rowWait); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			return fmt.Errorf("restore projection session settings: %w", err)
		}
		return nil
	}
	return conn, restore, nil
}

func verifyColumns(ctx context.Context, conn *sql.Conn, table string) error {
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLLATION_NAME,'') FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME IN ('seq','source','external_id','source_url','source_timestamp','created_at') ORDER BY FIELD(COLUMN_NAME,'seq','source','external_id','source_url','source_timestamp','created_at')`, table)
	if err != nil {
		return err
	}
	expected := [][4]string{{"seq", "int", "NO", ""}, {"source", "varchar(64)", "NO", "utf8mb4_unicode_ci"}, {"external_id", "varchar(255)", "NO", "utf8mb4_unicode_ci"}, {"source_url", "varchar(500)", "YES", "utf8mb4_unicode_ci"}, {"source_timestamp", "datetime", "YES", ""}, {"created_at", "timestamp", "YES", ""}}
	index := 0
	for rows.Next() {
		var value [4]string
		if err := rows.Scan(&value[0], &value[1], &value[2], &value[3]); err != nil {
			rows.Close()
			return err
		}
		if index >= len(expected) || value != expected[index] {
			rows.Close()
			return fmt.Errorf("external source header table has an unexpected column definition")
		}
		index++
	}
	iterationErr := rows.Err()
	rows.Close()
	if iterationErr != nil {
		return iterationErr
	}
	if index != len(expected) {
		return fmt.Errorf("external source header table has incomplete columns")
	}
	return nil
}

func verifyTable(ctx context.Context, conn *sql.Conn) error {
	if err := verifyColumns(ctx, conn, Table); err != nil {
		return err
	}
	var columns, foreignKeys int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='external_ingest_header_projection'`).Scan(&columns); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME='external_ingest_header_projection'`).Scan(&foreignKeys); err != nil {
		return err
	}
	if columns != 6 || foreignKeys != 0 {
		return fmt.Errorf("external source header table has extra columns or intake constraints")
	}
	rows, err := conn.QueryContext(ctx, `SELECT INDEX_NAME,NON_UNIQUE,SEQ_IN_INDEX,COLUMN_NAME,SUB_PART,COLLATION,INDEX_TYPE,IS_VISIBLE FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='external_ingest_header_projection' ORDER BY INDEX_NAME,SEQ_IN_INDEX`)
	if err != nil {
		return err
	}
	index := 0
	for rows.Next() {
		var name, column, direction, kind, visible string
		var nonUnique, ordinal int
		var prefix sql.NullInt64
		if err := rows.Scan(&name, &nonUnique, &ordinal, &column, &prefix, &direction, &kind, &visible); err != nil {
			rows.Close()
			return err
		}
		if index >= 3 || name != "PRIMARY" || nonUnique != 0 || ordinal != index+1 || column != []string{"seq", "source", "external_id"}[index] || prefix.Valid || direction != "A" || kind != "BTREE" || visible != "YES" {
			rows.Close()
			return fmt.Errorf("external source header table has an unexpected index definition")
		}
		index++
	}
	iterationErr := rows.Err()
	rows.Close()
	if iterationErr != nil {
		return iterationErr
	}
	var engine string
	if err := conn.QueryRowContext(ctx, `SELECT ENGINE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='external_ingest_header_projection'`).Scan(&engine); err != nil {
		return err
	}
	if index != 3 || engine != "InnoDB" {
		return fmt.Errorf("external source header table has an unexpected primary key or engine")
	}
	return nil
}
