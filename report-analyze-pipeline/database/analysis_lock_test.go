package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func analysisLockTestDB(t *testing.T) *Database {
	t.Helper()
	dsn := os.Getenv("REPORT_ANALYSIS_TEST_DSN")
	if dsn == "" {
		t.Skip("REPORT_ANALYSIS_TEST_DSN not set; requires a disposable MySQL database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test database DSN")
	}
	if !strings.HasPrefix(cfg.DBName, "cleanapp_analysis_test_") {
		t.Fatal("test database must have cleanapp_analysis_test_ prefix")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatal("cannot reach disposable test database")
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS report_raw (report_seq INT PRIMARY KEY, visibility VARCHAR(16) NOT NULL DEFAULT 'public', analysed_published_at TIMESTAMP NULL)`); err != nil {
		t.Fatal(err)
	}
	return &Database{db: db}
}

func TestReportAnalysisLockSerializesQueuedReplay(t *testing.T) {
	db := analysisLockTestDB(t)
	const seq = 81001
	if _, err := db.db.Exec(`INSERT INTO report_raw (report_seq, analysed_published_at) VALUES (?, NULL) ON DUPLICATE KEY UPDATE analysed_published_at = NULL`, seq); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.db.Exec(`DELETE FROM report_raw WHERE report_seq = ?`, seq) })

	first, published, err := db.LockReportAnalysis(context.Background(), seq)
	if err != nil || published {
		t.Fatalf("first acquisition published=%t err=%v", published, err)
	}
	t.Cleanup(func() { _ = first.Release() })
	type result struct {
		lock      *ReportAnalysisLock
		published bool
		err       error
	}
	secondResult := make(chan result, 1)
	started := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		close(started)
		lock, published, err := db.LockReportAnalysis(ctx, seq)
		secondResult <- result{lock, published, err}
	}()
	<-started
	select {
	case r := <-secondResult:
		if r.lock != nil {
			_ = r.lock.Release()
		}
		t.Fatalf("overlapping worker escaped held lock: %v", r.err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := db.db.Exec(`UPDATE report_raw SET analysed_published_at=UTC_TIMESTAMP() WHERE report_seq=?`, seq); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-secondResult:
		if r.err != nil {
			t.Fatal(r.err)
		}
		defer r.lock.Release()
		if !r.published {
			t.Fatal("overlapping worker did not see publication after first worker completed")
		}
		if err := r.lock.Release(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("overlapping worker did not acquire released lock")
	}
	var free int
	if err := db.db.QueryRow(`SELECT IS_FREE_LOCK(?)`, first.name).Scan(&free); err != nil || free != 1 {
		t.Fatalf("lock leaked after release: free=%d err=%v", free, err)
	}
}

func TestReportAnalysisLockKeepsShadowAndMissingRawRowsEligible(t *testing.T) {
	db := analysisLockTestDB(t)
	const seq = 81002
	if _, err := db.db.Exec(`INSERT INTO report_raw (report_seq, visibility, analysed_published_at) VALUES (?, 'shadow', NULL) ON DUPLICATE KEY UPDATE visibility='shadow', analysed_published_at=NULL`, seq); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.db.Exec(`DELETE FROM report_raw WHERE report_seq=?`, seq) })
	for _, candidate := range []int{seq, 81003} {
		lock, published, err := db.LockReportAnalysis(context.Background(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		if published {
			t.Fatalf("unpublished report %d was treated as complete", candidate)
		}
		if err := lock.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReportAnalysisLockFailsClosedOnMarkerReadError(t *testing.T) {
	db := analysisLockTestDB(t)
	db.db.SetMaxOpenConns(1)
	// This connection-local table hides the fixture only on this test's session.
	// It exercises the real missing-marker-column error without changing schema
	// seen by another package or worker.
	if _, err := db.db.Exec(`CREATE TEMPORARY TABLE report_raw (report_seq INT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	lock, published, err := db.LockReportAnalysis(context.Background(), 81004)
	if err == nil || lock != nil || published {
		t.Fatalf("failed marker read accepted: lock=%v published=%t err=%v", lock, published, err)
	}
	var free int
	if err := db.db.QueryRow(`SELECT IS_FREE_LOCK('cleanapp:report-analysis:81004')`).Scan(&free); err != nil || free != 1 {
		t.Fatalf("lock leaked after marker read error: free=%d err=%v", free, err)
	}
}
