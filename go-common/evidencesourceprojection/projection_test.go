package evidencesourceprojection

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestContractHasCompleteNullableDatesAndNoParentConstraints(t *testing.T) {
	ddl := createTableSQL()
	if strings.Contains(ddl, "FOREIGN KEY") || strings.Contains(ddl, "DATETIME") || strings.Contains(ddl, "TIMESTAMP") || strings.Count(ddl, "LONGTEXT") != 8 || strings.Count(ddl, "VARCHAR(26)") != 3 || !strings.Contains(ddl, "KEY idx_dirty_seq(dirty,seq)") {
		t.Fatal("projection narrowed outputs or introduced parent/date constraints")
	}
	if len(Columns()) != 15 {
		t.Fatal("wrong output count")
	}
	c := Columns()
	c[0] = "tampered"
	if Columns()[0] != "source_platform" {
		t.Fatal("mutable shared column contract")
	}
	predicate := CleanPredicate("p")
	for _, term := range []string{"p.dirty=0", "p.normalizer_version=1", "p.computed_revision=p.source_revision", "p.source_account_key IS NOT NULL", "p.privacy_restricted IS NOT NULL", "p.ingested_at_null=1 AND p.ts IS NULL", "p.ingested_at_null=0 AND p.ts IS NOT NULL"} {
		if !strings.Contains(predicate, term) {
			t.Fatalf("missing complete output guard %s", term)
		}
	}
	if strings.Contains(predicate, "source_timestamp IS NOT NULL") || strings.Contains(predicate, "observed_at IS NOT NULL") {
		t.Fatal("replaced authoritative null dates")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unsafe alias accepted")
		}
	}()
	CleanPredicate("p; SELECT 1")
}
func TestInvalidationCoversParentsProjectionsMovesAndFetcherCascade(t *testing.T) {
	ts := triggers()
	if len(ts) != 22 {
		t.Fatalf("expected seven IUD sets plus cascade, got %d", len(ts))
	}
	for _, tr := range ts {
		if !strings.Contains(tr.body, "source_revision=source_revision+1,dirty=TRUE") || strings.Contains(tr.body, "DELETE FROM report_evidence_source_projection") {
			t.Fatal("invalidation loses generation or dirty state")
		}
		if tr.event == "UPDATE" && (!strings.Contains(tr.body, "SELECT OLD.") || !strings.Contains(tr.body, "SELECT NEW.") || !strings.Contains(tr.body, " UNION ") || !strings.Contains(tr.body, "ORDER BY seq")) {
			t.Fatal("moved identity failed to invalidate both sorted generations")
		}
	}
	last := ts[len(ts)-1]
	if last.table != "fetchers" || last.timing != "BEFORE" || last.event != "DELETE" || !strings.Contains(last.body, "FROM wire_submissions_raw WHERE fetcher_id=OLD.fetcher_id") {
		t.Fatal("cascade invalidation is too late")
	}
}
func TestEnsureRowsAndLockRejectsInvalidIDsAndLocksInAscendingOrder(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ids := range [][]int{{1, 1}, {2, 1}, {0}, {-1}, make([]int, 201)} {
		if _, err := EnsureRowsAndLock(context.Background(), nil, ids); err == nil {
			t.Fatal("accepted invalid cohort")
		}
	}
	if got, err := EnsureRowsAndLock(context.Background(), nil, nil); err != nil || len(got) != 0 {
		t.Fatal("empty cohort accessed DB")
	}
	m.ExpectBegin()
	tx, _ := db.Begin()
	for _, id := range []int{10, 11} {
		m.ExpectExec(`INSERT INTO report_evidence_source_projection .* ON DUPLICATE KEY UPDATE seq=VALUES\(seq\)`).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 0))
		m.ExpectQuery(`SELECT source_revision .* WHERE seq=\? FOR UPDATE`).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(7))
	}
	got, err := EnsureRowsAndLock(context.Background(), tx, []int{10, 11})
	if err != nil || got[10] != 7 || got[11] != 7 {
		t.Fatalf("lock failed: %v %v", got, err)
	}
	m.ExpectRollback()
	tx.Rollback()
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func fixtureRecord() Record {
	zero := "0000-00-00 00:00:00.000000"
	micro := "2026-10-16 12:34:56.123456"
	return Record{Seq: 4, SourcePlatform: "native", SourceURL: strings.Repeat("u", 501), SourceTimestamp: &zero, ObservedAt: &micro, TS: &zero, ActorKind: "human", SourceKey: strings.Repeat("k", 800), SourceAccountKey: "", IngestedAtNull: false}
}
func TestStoreGuardsWholeResultAndSupportsIdempotentReplay(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed int64
		exists  bool
		want    bool
	}{{"new", 1, true, true}, {"same", 0, true, true}, {"stale", 0, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			m.ExpectBegin()
			tx, _ := db.Begin()
			r := fixtureRecord()
			m.ExpectExec(`UPDATE report_evidence_source_projection SET source_platform=\?,source_url=\?.*computed_revision=\?,normalizer_version=\?,dirty=0 WHERE seq=\? AND source_revision=\?`).WithArgs(r.SourcePlatform, r.SourceURL, r.SourceTimestamp, r.ObservedAt, r.TS, r.IngestionTimeMissing, r.ActorKind, r.FetcherID, r.PrivacyRestricted, r.SourceKey, r.ExternalSource, r.IntakeChannel, r.GenerationMethod, r.SourceAccountKey, r.IngestedAtNull, uint64(9), 1, 4, uint64(9)).WillReturnResult(sqlmock.NewResult(0, tc.changed))
			if tc.changed == 0 {
				rows := sqlmock.NewRows([]string{"revision"})
				if tc.exists {
					rows.AddRow(9)
				}
				m.ExpectQuery(regexp.QuoteMeta("SELECT p.source_revision FROM "+Table+" p WHERE p.seq=? AND p.source_revision=? AND "+CleanPredicate("p")+" FOR UPDATE")).WithArgs(4, uint64(9)).WillReturnRows(rows)
			}
			ok, err := Store(context.Background(), tx, 9, r)
			if err != nil || ok != tc.want {
				t.Fatalf("Store=%v,%v", ok, err)
			}
			m.ExpectRollback()
			tx.Rollback()
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestStoreRejectsInconsistentNullDatesAndInvalidLiteralsBeforeSQL(t *testing.T) {
	r := fixtureRecord()
	r.IngestedAtNull = true
	if _, err := Store(context.Background(), nil, 1, r); err == nil {
		t.Fatal("accepted nonnull TS with SQL-null flag")
	}
	r = fixtureRecord()
	r.TS = nil
	if _, err := Store(context.Background(), nil, 1, r); err == nil {
		t.Fatal("accepted missing TS with nonnull flag")
	}
	r = fixtureRecord()
	bad := "date ' injected"
	r.SourceTimestamp = &bad
	if _, err := Store(context.Background(), nil, 1, r); err == nil {
		t.Fatal("accepted malformed date")
	}
	r = fixtureRecord()
	r.Seq = 0
	if _, err := Store(context.Background(), nil, 1, r); err == nil {
		t.Fatal("accepted missing identity")
	}
	if _, err := Store(context.Background(), nil, 0, fixtureRecord()); err == nil {
		t.Fatal("accepted nonexistent generation")
	}
}
func TestMigrationRestoresSessionAfterUnsupportedCascade(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"wait"}).AddRow(60))
	m.ExpectExec(`SET SESSION lock_wait_timeout=\?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery(`SELECT k.TABLE_NAME.*REFERENTIAL_CONSTRAINTS`).WillReturnRows(sqlmock.NewRows([]string{"table", "col", "parent", "pcol", "delete", "update"}).AddRow("report_raw", "fetcher_id", "fetchers", "fetcher_id", "SET NULL", "RESTRICT"))
	m.ExpectExec(`SET SESSION lock_wait_timeout=\?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
	if err = Migrate(context.Background(), db); err == nil {
		t.Fatal("accepted uncovered cascade")
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestStorePropagatesSQLError(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m.ExpectBegin()
	tx, _ := db.Begin()
	failure := errors.New("deadline")
	m.ExpectExec(`UPDATE report_evidence_source_projection`).WillReturnError(failure)
	if _, err = Store(context.Background(), tx, 1, fixtureRecord()); !errors.Is(err, failure) {
		t.Fatal("lost storage error")
	}
	m.ExpectRollback()
	tx.Rollback()
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectContract(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT k.TABLE_NAME.*REFERENTIAL_CONSTRAINTS`).WillReturnRows(sqlmock.NewRows([]string{"table", "col", "parent", "pcol", "delete", "update"}).AddRow("report_raw", "report_seq", "reports", "seq", "CASCADE", "RESTRICT").AddRow("wire_submissions_raw", "report_seq", "reports", "seq", "SET NULL", "NO ACTION").AddRow("wire_submissions_raw", "fetcher_id", "fetchers", "fetcher_id", "CASCADE", "RESTRICT").AddRow("wire_evidence_projection", "submission_id", "wire_submissions_raw", "submission_id", "CASCADE", "CASCADE"))
}
func expectTable(mock sqlmock.Sqlmock) {
	rows := sqlmock.NewRows([]string{"name", "type", "null", "collation", "default", "extra"}).AddRow("seq", "int", "NO", "", nil, "").AddRow("source_revision", "bigint unsigned", "NO", "", "1", "").AddRow("computed_revision", "bigint unsigned", "NO", "", "0", "").AddRow("normalizer_version", "int", "NO", "", "0", "").AddRow("dirty", "tinyint(1)", "NO", "", "1", "")
	for _, name := range outputColumns {
		typ, coll := "longtext", "utf8mb4_bin"
		for _, d := range dateColumns {
			if d == name {
				typ, coll = "varchar(26)", "ascii_bin"
			}
		}
		for _, b := range boolColumns {
			if b == name {
				typ, coll = "tinyint(1)", ""
			}
		}
		rows.AddRow(name, typ, "YES", coll, nil, "")
	}
	mock.ExpectQuery(`SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE`).WithArgs(Table).WillReturnRows(rows)
	mock.ExpectQuery(`SELECT ENGINE FROM`).WithArgs(Table).WillReturnRows(sqlmock.NewRows([]string{"engine"}).AddRow("InnoDB"))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE`).WithArgs(Table, Table).WillReturnRows(sqlmock.NewRows([]string{"fks"}).AddRow(0))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE\(\) AND TABLE_NAME=\?$`).WithArgs(Table).WillReturnRows(sqlmock.NewRows([]string{"indexes"}).AddRow(3))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS .*INDEX_NAME='PRIMARY'`).WithArgs(Table).WillReturnRows(sqlmock.NewRows([]string{"primary"}).AddRow(1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM INFORMATION_SCHEMA.STATISTICS .*INDEX_NAME='idx_dirty_seq'`).WithArgs(Table).WillReturnRows(sqlmock.NewRows([]string{"dirty"}).AddRow(2))
}
func expectTrigger(mock sqlmock.Sqlmock, tr trigger, exists bool, body string) {
	rows := sqlmock.NewRows([]string{"table", "timing", "event", "body"})
	if exists {
		rows.AddRow(tr.table, tr.timing, tr.event, body)
	}
	mock.ExpectQuery(`SELECT EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT`).WithArgs(tr.name).WillReturnRows(rows)
}
func TestMigrationIsAdditiveAndIdempotentVerifyIsReadOnly(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, exists := range []bool{false, true} {
		m.ExpectQuery(`SELECT @@SESSION.lock_wait_timeout`).WillReturnRows(sqlmock.NewRows([]string{"wait"}).AddRow(60))
		m.ExpectExec(`SET SESSION lock_wait_timeout=\?`).WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 0))
		expectContract(m)
		m.ExpectExec(`CREATE TABLE IF NOT EXISTS report_evidence_source_projection`).WillReturnResult(sqlmock.NewResult(0, 0))
		expectTable(m)
		for _, tr := range triggers() {
			expectTrigger(m, tr, exists, tr.body)
			if !exists {
				m.ExpectExec("CREATE TRIGGER " + tr.name).WillReturnResult(sqlmock.NewResult(0, 0))
			}
		}
		m.ExpectExec(`SET SESSION lock_wait_timeout=\?`).WithArgs(60).WillReturnResult(sqlmock.NewResult(0, 0))
		if err = Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	expectContract(m)
	expectTable(m)
	for _, tr := range triggers() {
		expectTrigger(m, tr, true, tr.body)
	}
	if err = Verify(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestReadOnlyVerifyRejectsMissingOrForeignNamedTrigger(t *testing.T) {
	for _, exists := range []bool{false, true} {
		db, m, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		expectContract(m)
		expectTable(m)
		expectTrigger(m, triggers()[0], exists, "SET @unrelated=1")
		if err = Verify(context.Background(), db); err == nil {
			t.Fatal("accepted missing/foreign named trigger")
		}
		if err = m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
