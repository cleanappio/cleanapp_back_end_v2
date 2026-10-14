// Package wiremeta maintains transactional scalar Wire source metadata. The
// original covered Wire identity scan and live report guards stay authoritative.
package wiremeta

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

const Table = "wire_evidence_projection"

type sourceNode struct {
	name, document, path string
	uri                  bool
}

var sourceNodes = []sourceNode{
	{"platform", "report_json", "$.digital_context.platform", false},
	{"legacy_source", "report_json", "$.digital_context.legacy_source", false},
	{"metadata_source_url", "report_json", "$.digital_context.metadata.source_url", true},
	{"digital_source_url", "report_json", "$.digital_context.source_url", true},
	{"digital_url", "report_json", "$.digital_context.url", true},
	{"evidence_uri", "report_json", "$.evidence_bundle[0].uri", true},
	{"source_published_at", "report_json", "$.digital_context.metadata.source_published_at", false},
	{"author_did", "report_json", "$.digital_context.metadata.author_did", false},
	{"author_username", "report_json", "$.digital_context.metadata.author_username", false},
	{"metadata_author_handle", "report_json", "$.digital_context.metadata.author_handle", false},
	{"digital_author_handle", "report_json", "$.digital_context.author_handle", false},
	{"contains_pii", "report_json", "$.privacy.contains_pii", false},
	{"requires_redaction", "report_json", "$.privacy.requires_redaction", false},
	{"generation_method", "provenance_json", "$.generation_method", false},
	{"observation_time_supplied", "extensions_json", "$.observation_time_supplied", false},
}

// NodeColumns returns independent copies in the reader's existing node order.
func NodeColumns() []string {
	out := make([]string, len(sourceNodes))
	for i, node := range sourceNodes {
		out[i] = node.name
	}
	return out
}

// NodeExpressions extracts the exact nullable JSON nodes. alias is a trusted
// SQL identifier supplied by the caller, such as ws or NEW. URL blockers retain
// absent/JSON-null fallback semantics without copying inline media payloads.
func NodeExpressions(alias string) []string {
	out := make([]string, len(sourceNodes))
	for i, node := range sourceNodes {
		expr := "JSON_EXTRACT(" + alias + "." + node.document + ",'" + node.path + "')"
		if node.uri {
			expr = "CASE WHEN " + expr + " IS NULL THEN NULL WHEN JSON_UNQUOTE(" + expr + ")='null' OR LOWER(LEFT(TRIM(JSON_UNQUOTE(" + expr + ")),8)) REGEXP '^https?://' THEN " + expr + " ELSE JSON_QUOTE('__non_http_source_uri__') END"
		}
		out[i] = expr
	}
	return out
}

func projectionColumns() []string {
	return append([]string{"submission_id", "report_seq", "observed_at", "submitted_at", "actor_kind", "channel"}, NodeColumns()...)
}

func sourceExpressions(alias string) []string {
	values := []string{}
	for _, name := range projectionColumns()[:6] {
		values = append(values, alias+"."+name)
	}
	return append(values, NodeExpressions(alias)...)
}

