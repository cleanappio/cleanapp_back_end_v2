// backfill-brand-evidence repairs historical scalar projection gaps without
// changing reports, evidence, visibility, notification queues or API semantics.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cleanapp-common/appenv"
	"cleanapp-common/eiiheader"
	"cleanapp-common/reportmeta"
	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

// BuildSHA is populated by the operator's -ldflags build; -version never opens
// the database. Record it alongside the binary SHA256 in repair evidence.
var BuildSHA = "dev"

type config struct {
	Mode, Brand                   string
	FromSeq, ThroughSeq, AfterSeq int
	BatchSize, MaxBatches         int
	Timeout, Pause                time.Duration
}

func (c config) validate() error {
	if (c.Mode != "audit" && c.Mode != "apply") || c.Brand == "" || len(c.Brand) > 255 || strings.ContainsAny(c.Brand, "\x00\r\n\t") || c.Brand != strings.TrimSpace(c.Brand) ||
		c.FromSeq < 0 || c.ThroughSeq <= 0 || c.ThroughSeq < c.FromSeq || c.ThroughSeq > 2147483647 || c.AfterSeq < 0 || c.AfterSeq > c.ThroughSeq || (c.AfterSeq != 0 && c.AfterSeq < c.FromSeq) ||
		c.BatchSize < 1 || c.BatchSize > 200 || c.MaxBatches < 0 || c.Timeout < time.Second || c.Timeout > time.Minute || c.Pause < 0 || c.Pause > time.Minute {
		return errors.New("configuration_invalid")
	}
	return nil
}

type coverage struct {
	Reports         int64 `json:"reports"`
	MissingParents  int64 `json:"missing_parents"`
	MissingReports  int64 `json:"missing_report_metadata"`
	MissingExternal int64 `json:"missing_external_headers"`
	MissingWire     int64 `json:"missing_wire_headers"`
	StaleReports    int64 `json:"stale_report_metadata"`
	StaleExternal   int64 `json:"stale_external_headers"`
	StaleWire       int64 `json:"stale_wire_headers"`
}

func (c coverage) complete() bool {
	return c.MissingParents == 0 && c.MissingReports == 0 && c.MissingExternal == 0 && c.MissingWire == 0 && c.StaleReports == 0 && c.StaleExternal == 0 && c.StaleWire == 0
}

func (c *coverage) add(v coverage) {
	c.Reports += v.Reports
	c.MissingParents += v.MissingParents
	c.MissingReports += v.MissingReports
	c.MissingExternal += v.MissingExternal
	c.MissingWire += v.MissingWire
	c.StaleReports += v.StaleReports
	c.StaleExternal += v.StaleExternal
	c.StaleWire += v.StaleWire
}

