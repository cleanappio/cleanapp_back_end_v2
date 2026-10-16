// refresh-evidence-sources explicitly warms a bounded derived source projection.
// Original reports, visibility, case data and notification queues are untouched.
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
	"cleanapp-common/evidencesourceprojection"
	"github.com/go-sql-driver/mysql"
	"report-listener/database"
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

type coverage database.EvidenceSourceProjectionCoverage

func (c coverage) complete() bool { return c.MissingParents == 0 && c.Missing == 0 && c.Dirty == 0 }
func (c *coverage) add(v coverage) {
	c.Reports += v.Reports
	c.MissingParents += v.MissingParents
	c.Missing += v.Missing
	c.Dirty += v.Dirty
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
	flag.StringVar(&c.Mode, "mode", "audit", "audit (read-only) or apply (derived source projection only)")
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
	err = evidencesourceprojection.Verify(readyCtx, db)
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
			// Advance only once the whole generation-fenced batch has committed. A
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
	auditConfig := c
	auditConfig.Mode = "audit"
	for after := c.FromSeq - 1; ; {
		ids, err := selectBounded(ctx, auditConfig, store, max(0, after))
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
	dirty := ""
	if c.Mode == "apply" {
		dirty = " AND NOT COALESCE((" + evidencesourceprojection.CleanPredicate("p") + "),FALSE)"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT /*+ MAX_EXECUTION_TIME(8000) */ DISTINCT ra.seq
 FROM report_analysis ra FORCE INDEX(idx_ra_brand_valid_class_core)
 LEFT JOIN `+evidencesourceprojection.Table+` p ON p.seq=ra.seq
 WHERE ra.brand_name=? AND ra.is_valid=TRUE AND ra.classification='digital'
 AND ra.seq>=? AND ra.seq>? AND ra.seq<=?`+dirty+` ORDER BY ra.seq LIMIT ?`, c.Brand, c.FromSeq, after, c.ThroughSeq, c.BatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
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
	result, err := database.RefreshEvidenceSourceProjection(ctx, s.db, ids)
	if err != nil {
		return err
	}
	if result.Computed != len(ids) || result.MissingParents != 0 {
		return errors.New("source_projection_parent_missing")
	}
	return nil
}
func (s sqlStore) audit(ctx context.Context, ids []int) (coverage, error) {
	result, err := database.AuditEvidenceSourceProjection(ctx, s.db, ids)
	return coverage(result), err
}
func fail(code string) { fmt.Fprintln(os.Stderr, code); os.Exit(1) }