func createTableSQL() string {
	columns := `submission_id VARCHAR(64) NOT NULL,
 report_seq INT NULL,
 observed_at TIMESTAMP NULL DEFAULT NULL,
 submitted_at TIMESTAMP NOT NULL,
 actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',
 channel VARCHAR(32) NOT NULL DEFAULT 'wire',`
	for _, name := range NodeColumns() {
		columns += "\n " + name + " JSON NULL,"
	}
	return `CREATE TABLE IF NOT EXISTS wire_evidence_projection (` + columns + `
 PRIMARY KEY(submission_id),
 KEY idx_wire_evidence_report_seq(report_seq),
 CONSTRAINT fk_wire_evidence_submission FOREIGN KEY(submission_id) REFERENCES wire_submissions_raw(submission_id) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
}

// Migrate creates only the child projection and AFTER triggers. It never
// alters a parent, backfills history implicitly, or adds a report constraint.
func Migrate(ctx context.Context, db *sql.DB) (retErr error) {
	conn, restore, err := projectionSession(ctx, db)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer func() { retErr = errors.Join(retErr, restore()) }()
	if err := verifyParent(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, createTableSQL()); err != nil {
		return fmt.Errorf("create Wire metadata projection: %w", err)
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
			if table != "wire_submissions_raw" || timing != "AFTER" || event != trigger.event || normalizeBody(body) != normalizeBody(trigger.body) || normalizeMode(mode) != normalizeMode(creationMode) {
				return fmt.Errorf("Wire metadata trigger %s has an unexpected definition or SQL mode", trigger.name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check Wire metadata trigger %s: %w", trigger.name, err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TRIGGER "+trigger.name+" AFTER "+trigger.event+" ON wire_submissions_raw FOR EACH ROW "+trigger.body); err != nil {
			return fmt.Errorf("create Wire metadata trigger %s: %w", trigger.name, err)
		}
	}
	return nil
}

type projectionTrigger struct{ name, event, body string }

func projectionTriggers() []projectionTrigger {
	columns := projectionColumns()
	values := sourceExpressions("NEW")
	updates := []string{}
	for i, name := range columns {
		updates = append(updates, name+"="+values[i])
	}
	upsert := "INSERT INTO wire_evidence_projection (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(values, ",") + ") ON DUPLICATE KEY UPDATE " + strings.Join(updates, ",") + ";"
	return []projectionTrigger{
		{"wire_evidence_after_insert", "INSERT", upsert},
		{"wire_evidence_after_update", "UPDATE", `BEGIN DELETE FROM wire_evidence_projection WHERE submission_id=OLD.submission_id; ` + upsert + ` END`},
		{"wire_evidence_after_delete", "DELETE", `DELETE FROM wire_evidence_projection WHERE submission_id=OLD.submission_id`},
	}
}

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

// Preserve quoted JSON-path and sentinel case when checking named definitions.
func normalizeBody(body string) string {
	return strings.Join(strings.Fields(strings.TrimRight(strings.TrimSpace(body), ";")), " ")
}

// Only this dedicated connection permits copying historical zero timestamps.
// Captured trigger mode leaves caller parent validation and global mode intact.
func projectionSession(ctx context.Context, db *sql.DB) (*sql.Conn, func() error, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	var mode string
	var metadataWait, rowWait int
	if err := conn.QueryRowContext(ctx, `SELECT @@SESSION.sql_mode,@@SESSION.lock_wait_timeout,@@SESSION.innodb_lock_wait_timeout`).Scan(&mode, &metadataWait, &rowWait); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read Wire projection session settings: %w", err)
	}
	const settings = `SET SESSION sql_mode=?,lock_wait_timeout=?,innodb_lock_wait_timeout=?`
	if _, err := conn.ExecContext(ctx, settings, copyMode(mode), 5, 5); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		conn.Close()
		return nil, nil, fmt.Errorf("set Wire projection session settings: %w", err)
	}
	restore := func() error {
		resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(resetCtx, settings, mode, metadataWait, rowWait); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			return fmt.Errorf("restore Wire projection session settings: %w", err)
		}
		return nil
	}
	return conn, restore, nil
}

type columnDefinition struct{ name, kind, nullable, collation string }

func scalarDefinitions() []columnDefinition {
	return []columnDefinition{{"submission_id", "varchar(64)", "NO", "utf8mb4_0900_ai_ci"}, {"report_seq", "int", "YES", ""}, {"observed_at", "timestamp", "YES", ""}, {"submitted_at", "timestamp", "NO", ""}, {"actor_kind", "varchar(32)", "NO", "utf8mb4_0900_ai_ci"}, {"channel", "varchar(32)", "NO", "utf8mb4_0900_ai_ci"}}
}

func verifyColumns(ctx context.Context, conn *sql.Conn, table string, expected []columnDefinition) error {
	names := []string{}
	for _, value := range expected {
		names = append(names, "'"+value.name+"'")
	}
	list := strings.Join(names, ",")
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLLATION_NAME,'') FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME IN (`+list+`) ORDER BY FIELD(COLUMN_NAME,`+list+`)`, table)
	if err != nil {
		return err
	}
	index := 0
	for rows.Next() {
		var value columnDefinition
		if err := rows.Scan(&value.name, &value.kind, &value.nullable, &value.collation); err != nil {
			rows.Close()
			return err
		}
		if index >= len(expected) || value != expected[index] {
			rows.Close()
			return fmt.Errorf("Wire metadata source/projection has an unexpected column definition")
		}
		index++
	}
	iterationErr := rows.Err()
	rows.Close()
	if iterationErr != nil {
		return iterationErr
	}
	if index != len(expected) {
		return fmt.Errorf("Wire metadata source/projection has incomplete columns")
	}
	return nil
}