type event struct {
	Stage      string    `json:"stage"`
	SourceSHA  string    `json:"source_sha"`
	At         string    `json:"at"`
	Brand      string    `json:"brand"`
	FromSeq    int       `json:"from_seq"`
	ThroughSeq int       `json:"through_seq"`
	AfterSeq   int       `json:"after_seq"`
	Processed  int       `json:"processed"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Counts     *coverage `json:"counts,omitempty"`
}

type projectionStore interface {
	selectBatch(context.Context, config, int) ([]int, error)
	backfill(context.Context, []int) error
	audit(context.Context, []int) (coverage, error)
}

func main() {
	var c config
	flag.StringVar(&c.Mode, "mode", "audit", "audit (read-only) or apply (derived projections only)")
	flag.StringVar(&c.Brand, "brand", "", "one exact report_analysis brand_name; required")
	flag.IntVar(&c.FromSeq, "from-seq", 0, "inclusive lower bound; default 0")
	flag.IntVar(&c.ThroughSeq, "through-seq", 0, "inclusive immutable upper bound; required")
	flag.IntVar(&c.AfterSeq, "after-seq", 0, "resume apply after the last successfully copied checkpoint")
	flag.IntVar(&c.BatchSize, "batch-size", 100, "reports per batch, 1..200")
	flag.IntVar(&c.MaxBatches, "max-batches", 0, "pause apply after this many batches; 0 completes the bounded scope")
	flag.DurationVar(&c.Timeout, "batch-timeout", 10*time.Second, "deadline per selection/copy/audit, 1s..1m")
	flag.DurationVar(&c.Pause, "pause", 250*time.Millisecond, "pause between batches, 0..1m")
	version := flag.Bool("version", false, "print build source without contacting the database")
	flag.Parse()
	if *version {
		fmt.Println(BuildSHA)
		return
	}
	if flag.NArg() != 0 || c.validate() != nil {
		fail("configuration_invalid")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, c); err != nil {
		fail(err.Error())
	}
}

func run(ctx context.Context, c config) error {
	password, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return errors.New("database_password_unavailable")
	}
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = appenv.String("DB_USER", "server"), password
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(appenv.String("DB_HOST", "cleanapp_db"), appenv.String("DB_PORT", "3306"))
	cfg.DBName, cfg.ParseTime = appenv.String("DB_NAME", "cleanapp"), true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = c.Timeout, c.Timeout, c.Timeout
	cfg.Params = map[string]string{"innodb_lock_wait_timeout": "5", "lock_wait_timeout": "5"}
	// Driver errors may contain transport details. Do not log driver/SQL errors,
	// credentials, DSNs, source payloads, report IDs or model content.
	cfg.Logger = &mysql.NopLogger{}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return errors.New("database_open_failed")
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	readyCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	err = verifyReady(readyCtx, db)
	cancel()
	if err != nil {
		return errors.New("projection_migrations_not_ready")
	}
	return execute(ctx, c, sqlStore{db}, func(e event) { _ = json.NewEncoder(os.Stdout).Encode(e) })
}

func execute(ctx context.Context, c config, store projectionStore, emit func(event)) error {
	if err := c.validate(); err != nil {
		return err
	}
	checkpoint, processed := c.AfterSeq, 0
	output := func(stage string, counts *coverage, duration time.Duration) {
		emit(event{Stage: stage, SourceSHA: BuildSHA, At: time.Now().UTC().Format(time.RFC3339Nano), Brand: c.Brand, FromSeq: c.FromSeq, ThroughSeq: c.ThroughSeq, AfterSeq: checkpoint, Processed: processed, DurationMS: duration.Milliseconds(), Counts: counts})
	}
	output("started", nil, 0)
	if c.Mode == "apply" {
		for batches := 0; ; batches++ {
			ids, err := selectBounded(ctx, c, store, max(checkpoint, c.FromSeq-1))
			if err != nil {
				output("selection_failed", nil, 0)
				return errors.New("cohort_selection_failed")
			}
			if len(ids) == 0 {
				break
			}
			if c.MaxBatches > 0 && batches >= c.MaxBatches {
				output("paused", nil, 0)
				return nil
			}
			batchCtx, cancel := context.WithTimeout(ctx, c.Timeout)
			began := time.Now()
			err = store.backfill(batchCtx, ids)
			cancel()
			if err != nil {
				output("copy_failed", nil, time.Since(began))
				return errors.New("projection_batch_failed")
			}
			// Advance only once all three projection copies have committed. A
			// partial/interrupted batch can safely be replayed from prior output.
			checkpoint, processed = ids[len(ids)-1], processed+len(ids)
			output("copied", nil, time.Since(began))
			if err := pause(ctx, c.Pause); err != nil {
				return errors.New("projection_copy_interrupted")
			}
		}
	}
	// Resume affects copying only. Completion always audits the whole approved
	// brand/range, including rows before the supplied checkpoint.
	var total coverage
	for after := c.FromSeq - 1; ; {
		ids, err := selectBounded(ctx, c, store, max(0, after))
		if err != nil {
			return errors.New("audit_selection_failed")
		}
		if len(ids) == 0 {
			break
		}
		auditCtx, cancel := context.WithTimeout(ctx, c.Timeout)
		counts, err := store.audit(auditCtx, ids)
		cancel()
		if err != nil {
			return errors.New("projection_audit_failed")
		}
		total.add(counts)
		after = ids[len(ids)-1]
		if err := pause(ctx, c.Pause); err != nil {
			return errors.New("projection_audit_interrupted")
		}
	}
	output("audit", &total, 0)
	if total.Reports == 0 {
		return errors.New("cohort_empty")
	}
	if !total.complete() {
		return errors.New("projection_coverage_mismatch")
	}
	output("complete", &total, 0)
	return nil
}

func selectBounded(ctx context.Context, c config, store projectionStore, after int) ([]int, error) {
	batchCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	ids, err := store.selectBatch(batchCtx, c, after)
	if err != nil || len(ids) > c.BatchSize {
		return nil, errors.New("cohort_read_failed")
	}
	for i, seq := range ids {
		if seq <= after || seq < c.FromSeq || seq > c.ThroughSeq || (i > 0 && seq <= ids[i-1]) {
			return nil, errors.New("cohort_bounds_invalid")
		}
	}
	return ids, nil
}

func pause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type sqlStore struct{ db *sql.DB }

func (s sqlStore) selectBatch(ctx context.Context, c config, after int) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT seq FROM report_analysis FORCE INDEX (idx_ra_brand_valid_class_core)
 WHERE brand_name=? AND is_valid=TRUE AND classification='digital' AND seq>? AND seq>=? AND seq<=?
 ORDER BY seq LIMIT ?`, c.Brand, after, c.FromSeq, c.ThroughSeq, c.BatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		ids = append(ids, seq)
	}
	return ids, rows.Err()
}

