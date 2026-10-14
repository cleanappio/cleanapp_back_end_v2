// Package reportmeta maintains a small projection of raw report identity and
// display metadata. Privacy, source provenance and status remain live joins.
package reportmeta

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Migrate is an explicit cmd/migrate step. It creates only a new table and
// AFTER triggers; it never rebuilds reports or implicitly backfills history.
// Types match the legacy production reports columns, including FLOAT precision.
func Migrate(ctx context.Context, db *sql.DB) (retErr error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var previousTimeout int
	if err := conn.QueryRowContext(ctx, `SELECT @@SESSION.lock_wait_timeout`).Scan(&previousTimeout); err != nil {
		return fmt.Errorf("read migration metadata lock limit: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SET SESSION lock_wait_timeout = ?`, 5); err != nil {
		return fmt.Errorf("set migration metadata lock limit: %w", err)
	}
	defer func() {
		resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(resetCtx, `SET SESSION lock_wait_timeout = ?`, previousTimeout); err != nil {
			// Do not return a session with altered limits to the connection pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			retErr = errors.Join(retErr, fmt.Errorf("restore migration metadata lock limit: %w", err))
		}
	}()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS report_metadata_projection (
		seq INT NOT NULL PRIMARY KEY,
		id VARCHAR(255) NOT NULL,
		public_id VARCHAR(32) NULL,
		ts TIMESTAMP NULL DEFAULT NULL,
		latitude FLOAT NOT NULL,
		longitude FLOAT NOT NULL,
		has_original_media BOOL NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return fmt.Errorf("create report metadata projection: %w", err)
	}
	for _, trigger := range metadataTriggers() {
		var table, timing, event, body string
		err := conn.QueryRowContext(ctx, `SELECT EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT
			FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?`, trigger.name).Scan(&table, &timing, &event, &body)
		if err == nil {
			if table != "reports" || timing != "AFTER" || event != trigger.event || normalizeTrigger(body) != normalizeTrigger(trigger.body) {
				return fmt.Errorf("metadata trigger %s has an unexpected definition", trigger.name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check metadata trigger %s: %w", trigger.name, err)
		}
		ddl := "CREATE TRIGGER " + trigger.name + " AFTER " + trigger.event + " ON reports FOR EACH ROW " + trigger.body
		if _, err := conn.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create metadata trigger %s: %w", trigger.name, err)
		}
	}
	return nil
}

type metadataTrigger struct{ name, event, body string }

func metadataTriggers() []metadataTrigger {
	upsert := `INSERT INTO report_metadata_projection (seq,id,public_id,ts,latitude,longitude,has_original_media)
		VALUES (NEW.seq,NEW.id,NEW.public_id,NEW.ts,NEW.latitude,NEW.longitude,COALESCE(LENGTH(NEW.image),0)>0)
		ON DUPLICATE KEY UPDATE id=NEW.id,public_id=NEW.public_id,ts=NEW.ts,
		latitude=NEW.latitude,longitude=NEW.longitude,has_original_media=COALESCE(LENGTH(NEW.image),0)>0;`
	return []metadataTrigger{
		{"reports_metadata_after_insert", "INSERT", upsert},
		{"reports_metadata_after_update", "UPDATE", `BEGIN
			IF OLD.seq <> NEW.seq THEN DELETE FROM report_metadata_projection WHERE seq=OLD.seq; END IF;
			` + upsert + ` END`},
		{"reports_metadata_after_delete", "DELETE", `DELETE FROM report_metadata_projection WHERE seq=OLD.seq`},
	}
}

func normalizeTrigger(body string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimRight(strings.TrimSpace(body), ";")), " "))
}