func verifyParent(ctx context.Context, conn *sql.Conn) error {
	expected := append(scalarDefinitions(), columnDefinition{"report_json", "json", "YES", ""}, columnDefinition{"provenance_json", "json", "YES", ""}, columnDefinition{"extensions_json", "json", "YES", ""})
	if err := verifyColumns(ctx, conn, "wire_submissions_raw", expected); err != nil {
		return err
	}
	// Known fetcher deletes are covered by the child FK. Report SET NULL can
	// leave copied report_seq stale, which the live parent identity guard rejects.
	rows, err := conn.QueryContext(ctx, `SELECT k.COLUMN_NAME,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.UPDATE_RULE,r.DELETE_RULE FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE k JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME WHERE k.TABLE_SCHEMA=DATABASE() AND k.TABLE_NAME='wire_submissions_raw' AND k.REFERENCED_TABLE_NAME IS NOT NULL ORDER BY k.CONSTRAINT_NAME,k.ORDINAL_POSITION`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var column, table, referenced, update, deletion string
		if err := rows.Scan(&column, &table, &referenced, &update, &deletion); err != nil {
			return err
		}
		known := update == "NO ACTION" && ((column == "fetcher_id" && table == "fetchers" && referenced == "fetcher_id" && deletion == "CASCADE") || (column == "report_seq" && table == "reports" && referenced == "seq" && deletion == "SET NULL"))
		if !known {
			return fmt.Errorf("Wire metadata source has an unsupported cascade contract")
		}
	}
	return rows.Err()
}

func verifyTable(ctx context.Context, conn *sql.Conn) error {
	expected := scalarDefinitions()
	for _, name := range NodeColumns() {
		expected = append(expected, columnDefinition{name, "json", "YES", ""})
	}
	if err := verifyColumns(ctx, conn, Table, expected); err != nil {
		return err
	}
	var count int
	var engine string
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='wire_evidence_projection'`).Scan(&count); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT ENGINE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='wire_evidence_projection'`).Scan(&engine); err != nil {
		return err
	}
	if count != len(expected) || engine != "InnoDB" {
		return fmt.Errorf("Wire metadata projection has extra columns or an unexpected engine")
	}
	rows, err := conn.QueryContext(ctx, `SELECT INDEX_NAME,NON_UNIQUE,SEQ_IN_INDEX,COLUMN_NAME,SUB_PART,COLLATION,INDEX_TYPE,IS_VISIBLE FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='wire_evidence_projection' ORDER BY INDEX_NAME,SEQ_IN_INDEX`)
	if err != nil {
		return err
	}
	count = 0
	for rows.Next() {
		var name, column, direction, kind, visible string
		var nonUnique, ordinal int
		var prefix sql.NullInt64
		if err := rows.Scan(&name, &nonUnique, &ordinal, &column, &prefix, &direction, &kind, &visible); err != nil {
			rows.Close()
			return err
		}
		known := (name == "PRIMARY" && nonUnique == 0 && column == "submission_id") || (name == "idx_wire_evidence_report_seq" && nonUnique == 1 && column == "report_seq")
		if count >= 2 || !known || ordinal != 1 || prefix.Valid || direction != "A" || kind != "BTREE" || visible != "YES" {
			rows.Close()
			return fmt.Errorf("Wire metadata projection has an unexpected index definition")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != 2 {
		return fmt.Errorf("Wire metadata projection has incomplete indexes")
	}
	rows, err = conn.QueryContext(ctx, `SELECT k.CONSTRAINT_NAME,k.COLUMN_NAME,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.UPDATE_RULE,r.DELETE_RULE FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE k JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME WHERE k.TABLE_SCHEMA=DATABASE() AND k.TABLE_NAME='wire_evidence_projection' AND k.REFERENCED_TABLE_NAME IS NOT NULL`)
	if err != nil {
		return err
	}
	count = 0
	for rows.Next() {
		var name, column, table, referenced, update, deletion string
		if err := rows.Scan(&name, &column, &table, &referenced, &update, &deletion); err != nil {
			rows.Close()
			return err
		}
		if count != 0 || name != "fk_wire_evidence_submission" || column != "submission_id" || table != "wire_submissions_raw" || referenced != "submission_id" || update != "CASCADE" || deletion != "CASCADE" {
			rows.Close()
			return fmt.Errorf("Wire metadata projection has an unexpected foreign key")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("Wire metadata projection has an incomplete foreign key")
	}
	return nil
}
