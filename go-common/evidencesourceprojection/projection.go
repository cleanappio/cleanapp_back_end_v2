// Package evidencesourceprojection stores versioned normalizer results. Source
// tables and their existing projections remain authoritative. A dirty or absent
// row is never evidence that a report is absent or public.
package evidencesourceprojection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const Table = "report_evidence_source_projection"
const NormalizerVersion = 1
const MaxBatch = 200

var outputColumns = []string{"source_platform", "source_url", "source_timestamp", "observed_at", "ts", "ingestion_time_missing", "actor_kind", "fetcher_id", "privacy_restricted", "source_key", "external_source", "intake_channel", "generation_method", "source_account_key", "ingested_at_null"}
var stringColumns = []string{"source_platform", "source_url", "actor_kind", "fetcher_id", "source_key", "intake_channel", "generation_method", "source_account_key"}
var boolColumns = []string{"ingestion_time_missing", "privacy_restricted", "external_source", "ingested_at_null"}
var dateColumns = []string{"source_timestamp", "observed_at", "ts"}
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var dateLiteral = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?$`)

// Record is an entire normalizer result, not a patch. Dates are SQL literals so
// NULL, historical zero dates and fractional seconds survive strict SQL modes.
// Visibility, analysis, report kind and original media are deliberately absent.
type Record struct {
	Seq                                               int
	SourcePlatform, SourceURL                         string
	SourceTimestamp, ObservedAt, TS                   *string
	IngestionTimeMissing                              bool
	ActorKind, FetcherID                              string
	PrivacyRestricted                                 bool
	SourceKey                                         string
	ExternalSource                                    bool
	IntakeChannel, GenerationMethod, SourceAccountKey string
	IngestedAtNull                                    bool
}

func Columns() []string { return append([]string(nil), outputColumns...) }

// CleanPredicate accepts a trusted SQL alias, never a report value. Nullable
// source/observed dates are authoritative; all remaining outputs must be loaded.
func CleanPredicate(alias string) string {
	if !identifier.MatchString(alias) {
		panic("invalid source projection SQL alias")
	}
	p := alias + "."
	terms := []string{p + "dirty=0", p + "normalizer_version=1", p + "computed_revision=" + p + "source_revision"}
	for _, c := range append(append([]string{}, stringColumns...), boolColumns...) {
		terms = append(terms, p+c+" IS NOT NULL")
	}
	terms = append(terms, "(("+p+"ingested_at_null=1 AND "+p+"ts IS NULL) OR ("+p+"ingested_at_null=0 AND "+p+"ts IS NOT NULL))")
	return "(" + strings.Join(terms, " AND ") + ")"
}

// EnsureRowsAndLock must be the first database access in a fresh repeatable-read
// refresh transaction: no consistent read may precede these current row locks.
// Keep the locks until the source snapshot has been normalized and stored. The
// caller owns commit/rollback. Never delete generation rows, including orphans.
func EnsureRowsAndLock(ctx context.Context, tx *sql.Tx, sortedIDs []int) (map[int]uint64, error) {
	if len(sortedIDs) > MaxBatch {
		return nil, fmt.Errorf("source projection batch exceeds %d", MaxBatch)
	}
	for i, id := range sortedIDs {
		if id <= 0 || (i > 0 && id <= sortedIDs[i-1]) {
			return nil, errors.New("source projection IDs must be positive, unique and sorted")
		}
	}
	revisions := make(map[int]uint64, len(sortedIDs))
	if len(sortedIDs) == 0 {
		return revisions, nil
	}
	// Individual ascending writes/locks give all refreshers one lock order. INSERT
	// IGNORE would hide warnings other than duplicates, so use a no-op upsert.
	for _, id := range sortedIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO report_evidence_source_projection (seq) VALUES (?) ON DUPLICATE KEY UPDATE seq=VALUES(seq)`, id); err != nil {
			return nil, fmt.Errorf("ensure source generation: %w", err)
		}
		var rev uint64
		if err := tx.QueryRowContext(ctx, `SELECT source_revision FROM report_evidence_source_projection WHERE seq=? FOR UPDATE`, id).Scan(&rev); err != nil {
			return nil, fmt.Errorf("lock source generation: %w", err)
		}
		revisions[id] = rev
	}
	return revisions, nil
}