func (s sqlStore) backfill(ctx context.Context, ids []int) error {
	if _, err := reportmeta.Backfill(ctx, s.db, ids); err != nil {
		return err
	}
	if _, err := eiiheader.Backfill(ctx, s.db, ids); err != nil {
		return err
	}
	_, err := wiremeta.Backfill(ctx, s.db, ids)
	return err
}

func (s sqlStore) audit(ctx context.Context, ids []int) (coverage, error) {
	var c coverage
	if len(ids) == 0 || len(ids) > 200 {
		return c, errors.New("audit_batch_invalid")
	}
	content, _ := json.Marshal(ids)
	wireComparisons := []string{"NOT(BINARY w.submission_id <=> BINARY h.submission_id)", "NOT(w.report_seq <=> h.report_seq)", "NOT(w.observed_at <=> h.observed_at)", "NOT(w.submitted_at <=> h.submitted_at)", "NOT(BINARY w.actor_kind <=> BINARY h.actor_kind)", "NOT(BINARY w.channel <=> BINARY h.channel)"}
	nodes := wiremeta.NodeExpressions("w")
	for i, name := range wiremeta.NodeColumns() {
		// JSON assignment parses CASE URI expressions, which have a SQL string
		// result. Compare that same typed serialization, preserving SQL NULL,
		// JSON null and INTEGER versus DOUBLE distinctions.
		typed := "CAST(" + nodes[i] + " AS JSON)"
		wireComparisons = append(wireComparisons, "NOT(JSON_TYPE("+typed+") <=> JSON_TYPE(h."+name+")) OR NOT(BINARY CAST("+typed+" AS CHAR CHARACTER SET utf8mb4) <=> BINARY CAST(h."+name+" AS CHAR CHARACTER SET utf8mb4))")
	}
	err := s.db.QueryRowContext(ctx, `WITH cohort AS (
 SELECT seq FROM JSON_TABLE(?, '$[*]' COLUMNS(seq INT PATH '$' ERROR ON ERROR)) ids
)
SELECT
 (SELECT COUNT(*) FROM cohort),
 (SELECT COUNT(*) FROM cohort c LEFT JOIN reports r ON r.seq=c.seq WHERE r.seq IS NULL),
 (SELECT COUNT(*) FROM cohort c JOIN reports r ON r.seq=c.seq LEFT JOIN report_metadata_projection m ON m.seq=c.seq WHERE m.seq IS NULL),
 (SELECT COUNT(*) FROM cohort c STRAIGHT_JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=c.seq
  LEFT JOIN external_ingest_header_projection h ON h.seq=e.seq AND h.source=e.source AND h.external_id=e.external_id WHERE h.seq IS NULL),
 (SELECT COUNT(*) FROM cohort c STRAIGHT_JOIN wire_submissions_raw w ON w.report_seq=c.seq
  LEFT JOIN wire_evidence_projection h ON h.submission_id=w.submission_id AND h.report_seq=w.report_seq WHERE h.submission_id IS NULL),
 (SELECT COUNT(*) FROM cohort c JOIN reports r ON r.seq=c.seq JOIN report_metadata_projection m ON m.seq=c.seq
  WHERE NOT(BINARY r.id <=> BINARY m.id) OR NOT(BINARY r.public_id <=> BINARY m.public_id) OR NOT(r.ts <=> m.ts)
   OR NOT(r.latitude <=> m.latitude) OR NOT(r.longitude <=> m.longitude) OR NOT((COALESCE(LENGTH(r.image),0)>0) <=> m.has_original_media)),
 (SELECT COUNT(*) FROM cohort c STRAIGHT_JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=c.seq
  JOIN external_ingest_header_projection h ON h.seq=e.seq AND h.source=e.source AND h.external_id=e.external_id
  WHERE NOT(BINARY e.source <=> BINARY h.source) OR NOT(BINARY e.external_id <=> BINARY h.external_id)
   OR NOT(BINARY e.source_url <=> BINARY h.source_url) OR NOT(e.source_timestamp <=> h.source_timestamp) OR NOT(e.created_at <=> h.created_at)),
 (SELECT COUNT(*) FROM cohort c STRAIGHT_JOIN wire_submissions_raw w ON w.report_seq=c.seq
  JOIN wire_evidence_projection h ON h.submission_id=w.submission_id AND h.report_seq=w.report_seq
  WHERE `+strings.Join(wireComparisons, " OR ")+`)`, string(content)).Scan(&c.Reports, &c.MissingParents, &c.MissingReports, &c.MissingExternal, &c.MissingWire, &c.StaleReports, &c.StaleExternal, &c.StaleWire)
	return c, err
}

