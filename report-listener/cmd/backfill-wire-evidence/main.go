// This release helper copies only the approved evidence rollout cohort into a
// Wire scalar projection. It never calls a model or prints source payloads.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"cleanapp-common/appenv"
	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

const (
	throughSeq        = 1344225
	expectedReports   = 22262
	batchSize         = 200
	expectedCohortSHA = "5dd45fe6620aa44f4a03a3cdc992af4843117a8f16070961c96ee49e9fb32585"
	batchTimeout      = 60 * time.Second
	batchPause        = 250 * time.Millisecond
)

type cohort struct {
	ThroughSeq int   `json:"through_seq"`
	Sequences  []int `json:"sequences"`
}

type auditCounts struct {
	ParentRows             int64 `json:"parent_rows"`
	HeaderRows             int64 `json:"header_rows"`
	MissingHeaders         int64 `json:"missing_headers"`
	MissingParents         int64 `json:"missing_parents"`
	StaleHeaders           int64 `json:"stale_headers"`
	IgnoredUnlinkedHeaders int64 `json:"ignored_unlinked_headers"`
}

func main() {
	mode := flag.String("mode", "audit", "apply or audit; apply requires the existing Wire projection migration")
	seqFile := flag.String("seq-file", "", "approved private metadata-projection/cohort.json")
	flag.Parse()
	if *seqFile == "" || (*mode != "apply" && *mode != "audit") || flag.NArg() != 0 {
		fail("configuration_invalid")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *mode, *seqFile); err != nil {
		fail(err.Error())
	}
}

func run(ctx context.Context, mode, seqFile string) error {
	seqs, err := loadCohort(seqFile)
	if err != nil {
		return err
	}
	password, err := appenv.Secret("DB_PASSWORD", "")
	if err != nil {
		return errors.New("database_password_unavailable")
	}
	cfg := mysql.NewConfig()
	cfg.User = appenv.String("DB_USER", "server")
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(appenv.String("DB_HOST", "cleanapp_db"), appenv.String("DB_PORT", "3306"))
	cfg.DBName = appenv.String("DB_NAME", "cleanapp")
	cfg.ParseTime = true
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = batchTimeout
	cfg.WriteTimeout = batchTimeout
	cfg.Params = map[string]string{"innodb_lock_wait_timeout": "5", "lock_wait_timeout": "5"}
	// The driver's default stderr logger can expose raw transport errors.
	// Preserve the no-op logger via a connector, without serializing a DSN.
	cfg.Logger = &mysql.NopLogger{}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return errors.New("database_open_failed")
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	readyCtx, cancel := context.WithTimeout(ctx, batchTimeout)
	err = verifyReady(readyCtx, db)
	cancel()
	if err != nil {
		return errors.New("wire_projection_migration_not_ready")
	}
	if mode == "apply" {
		// The admin migration must already have created the table and triggers.
		// The shared call verifies their exact definitions and captured SQL mode.
		migrateCtx, cancel := context.WithTimeout(ctx, batchTimeout)
		err = wiremeta.Migrate(migrateCtx, db)
		cancel()
		if err != nil {
			return errors.New("wire_projection_migration_verification_failed")
		}
		emit(map[string]any{"stage": "migration_verified", "reports": len(seqs), "through_seq": throughSeq})
	}
	var affected int64
	if mode == "apply" {
		for start := 0; start < len(seqs); start += batchSize {
			end := min(start+batchSize, len(seqs))
			batchCtx, cancel := context.WithTimeout(ctx, batchTimeout)
			began := time.Now()
			count, err := wiremeta.Backfill(batchCtx, db, seqs[start:end])
			cancel()
			if err != nil {
				emit(map[string]any{"stage": "batch_failed", "processed": start, "batch_reports": end - start})
				return errors.New("wire_projection_batch_failed")
			}
			affected += count
			emit(map[string]any{"stage": "copied", "processed": end, "affected_rows": affected, "batch_affected_rows": count, "duration_ms": time.Since(began).Milliseconds()})
			if end < len(seqs) && pause(ctx) != nil {
				return errors.New("wire_projection_backfill_interrupted")
			}
		}
	}
	var total auditCounts
	for start := 0; start < len(seqs); start += batchSize {
		end := min(start+batchSize, len(seqs))
		auditCtx, cancel := context.WithTimeout(ctx, batchTimeout)
		counts, err := auditBatch(auditCtx, db, seqs[start:end])
		cancel()
		if err != nil {
			return errors.New("wire_projection_final_audit_failed")
		}
		total.ParentRows += counts.ParentRows
		total.HeaderRows += counts.HeaderRows
		total.MissingHeaders += counts.MissingHeaders
		total.MissingParents += counts.MissingParents
		total.StaleHeaders += counts.StaleHeaders
		total.IgnoredUnlinkedHeaders += counts.IgnoredUnlinkedHeaders
		if end < len(seqs) && pause(ctx) != nil {
			return errors.New("wire_projection_audit_interrupted")
		}
	}
	emit(map[string]any{"stage": "audit", "reports": len(seqs), "through_seq": throughSeq, "affected_rows": affected, "counts": total})
	if !total.equal() {
		return errors.New("wire_projection_final_audit_mismatch")
	}
	emit(map[string]any{"stage": "complete", "reports": len(seqs), "through_seq": throughSeq, "affected_rows": affected, "counts": total})
	return nil
}