// Store publishes a complete result only for the locked generation. false means
// the generation changed/missing; callers must not treat that as a clean cache.
func Store(ctx context.Context, tx *sql.Tx, expectedRevision uint64, r Record) (bool, error) {
	if r.Seq <= 0 || expectedRevision == 0 || r.IngestedAtNull != (r.TS == nil) {
		return false, errors.New("invalid source projection identity/revision or ingestion null state")
	}
	for _, d := range []*string{r.SourceTimestamp, r.ObservedAt, r.TS} {
		if d != nil && !dateLiteral.MatchString(*d) {
			return false, errors.New("invalid source projection date literal")
		}
	}
	assignments := make([]string, 0, len(outputColumns)+2)
	for _, c := range outputColumns {
		assignments = append(assignments, c+"=?")
	}
	assignments = append(assignments, "computed_revision=?", "normalizer_version=?", "dirty=0")
	args := []any{r.SourcePlatform, r.SourceURL, r.SourceTimestamp, r.ObservedAt, r.TS, r.IngestionTimeMissing, r.ActorKind, r.FetcherID, r.PrivacyRestricted, r.SourceKey, r.ExternalSource, r.IntakeChannel, r.GenerationMethod, r.SourceAccountKey, r.IngestedAtNull, expectedRevision, NormalizerVersion, r.Seq, expectedRevision}
	result, err := tx.ExecContext(ctx, "UPDATE "+Table+" SET "+strings.Join(assignments, ",")+" WHERE seq=? AND source_revision=?", args...)
	if err != nil {
		return false, fmt.Errorf("store source projection: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}
	// The default MySQL driver reports changed rows, not matched rows. A replay
	// of the same complete result has zero changes and is still successful.
	var revision uint64
	err = tx.QueryRowContext(ctx, "SELECT p.source_revision FROM "+Table+" p WHERE p.seq=? AND p.source_revision=? AND "+CleanPredicate("p")+" FOR UPDATE", r.Seq, expectedRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func createTableSQL() string {
	columns := []string{"seq INT NOT NULL", "source_revision BIGINT UNSIGNED NOT NULL DEFAULT 1", "computed_revision BIGINT UNSIGNED NOT NULL DEFAULT 0", "normalizer_version INT NOT NULL DEFAULT 0", "dirty BOOL NOT NULL DEFAULT TRUE"}
	for _, c := range outputColumns {
		typ := "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
		for _, d := range dateColumns {
			if c == d {
				typ = "VARCHAR(26) CHARACTER SET ascii COLLATE ascii_bin"
			}
		}
		for _, b := range boolColumns {
			if c == b {
				typ = "BOOL"
			}
		}
		columns = append(columns, c+" "+typ+" NULL")
	}
	return "CREATE TABLE IF NOT EXISTS " + Table + " (" + strings.Join(columns, ",") + ", PRIMARY KEY(seq), KEY idx_dirty_seq(dirty,seq)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin"
}

type trigger struct{ name, table, timing, event, body string }

func triggers() []trigger {
	var result []trigger
	for _, source := range []struct{ table, key string }{{"reports", "seq"}, {"report_raw", "report_seq"}, {"external_ingest_index", "seq"}, {"wire_submissions_raw", "report_seq"}, {"report_metadata_projection", "seq"}, {"external_ingest_header_projection", "seq"}, {"wire_evidence_projection", "report_seq"}} {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			var selections []string
			if event != "INSERT" {
				selections = append(selections, "SELECT OLD."+source.key+" AS seq")
			}
			if event != "DELETE" {
				selections = append(selections, "SELECT NEW."+source.key+" AS seq")
			}
			body := `INSERT INTO report_evidence_source_projection (seq) SELECT seq FROM (` + strings.Join(selections, " UNION ") + `) AS changed WHERE seq IS NOT NULL ORDER BY seq ON DUPLICATE KEY UPDATE source_revision=source_revision+1,dirty=TRUE`
			result = append(result, trigger{"evidence_source_" + source.table + "_" + strings.ToLower(event), source.table, "AFTER", event, body})
		}
	}
	// InnoDB cascades skip the Wire and projection triggers. Dirty affected live
	// reports before deleting their fetcher, while the source children still exist.
	body := `INSERT INTO report_evidence_source_projection (seq) SELECT DISTINCT report_seq FROM wire_submissions_raw WHERE fetcher_id=OLD.fetcher_id AND report_seq IS NOT NULL ORDER BY report_seq ON DUPLICATE KEY UPDATE source_revision=source_revision+1,dirty=TRUE`
	result = append(result, trigger{"evidence_source_fetcher_delete", "fetchers", "BEFORE", "DELETE", body})
	return result
}

// Migrate is explicit DDL only: no base table ALTER, backfill, trigger
// replacement, cache deletion, global setting or startup migration is performed.
func Migrate(ctx context.Context, db *sql.DB) (retErr error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var oldWait int
	if err = conn.QueryRowContext(ctx, `SELECT @@SESSION.lock_wait_timeout`).Scan(&oldWait); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `SET SESSION lock_wait_timeout=?`, 5); err != nil {
		return err
	}
	defer func() {
		resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, e := conn.ExecContext(resetCtx, `SET SESSION lock_wait_timeout=?`, oldWait); e != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			retErr = errors.Join(retErr, fmt.Errorf("restore projection session: %w", e))
		}
	}()
	if err = verifyCascadeContract(ctx, conn); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, createTableSQL()); err != nil {
		return fmt.Errorf("create evidence source projection: %w", err)
	}
	if err = verifyTable(ctx, conn); err != nil {
		return err
	}
	for _, t := range triggers() {
		exists, e := verifyTrigger(ctx, conn, t)
		if e != nil {
			return e
		}
		if exists {
			continue
		}
		if _, e = conn.ExecContext(ctx, "CREATE TRIGGER "+t.name+" "+t.timing+" "+t.event+" ON "+t.table+" FOR EACH ROW "+t.body); e != nil {
			return fmt.Errorf("create source invalidation trigger %s: %w", t.name, e)
		}
	}
	return nil
}