func verifyReady(ctx context.Context, db *sql.DB) error {
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE()
 AND TABLE_NAME IN ('report_metadata_projection','external_ingest_header_projection','wire_evidence_projection')`).Scan(&tables); err != nil || tables != 3 {
		return errors.New("projection_tables_missing")
	}
	want := map[string][2]string{
		"reports_metadata_after_insert": {"reports", "INSERT"}, "reports_metadata_after_update": {"reports", "UPDATE"}, "reports_metadata_after_delete": {"reports", "DELETE"},
		"eii_header_after_insert": {"external_ingest_index", "INSERT"}, "eii_header_after_update": {"external_ingest_index", "UPDATE"}, "eii_header_after_delete": {"external_ingest_index", "DELETE"},
		"wire_evidence_after_insert": {"wire_submissions_raw", "INSERT"}, "wire_evidence_after_update": {"wire_submissions_raw", "UPDATE"}, "wire_evidence_after_delete": {"wire_submissions_raw", "DELETE"},
	}
	rows, err := db.QueryContext(ctx, `SELECT TRIGGER_NAME,EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION
 FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()
 AND TRIGGER_NAME IN ('reports_metadata_after_insert','reports_metadata_after_update','reports_metadata_after_delete',
 'eii_header_after_insert','eii_header_after_update','eii_header_after_delete',
 'wire_evidence_after_insert','wire_evidence_after_update','wire_evidence_after_delete')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, table, timing, manipulation string
		if err := rows.Scan(&name, &table, &timing, &manipulation); err != nil {
			return err
		}
		if expected, ok := want[name]; !ok || timing != "AFTER" || expected != [2]string{table, manipulation} {
			return errors.New("projection_trigger_contract_invalid")
		}
		delete(want, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(want) != 0 {
		return errors.New("projection_triggers_missing")
	}
	return nil
}

func fail(code string) {
	// Only static error codes from run/execute reach this boundary.
	fmt.Fprintln(os.Stderr, code)
	os.Exit(1)
}