func loadCohort(path string) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cohort_snapshot_read_failed")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4<<20 {
		return nil, errors.New("cohort_snapshot_not_private")
	}
	content, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(content) > 4<<20 {
		return nil, errors.New("cohort_snapshot_read_failed")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != expectedCohortSHA {
		return nil, errors.New("cohort_snapshot_hash_mismatch")
	}
	return validateCohort(content)
}

func validateCohort(content []byte) ([]int, error) {
	var s cohort
	if err := json.Unmarshal(content, &s); err != nil || s.ThroughSeq != throughSeq || len(s.Sequences) != expectedReports || !sort.IntsAreSorted(s.Sequences) {
		return nil, errors.New("cohort_snapshot_invalid")
	}
	for i, seq := range s.Sequences {
		if seq <= 0 || seq > throughSeq || (i > 0 && s.Sequences[i-1] == seq) {
			return nil, errors.New("cohort_sequence_invalid")
		}
	}
	return s.Sequences, nil
}

func verifyReady(ctx context.Context, db *sql.DB) error {
	var tables, triggers int
	err := db.QueryRowContext(ctx, `SELECT
	 (SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='wire_evidence_projection'),
	 (SELECT COUNT(*) FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND EVENT_OBJECT_TABLE='wire_submissions_raw' AND ACTION_TIMING='AFTER' AND TRIGGER_NAME IN ('wire_evidence_after_insert','wire_evidence_after_update','wire_evidence_after_delete'))`).Scan(&tables, &triggers)
	if err != nil || tables != 1 || triggers != 3 {
		return errors.New("wire_projection_migration_not_ready")
	}
	return nil
}

func auditSQL() string {
	comparisons := []string{"NOT(BINARY ws.submission_id <=> BINARY h.submission_id)", "NOT(ws.report_seq <=> h.report_seq)", "NOT(ws.observed_at <=> h.observed_at)", "NOT(ws.submitted_at <=> h.submitted_at)", "NOT(BINARY ws.actor_kind <=> BINARY h.actor_kind)", "NOT(BINARY ws.channel <=> BINARY h.channel)"}
	nodes := wiremeta.NodeExpressions("ws")
	for i, name := range wiremeta.NodeColumns() {
		comparisons = append(comparisons, "NOT("+nodes[i]+" <=> h."+name+")")
	}
	return `WITH cohort AS (
	SELECT seq FROM JSON_TABLE(?, '$[*]' COLUMNS(seq INT PATH '$' ERROR ON ERROR)) c
), parent_counts AS (
	SELECT COUNT(*) parent_rows, COALESCE(SUM(h.submission_id IS NULL),0) missing_headers,
	 COALESCE(SUM(h.submission_id IS NOT NULL AND (` + strings.Join(comparisons, " OR ") + `)),0) stale_headers
	FROM cohort c STRAIGHT_JOIN wire_submissions_raw ws ON ws.report_seq=c.seq
	LEFT JOIN wire_evidence_projection h ON h.submission_id=ws.submission_id AND h.report_seq=ws.report_seq
), header_counts AS (
	SELECT COALESCE(SUM(ws.submission_id IS NULL OR ws.report_seq IS NOT NULL),0) header_rows,
	 COALESCE(SUM(ws.submission_id IS NULL OR (ws.report_seq IS NOT NULL AND NOT(ws.report_seq <=> h.report_seq))),0) missing_parents,
	 COALESCE(SUM(ws.submission_id IS NOT NULL AND ws.report_seq IS NULL),0) ignored_unlinked_headers
	FROM cohort c STRAIGHT_JOIN wire_evidence_projection h ON h.report_seq=c.seq
	LEFT JOIN wire_submissions_raw ws ON ws.submission_id=h.submission_id
)
SELECT /*+ MAX_EXECUTION_TIME(60000) */ parent_rows,header_rows,missing_headers,missing_parents,stale_headers,ignored_unlinked_headers
FROM parent_counts CROSS JOIN header_counts`
}

func auditBatch(ctx context.Context, db *sql.DB, seqs []int) (auditCounts, error) {
	var counts auditCounts
	if len(seqs) == 0 || len(seqs) > batchSize {
		return counts, errors.New("audit_batch_invalid")
	}
	content, err := json.Marshal(seqs)
	if err != nil {
		return counts, errors.New("audit_scope_encoding_failed")
	}
	err = db.QueryRowContext(ctx, auditSQL(), string(content)).Scan(&counts.ParentRows, &counts.HeaderRows, &counts.MissingHeaders, &counts.MissingParents, &counts.StaleHeaders, &counts.IgnoredUnlinkedHeaders)
	return counts, err
}

func (counts auditCounts) equal() bool {
	return counts.ParentRows == counts.HeaderRows && counts.MissingHeaders == 0 && counts.MissingParents == 0 && counts.StaleHeaders == 0
}

func pause(ctx context.Context) error {
	timer := time.NewTimer(batchPause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Output contains numeric counts/stages and static error categories only.
// Never emit driver errors, DSNs, credentials, sequence IDs or source payloads.
func emit(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }

func fail(category string) {
	fmt.Printf("{\"error\":%q}\n", category)
	os.Exit(1)
}