// Verify is read-only readiness validation for operators and workers. It never
// creates a table/trigger, changes settings, performs backfill or repairs drift.
func Verify(ctx context.Context, db *sql.DB) error {
	c, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = verifyCascadeContract(ctx, c); err != nil {
		return err
	}
	if err = verifyTable(ctx, c); err != nil {
		return err
	}
	for _, t := range triggers() {
		exists, e := verifyTrigger(ctx, c, t)
		if e != nil {
			return e
		}
		if !exists {
			return fmt.Errorf("source invalidation trigger %s is missing", t.name)
		}
	}
	return nil
}
func verifyTrigger(ctx context.Context, c *sql.Conn, t trigger) (bool, error) {
	var table, timing, event, body string
	e := c.QueryRowContext(ctx, `SELECT EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?`, t.name).Scan(&table, &timing, &event, &body)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if table != t.table || timing != t.timing || event != t.event || normalize(body) != normalize(t.body) {
		return false, fmt.Errorf("source invalidation trigger %s has an unexpected definition", t.name)
	}
	return true, nil
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(s), ";")), " ")
}

func verifyTable(ctx context.Context, c *sql.Conn) error {
	rows, err := c.QueryContext(ctx, `SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLLATION_NAME,''),COLUMN_DEFAULT,EXTRA FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, Table)
	if err != nil {
		return err
	}
	expected := [][5]string{{"seq", "int", "NO", "", ""}, {"source_revision", "bigint unsigned", "NO", "", "1"}, {"computed_revision", "bigint unsigned", "NO", "", "0"}, {"normalizer_version", "int", "NO", "", "0"}, {"dirty", "tinyint(1)", "NO", "", "1"}}
	for _, name := range outputColumns {
		typ, coll := "longtext", "utf8mb4_bin"
		for _, d := range dateColumns {
			if name == d {
				typ, coll = "varchar(26)", "ascii_bin"
			}
		}
		for _, b := range boolColumns {
			if name == b {
				typ, coll = "tinyint(1)", ""
			}
		}
		expected = append(expected, [5]string{name, typ, "YES", coll, ""})
	}
	i := 0
	for rows.Next() {
		var name, typ, nullable, coll, extra string
		var def sql.NullString
		if err = rows.Scan(&name, &typ, &nullable, &coll, &def, &extra); err != nil {
			rows.Close()
			return err
		}
		v := [5]string{name, typ, nullable, coll, def.String}
		if i >= len(expected) || v != expected[i] || extra != "" || (i == 0 && def.Valid) || (i >= 5 && def.Valid) {
			rows.Close()
			return fmt.Errorf("source projection has unexpected column %s", name)
		}
		i++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if i != len(expected) {
		return errors.New("source projection has incomplete columns")
	}
	var engine string
	var fks, indexes int
	if err = c.QueryRowContext(ctx, `SELECT ENGINE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, Table).Scan(&engine); err != nil {
		return err
	}
	if err = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE CONSTRAINT_SCHEMA=DATABASE() AND (TABLE_NAME=? AND REFERENCED_TABLE_NAME IS NOT NULL OR REFERENCED_TABLE_NAME=?)`, Table, Table).Scan(&fks); err != nil {
		return err
	}
	if err = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, Table).Scan(&indexes); err != nil {
		return err
	}
	var primary, dirtyIndex int
	if err = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME='PRIMARY' AND NON_UNIQUE=0 AND SEQ_IN_INDEX=1 AND COLUMN_NAME='seq' AND SUB_PART IS NULL AND COLLATION='A' AND INDEX_TYPE='BTREE' AND IS_VISIBLE='YES'`, Table).Scan(&primary); err != nil {
		return err
	}
	if err = c.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME='idx_dirty_seq' AND NON_UNIQUE=1 AND SUB_PART IS NULL AND COLLATION='A' AND INDEX_TYPE='BTREE' AND IS_VISIBLE='YES' AND ((SEQ_IN_INDEX=1 AND COLUMN_NAME='dirty') OR (SEQ_IN_INDEX=2 AND COLUMN_NAME='seq'))`, Table).Scan(&dirtyIndex); err != nil {
		return err
	}
	if engine != "InnoDB" || fks != 0 || indexes != 3 || primary != 1 || dirtyIndex != 2 {
		return errors.New("source projection has unexpected engine, foreign keys or indexes")
	}
	return nil
}

func verifyCascadeContract(ctx context.Context, c *sql.Conn) error {
	rows, err := c.QueryContext(ctx, `SELECT k.TABLE_NAME,k.COLUMN_NAME,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.DELETE_RULE,r.UPDATE_RULE FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS r JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE k ON k.CONSTRAINT_SCHEMA=r.CONSTRAINT_SCHEMA AND k.CONSTRAINT_NAME=r.CONSTRAINT_NAME AND k.TABLE_NAME=r.TABLE_NAME WHERE r.CONSTRAINT_SCHEMA=DATABASE() AND r.TABLE_NAME IN ('reports','fetchers','report_raw','external_ingest_index','wire_submissions_raw','report_metadata_projection','external_ingest_header_projection','wire_evidence_projection')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	allowed := map[string]bool{"report_raw/report_seq/reports/seq/CASCADE/RESTRICT": true, "wire_submissions_raw/report_seq/reports/seq/SET NULL/RESTRICT": true, "wire_submissions_raw/fetcher_id/fetchers/fetcher_id/CASCADE/RESTRICT": true, "wire_evidence_projection/submission_id/wire_submissions_raw/submission_id/CASCADE/CASCADE": true}
	for rows.Next() {
		var v [6]string
		if err = rows.Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5]); err != nil {
			return err
		}
		for i := 4; i < 6; i++ {
			if v[i] == "NO ACTION" {
				v[i] = "RESTRICT"
			}
		}
		if !allowed[strings.Join(v[:], "/")] {
			return fmt.Errorf("unsupported source cascade contract on %s.%s", v[0], v[1])
		}
	}
	return rows.Err()
}
