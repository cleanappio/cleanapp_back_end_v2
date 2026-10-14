package wiremeta

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const strictMode = "STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,NO_ENGINE_SUBSTITUTION"

func expectSession(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT @@SESSION.sql_mode,@@SESSION.lock_wait_timeout,@@SESSION.innodb_lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"mode", "metadata", "row"}).AddRow(strictMode, 60, 50))
	mock.ExpectExec(`SET SESSION sql_mode=\?,lock_wait_timeout=\?,innodb_lock_wait_timeout=\?`).WithArgs("STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION", 5, 5).WillReturnResult(sqlmock.NewResult(0, 0))
}
func expectRestore(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`SET SESSION sql_mode=\?,lock_wait_timeout=\?,innodb_lock_wait_timeout=\?`).WithArgs(strictMode, 60, 50).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestProjectionPreservesStrictModeLiteralPathsAndChildOnlyConstraint(t *testing.T) {
	if copyMode(strictMode) != "STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION" {
		t.Fatal("removed caller validation unrelated to historical zero dates")
	}
	ddl := createTableSQL()
	if strings.Contains(ddl, "REFERENCES reports") || strings.Contains(ddl, "ALTER") || strings.Count(ddl, "FOREIGN KEY") != 1 || !strings.Contains(ddl, "ON DELETE CASCADE ON UPDATE CASCADE") {
		t.Fatal("projection introduced an intake constraint or omitted cascade deletion")
	}
	if normalizeBody("JSON_EXTRACT(NEW.report_json,'$.privacy.contains_pii')") == normalizeBody("JSON_EXTRACT(NEW.report_json,'$.privacy.Contains_PII')") {
		t.Fatal("named trigger verification ignored JSON path case")
	}
	columns := NodeColumns()
	columns[0] = "tampered"
	if NodeColumns()[0] != "platform" {
		t.Fatal("caller changed shared projection contract")
	}
	for _, trigger := range projectionTriggers() {
		if strings.Contains(trigger.body, "UPDATE wire_submissions_raw") || strings.Contains(trigger.body, "ALTER") {
			t.Fatal("projection changed parent history/schema")
		}
		if trigger.event != "DELETE" && (!strings.Contains(trigger.body, "REPLACE INTO wire_evidence_projection") || strings.Contains(trigger.body, "ON DUPLICATE KEY UPDATE")) {
			t.Fatal("trigger retained equal-value JSON update instead of exact node replacement")
		}
	}
}

func TestNodeExpressionsPreserveAbsentAndLiteralNullBeforeURIGate(t *testing.T) {
	nodes := NodeExpressions("ws")
	if len(nodes) != 15 || len(NodeColumns()) != 15 {
		t.Fatal("source node contract changed")
	}
	for i, node := range nodes {
		if i >= 2 && i <= 5 {
			if !strings.Contains(node, "IS NULL THEN NULL WHEN JSON_UNQUOTE(") || !strings.Contains(node, ")='null' OR LOWER(LEFT(TRIM(JSON_UNQUOTE(") || !strings.Contains(node, "REGEXP '^https?://'") || !strings.Contains(node, "ELSE JSON_QUOTE('__non_http_source_uri__') END") {
				t.Fatal("URI extraction changed null/fallback semantics")
			}
		} else if strings.Contains(node, "CASE") {
			t.Fatal("altered a raw provenance/privacy node")
		}
	}
}

func TestBackfillIsBoundedLockingAndRestoresSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n, err := Backfill(context.Background(), db, nil); err != nil || n != 0 {
		t.Fatal("empty cohort accessed DB")
	}
	if _, err := Backfill(context.Background(), db, make([]int, 201)); err == nil {
		t.Fatal("accepted unbounded cohort")
	}
	expectSession(mock)
	mock.ExpectBegin()
	mock.ExpectExec(`REPLACE INTO wire_evidence_projection .* FROM wire_submissions_raw ws WHERE ws.report_seq IN \(\?,\?\) FOR SHARE$`).WithArgs(10, 11).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	expectRestore(mock)
	if n, err := Backfill(context.Background(), db, []int{10, 11}); err != nil || n != 2 {
		t.Fatalf("bounded copy failed: %d %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillFailureRollsBackAndDiscardsUnrestoredConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectSession(mock)
	mock.ExpectBegin()
	copyFailure, restoreFailure := errors.New("copy failed"), errors.New("reset failed")
	mock.ExpectExec(`REPLACE INTO wire_evidence_projection`).WillReturnError(copyFailure)
	mock.ExpectRollback()
	mock.ExpectExec(`SET SESSION sql_mode=\?,lock_wait_timeout=\?,innodb_lock_wait_timeout=\?`).WithArgs(strictMode, 60, 50).WillReturnError(restoreFailure)
	if _, err := Backfill(context.Background(), db, []int{10}); !errors.Is(err, copyFailure) || !errors.Is(err, restoreFailure) {
		t.Fatalf("lost copy/session failure: %v", err)
	}
	if stats := db.Stats(); stats.OpenConnections != 0 || stats.Idle != 0 {
		t.Fatal("returned changed session settings to pool")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
