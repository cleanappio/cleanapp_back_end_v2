package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"cleanapp-common/reportmeta"
	"github.com/go-sql-driver/mysql"
)

// Runs only against a dedicated local disposable fixture, never an app database.
func TestMetadataProjectionMySQLFixture(t *testing.T) {
	dsn := os.Getenv("METADATA_TEST_DSN")
	if dsn == "" {
		t.Skip("set METADATA_TEST_DSN to a dedicated local MySQL database ending _evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "metadata_projection_evidence_test" {
		t.Fatal("fixture only permits dedicated local metadata_projection_evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE reports (seq INT PRIMARY KEY,id VARCHAR(255) NOT NULL,public_id VARCHAR(32) NULL,ts TIMESTAMP NULL,latitude FLOAT NOT NULL,longitude FLOAT NOT NULL,image LONGBLOB NULL,description TEXT) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	defer db.Exec(`DROP TABLE reports`)
	defer db.Exec(`DROP TABLE report_metadata_projection`)
	exec(`CREATE TRIGGER reports_public_id_before_insert BEFORE INSERT ON reports FOR EACH ROW SET NEW.public_id=COALESCE(NEW.public_id,CONCAT('r_',NEW.seq))`)
	exec(`CREATE TRIGGER reports_public_id_before_update BEFORE UPDATE ON reports FOR EACH ROW SET NEW.public_id=COALESCE(NEW.public_id,CONCAT('r_',NEW.seq))`)
	exec(`INSERT INTO reports VALUES (1,'legacy',NULL,NULL,47.3768866,8.541694,NULL,'raw retained')`)
	var beforeName, beforeDDL string
	if err := db.QueryRow(`SHOW CREATE TABLE reports`).Scan(&beforeName, &beforeDDL); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := reportmeta.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var afterName, afterDDL string
	if err := db.QueryRow(`SHOW CREATE TABLE reports`).Scan(&afterName, &afterDDL); err != nil {
		t.Fatal(err)
	}
	if beforeDDL != afterDDL {
		t.Fatal("metadata migration altered legacy reports schema")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration unexpectedly backfilled history: count=%d err=%v", count, err)
	}
	var existingBeforeTriggers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND ACTION_TIMING='BEFORE'`).Scan(&existingBeforeTriggers); err != nil || existingBeforeTriggers != 2 {
		t.Fatalf("existing public ID triggers changed: count=%d err=%v", existingBeforeTriggers, err)
	}
	if n, err := reportmeta.Backfill(context.Background(), db, []int{1}); err != nil || n != 1 {
		t.Fatalf("backfill: inserted=%d err=%v", n, err)
	}
	assertEqual := func(seq int) {
		t.Helper()
		var equal int
		if err := db.QueryRow(`SELECT COUNT(*) FROM reports r JOIN report_metadata_projection m ON m.seq=r.seq
			WHERE r.seq=? AND BINARY r.id=BINARY m.id AND r.public_id <=> m.public_id AND r.ts <=> m.ts
			AND r.latitude=m.latitude AND r.longitude=m.longitude AND m.has_original_media=(COALESCE(LENGTH(r.image),0)>0)`, seq).Scan(&equal); err != nil || equal != 1 {
			t.Fatalf("raw/projection values diverged for seq%d: equal=%d err=%v", seq, equal, err)
		}
	}
	assertEqual(1)
	if n, err := reportmeta.Backfill(context.Background(), db, []int{1}); err != nil || n != 0 {
		t.Fatalf("backfill was not idempotent: inserted=%d err=%v", n, err)
	}
	// A trigger migration can fail partway through without changing legacy data.
	// Reconciliation must repair projection rows created before all three
	// maintenance triggers were installed, rather than ignoring a stale row.
	exec(`UPDATE report_metadata_projection SET id='stale',public_id=NULL,latitude=0,has_original_media=TRUE WHERE seq=1`)
	if n, err := reportmeta.Backfill(context.Background(), db, []int{1}); err != nil || n != 2 {
		t.Fatalf("stale partial-migration row was not repaired: affected=%d err=%v", n, err)
	}
	assertEqual(1)
	exec(`INSERT INTO reports VALUES (2,'reddit_MixedCase',NULL,'2025-09-01 12:34:56',-33.1234567,151.9876543,X'000102','retained')`)
	assertEqual(2)
	exec(`UPDATE reports SET seq=3,id='updated',public_id=NULL,ts=NULL,latitude=0,longitude=0,image='' WHERE seq=2`)
	assertEqual(3)
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection WHERE seq=2`).Scan(&count); err != nil || count != 0 {
		t.Fatal("primary key update retained stale old projection")
	}
	exec(`DELETE FROM reports WHERE seq=3`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection WHERE seq=3`).Scan(&count); err != nil || count != 0 {
		t.Fatal("delete retained report projection")
	}
	// INSERT...SELECT at REPEATABLE READ must retain a record lock until commit.
	// A fixture-only delay makes simultaneous DELETE/UPDATE deterministic without
	// adding any production hooks or exposing original media to the application.
	exec(`CREATE TRIGGER metadata_fixture_delay AFTER INSERT ON report_metadata_projection FOR EACH ROW BEGIN
		DO GET_LOCK('cleanapp_metadata_fixture_gate',5); DO SLEEP(0.6); DO RELEASE_LOCK('cleanapp_metadata_fixture_gate'); END`)
	defer db.Exec(`DROP TRIGGER IF EXISTS metadata_fixture_delay`)
	for _, operation := range []string{"delete", "update"} {
		seq := 10
		if operation == "update" {
			seq = 11
		}
		exec(`INSERT INTO reports VALUES (?,'concurrent','original',NULL,1.1234567,2.7654321,'','retained')`, seq)
		exec(`DELETE FROM report_metadata_projection WHERE seq=?`, seq)
		backfilled := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := reportmeta.Backfill(ctx, db, []int{seq})
			backfilled <- err
		}()
		deadline := time.Now().Add(2 * time.Second)
		for {
			select {
			case err := <-backfilled:
				t.Fatalf("backfill completed before the fixture concurrency gate was observable: %v", err)
			default:
			}
			var gateHeld int
			if err := db.QueryRow(`SELECT IS_USED_LOCK('cleanapp_metadata_fixture_gate') IS NOT NULL`).Scan(&gateHeld); err != nil {
				t.Fatal(err)
			}
			if gateHeld > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("backfill did not reach the fixture concurrency gate")
			}
			time.Sleep(10 * time.Millisecond)
		}
		changed := make(chan error, 1)
		go func() {
			query := `DELETE FROM reports WHERE seq=?`
			if operation == "update" {
				query = `UPDATE reports SET id='current',public_id='current',ts='2026-10-14',image=X'ABCD' WHERE seq=?`
			}
			_, err := db.Exec(query, seq)
			changed <- err
		}()
		select {
		case err := <-changed:
			t.Fatalf("concurrent%s bypassed the source row lock: %v", operation, err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := <-backfilled; err != nil {
			t.Fatal(err)
		}
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
		if operation == "update" {
			assertEqual(seq)
		} else if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection WHERE seq=?`, seq).Scan(&count); err != nil || count != 0 {
			t.Fatal("backfill resurrected concurrently deleted report metadata")
		}
	}
}
