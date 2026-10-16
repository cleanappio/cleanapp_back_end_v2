package database

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"cleanapp-common/eiiheader"
	"cleanapp-common/evidence"
	"cleanapp-common/evidencesourceprojection"
	"cleanapp-common/reportmeta"
	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

// This opt-in fixture owns an empty, explicitly local database. It never uses
// application credentials or makes a request to the production database.
func TestEvidenceSourceProjectionMySQLFixture(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_SOURCE_PROJECTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set a local EVIDENCE_SOURCE_PROJECTION_TEST_DSN for evidence_source_projection_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid dedicated source projection fixture DSN")
	}
	host, _, hostErr := net.SplitHostPort(cfg.Addr)
	if cfg.Net != "tcp" || hostErr != nil || (host != "127.0.0.1" && host != "localhost") || cfg.DBName != "evidence_source_projection_test" {
		t.Fatal("fixture requires loopback TCP and exact database evidence_source_projection_test")
	}
	cfg.ParseTime = true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("fixture database connector failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, e := db.ExecContext(c, q, args...); e != nil {
			t.Fatalf("fixture SQL: %v", e)
		}
	}
	var existing int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("fixture refuses a nonempty or unreadable database")
	}
	parents := []struct{ name, sql string }{
		{"fetchers", `CREATE TABLE fetchers(fetcher_id VARCHAR(64) PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"reports", `CREATE TABLE reports(seq INT PRIMARY KEY,id VARCHAR(255) NOT NULL DEFAULT '',public_id VARCHAR(32),ts TIMESTAMP NULL,latitude FLOAT NOT NULL DEFAULT 0,longitude FLOAT NOT NULL DEFAULT 0,image LONGBLOB) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"report_raw", `CREATE TABLE report_raw(report_seq INT PRIMARY KEY,source_type VARCHAR(32),source_id VARCHAR(255),fetcher_id VARCHAR(64),visibility VARCHAR(16) NOT NULL DEFAULT 'public',CONSTRAINT fixture_raw_report FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"external_ingest_index", `CREATE TABLE external_ingest_index(seq INT NOT NULL,source VARCHAR(64) NOT NULL,external_id VARCHAR(255) NOT NULL,source_url VARCHAR(500),source_timestamp DATETIME,created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,PRIMARY KEY(source,external_id),KEY idx_seq(seq)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"wire_submissions_raw", `CREATE TABLE wire_submissions_raw(submission_id VARCHAR(64) PRIMARY KEY,report_seq INT,fetcher_id VARCHAR(64) NOT NULL,report_json JSON,provenance_json JSON,extensions_json JSON,observed_at TIMESTAMP NULL,submitted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',channel VARCHAR(32) NOT NULL DEFAULT 'wire',KEY fixture_wire_report_seq(report_seq),CONSTRAINT fixture_wire_report FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE SET NULL,CONSTRAINT fixture_wire_fetcher FOREIGN KEY(fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"report_analysis", `CREATE TABLE report_analysis(seq INT,language VARCHAR(2),brand_name VARCHAR(255),brand_display_name VARCHAR(255),is_valid BOOL,classification VARCHAR(32),created_at DATETIME,title VARCHAR(500),summary TEXT,severity_level FLOAT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`},
		{"report_status", `CREATE TABLE report_status(seq INT,status VARCHAR(32)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"reports_owners", `CREATE TABLE reports_owners(seq INT,owner VARCHAR(64),is_public BOOL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"case_reports", `CREATE TABLE case_reports(seq INT,case_id VARCHAR(64)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, p := range parents {
		exec(p.sql)
		created = append(created, p.name)
	}
	// Install the real existing maintenance triggers as well as the new invalidators.
	for _, m := range []struct {
		name string
		up   func(context.Context, *sql.DB) error
	}{{"report_metadata_projection", reportmeta.Migrate}, {"external_ingest_header_projection", eiiheader.Migrate}, {"wire_evidence_projection", wiremeta.Migrate}, {"report_evidence", evidence.Migrate}} {
		// A failed migration may already have created its table. Register cleanup
		// before running it so the next dedicated-fixture attempt stays empty.
		created = append(created, m.name)
		if err := m.up(ctx, db); err != nil {
			t.Fatalf("fixture dependency %s: %v", m.name, err)
		}
	}
	schema := func(table string) string {
		t.Helper()
		var name, ddl string
		if err := db.QueryRowContext(ctx, "SHOW CREATE TABLE "+table).Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		return ddl
	}
	beforeSchema := map[string]string{}
	for _, p := range parents {
		beforeSchema[p.name] = schema(p.name)
	}
	exec(`SET SESSION lock_wait_timeout=37`)
	created = append(created, evidencesourceprojection.Table)
	for i := 0; i < 2; i++ {
		if err := evidencesourceprojection.Migrate(ctx, db); err != nil {
			t.Fatalf("source projection migration attempt %d: %v", i+1, err)
		}
	}
	var lockLimit int
	if err := db.QueryRowContext(ctx, `SELECT @@SESSION.lock_wait_timeout`).Scan(&lockLimit); err != nil || lockLimit != 37 {
		t.Fatal("migration did not restore its pooled session limit")
	}
	for _, p := range parents {
		if schema(p.name) != beforeSchema[p.name] {
			t.Fatalf("migration changed parent schema %s", p.name)
		}
	}
	var invalidators int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND LEFT(trigger_name,16)='evidence_source_'`).Scan(&invalidators); err != nil || invalidators != 22 {
		t.Fatalf("expected 21 source invalidators plus fetcher guard, got %d: %v", invalidators, err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(3)
	if err := ensureEvidenceBrandCoreIndex(ctx, db); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO fetchers VALUES ('fixture-fetcher')`)
	seed := func(seq int) {
		t.Helper()
		exec(`INSERT INTO reports(seq,id,public_id,ts,image) VALUES (?,?,?,'2026-10-10 12:00:00','')`, seq, fmt.Sprintf("fixture_%d", seq), fmt.Sprintf("r_%d", seq))
	}
	type generation struct {
		source, computed uint64
		version          int
		dirty, clean     bool
	}
	state := func(seq int) generation {
		t.Helper()
		var s generation
		q := `SELECT source_revision,computed_revision,normalizer_version,dirty,COALESCE(` + evidencesourceprojection.CleanPredicate("p") + `,FALSE) FROM ` + evidencesourceprojection.Table + ` p WHERE seq=?`
		if err := db.QueryRowContext(ctx, q, seq).Scan(&s.source, &s.computed, &s.version, &s.dirty, &s.clean); err != nil {
			t.Fatalf("generation %d: %v", seq, err)
		}
		return s
	}
	record := func(seq int) evidencesourceprojection.Record {
		return evidencesourceprojection.Record{Seq: seq, SourcePlatform: "fixture", SourceKey: fmt.Sprintf("report:%d", seq), IngestedAtNull: true}
	}
	warm := func(seq int) {
		t.Helper()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		revisions, err := evidencesourceprojection.EnsureRowsAndLock(ctx, tx, []int{seq})
		if err != nil {
			t.Fatal(err)
		}
		for replay := 0; replay < 2; replay++ {
			ok, err := evidencesourceprojection.Store(ctx, tx, revisions[seq], record(seq))
			if err != nil || !ok {
				t.Fatalf("store replay %d: success=%v error=%v", replay, ok, err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		s := state(seq)
		if s.dirty || !s.clean || s.source != s.computed {
			t.Fatalf("not clean after store: %+v", s)
		}
	}
	dirtyAfter := func(seq int, before generation) {
		t.Helper()
		after := state(seq)
		if after.source <= before.source || !after.dirty || after.clean {
			t.Fatalf("source %d did not invalidate: before=%+v after=%+v", seq, before, after)
		}
	}

	t.Run("all source and projection insert update delete triggers", func(t *testing.T) {
		seed(100)
		s := state(100)
		if !s.dirty || s.clean {
			t.Fatal("new report unexpectedly clean")
		}
		warm(100)
		s = state(100)
		exec(`UPDATE reports SET id='changed-original' WHERE seq=100`)
		dirtyAfter(100, s)
		warm(100)
		s = state(100)
		exec(`DELETE FROM reports WHERE seq=100`)
		dirtyAfter(100, s)
		tombstone := state(100)
		seed(100)
		dirtyAfter(100, tombstone)
		for _, tc := range []struct {
			name                            string
			seq                             int
			prepare, insert, update, remove string
		}{
			{"report_raw", 110, "", `INSERT INTO report_raw(report_seq,source_type,source_id,fetcher_id) VALUES(110,'github','raw-id','fixture')`, `UPDATE report_raw SET source_id='new-raw-id' WHERE report_seq=110`, `DELETE FROM report_raw WHERE report_seq=110`},
			{"external_ingest_index", 120, "", `INSERT INTO external_ingest_index(seq,source,external_id,source_url) VALUES(120,'fixture','index-120','https://example.test/old')`, `UPDATE external_ingest_index SET source_url='https://example.test/new' WHERE source='fixture' AND external_id='index-120'`, `DELETE FROM external_ingest_index WHERE source='fixture' AND external_id='index-120'`},
			{"wire_submissions_raw", 130, "", `INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id,report_json) VALUES('wire-130',130,'fixture-fetcher',JSON_OBJECT())`, `UPDATE wire_submissions_raw SET report_json=JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',TRUE)) WHERE submission_id='wire-130'`, `DELETE FROM wire_submissions_raw WHERE submission_id='wire-130'`},
			{"report_metadata_projection", 140, `DELETE FROM report_metadata_projection WHERE seq=140`, `INSERT INTO report_metadata_projection(seq,id,public_id,ts,latitude,longitude,has_original_media) VALUES(140,'projected','r_140',NULL,0,0,FALSE)`, `UPDATE report_metadata_projection SET id='changed-projected' WHERE seq=140`, `DELETE FROM report_metadata_projection WHERE seq=140`},
			{"external_ingest_header_projection", 150, "", `INSERT INTO external_ingest_header_projection(seq,source,external_id,source_url) VALUES(150,'fixture','header-150','https://example.test/old')`, `UPDATE external_ingest_header_projection SET source_url=NULL WHERE seq=150`, `DELETE FROM external_ingest_header_projection WHERE seq=150`},
			{"wire_evidence_projection", 160, `INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id) VALUES('wire-160',160,'fixture-fetcher')`, `INSERT INTO wire_evidence_projection(submission_id,report_seq,observed_at,submitted_at,actor_kind,channel) VALUES('wire-160',160,NULL,'2026-10-10 12:00:00','machine','wire')`, `UPDATE wire_evidence_projection SET contains_pii=CAST('true' AS JSON) WHERE submission_id='wire-160'`, `DELETE FROM wire_evidence_projection WHERE submission_id='wire-160'`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				seed(tc.seq)
				if tc.prepare != "" {
					exec(tc.prepare)
				}
				if tc.name == "wire_evidence_projection" {
					exec(`DELETE FROM wire_evidence_projection WHERE submission_id='wire-160'`)
				}
				for _, q := range []string{tc.insert, tc.update, tc.remove} {
					warm(tc.seq)
					before := state(tc.seq)
					exec(q)
					dirtyAfter(tc.seq, before)
				}
			})
		}
	})
	t.Run("moves invalidate old and new sequences", func(t *testing.T) {
		for _, tc := range []struct {
			name          string
			old, next     int
			prepare, move string
		}{
			{"reports", 201, 202, "", `UPDATE reports SET seq=202 WHERE seq=201`},
			{"report_raw", 211, 212, `INSERT INTO report_raw(report_seq) VALUES(211)`, `UPDATE report_raw SET report_seq=212 WHERE report_seq=211`},
			{"external_ingest_index", 221, 222, `INSERT INTO external_ingest_index(seq,source,external_id) VALUES(221,'fixture','move-221')`, `UPDATE external_ingest_index SET seq=222 WHERE external_id='move-221'`},
			{"wire_submissions_raw", 231, 232, `INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id) VALUES('move-231',231,'fixture-fetcher')`, `UPDATE wire_submissions_raw SET report_seq=232 WHERE submission_id='move-231'`},
			{"report_metadata_projection", 241, 242, `DELETE FROM report_metadata_projection WHERE seq=242`, `UPDATE report_metadata_projection SET seq=242 WHERE seq=241`},
			{"external_ingest_header_projection", 251, 252, `INSERT INTO external_ingest_header_projection(seq,source,external_id) VALUES(251,'fixture','move-251')`, `UPDATE external_ingest_header_projection SET seq=252 WHERE external_id='move-251'`},
			{"wire_evidence_projection", 261, 262, `INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id) VALUES('move-261',261,'fixture-fetcher')`, `UPDATE wire_evidence_projection SET report_seq=262 WHERE submission_id='move-261'`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				seed(tc.old)
				if tc.name != "reports" {
					seed(tc.next)
				}
				if tc.prepare != "" {
					exec(tc.prepare)
				}
				warm(tc.old)
				warm(tc.next)
				a, b := state(tc.old), state(tc.next)
				exec(tc.move)
				dirtyAfter(tc.old, a)
				dirtyAfter(tc.next, b)
			})
		}
	})
	t.Run("fetcher cascade dirties surviving reports", func(t *testing.T) {
		exec(`INSERT INTO fetchers VALUES('delete-fetcher')`)
		for _, seq := range []int{301, 302} {
			seed(seq)
			exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id) VALUES(?,?,'delete-fetcher')`, fmt.Sprintf("cascade-%d", seq), seq)
			warm(seq)
		}
		a, b := state(301), state(302)
		exec(`DELETE FROM fetchers WHERE fetcher_id='delete-fetcher'`)
		dirtyAfter(301, a)
		dirtyAfter(302, b)
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wire_submissions_raw WHERE submission_id IN('cascade-301','cascade-302')`).Scan(&n); err != nil || n != 0 {
			t.Fatal("fetcher FK cascade did not run")
		}
	})
	t.Run("source and invalidation rollback atomically", func(t *testing.T) {
		seed(401)
		exec(`INSERT INTO report_raw(report_seq,source_id) VALUES(401,'before')`)
		warm(401)
		before := state(401)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `UPDATE report_raw SET source_id='rolled-back' WHERE report_seq=401`); err != nil {
			t.Fatal(err)
		}
		var inside uint64
		if err := tx.QueryRowContext(ctx, `SELECT source_revision FROM report_evidence_source_projection WHERE seq=401`).Scan(&inside); err != nil || inside <= before.source {
			t.Fatal("writer did not see transactional invalidation")
		}
		if got := state(401); got != before {
			t.Fatal("uncommitted generation escaped writer transaction")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if got := state(401); got != before {
			t.Fatal("rolled-back invalidation became visible")
		}
		var id string
		if err := db.QueryRowContext(ctx, `SELECT source_id FROM report_raw WHERE report_seq=401`).Scan(&id); err != nil || id != "before" {
			t.Fatal("source mutation did not roll back")
		}
	})
	t.Run("generation lock serializes source mutation and guarded replay", func(t *testing.T) {
		seed(402)
		exec(`INSERT INTO report_raw(report_seq,source_id) VALUES(402,'before')`)
		warm(402)
		a, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Rollback()
		revs, err := evidencesourceprojection.EnsureRowsAndLock(ctx, a, []int{402})
		if err != nil {
			t.Fatal(err)
		}
		var id string
		if err := a.QueryRowContext(ctx, `SELECT source_id FROM report_raw WHERE report_seq=402`).Scan(&id); err != nil || id != "before" {
			t.Fatal("refresh snapshot was not established after generation lock")
		}
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		var oldWait int
		if err := c.QueryRowContext(ctx, `SELECT @@SESSION.innodb_lock_wait_timeout`).Scan(&oldWait); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = c.ExecContext(context.Background(), `SET SESSION innodb_lock_wait_timeout=?`, oldWait) }()
		if _, err := c.ExecContext(ctx, `SET SESSION innodb_lock_wait_timeout=1`); err != nil {
			t.Fatal(err)
		}
		b, err := c.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Rollback()
		_, err = b.ExecContext(ctx, `UPDATE report_raw SET source_id='after' WHERE report_seq=402`)
		var mysqlErr *mysql.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1205 {
			t.Fatalf("competing source write should wait on generation lock, got %v", err)
		}
		if err := b.Rollback(); err != nil {
			t.Fatal(err)
		}
		if ok, err := evidencesourceprojection.Store(ctx, a, revs[402], record(402)); err != nil || !ok {
			t.Fatalf("locked writer publication failed: %v", err)
		}
		if err := a.Commit(); err != nil {
			t.Fatal(err)
		}
		before := state(402)
		if _, err := c.ExecContext(ctx, `UPDATE report_raw SET source_id='after' WHERE report_seq=402`); err != nil {
			t.Fatal(err)
		}
		dirtyAfter(402, before)
		stale, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer stale.Rollback()
		if ok, err := evidencesourceprojection.Store(ctx, stale, revs[402], record(402)); err != nil || ok {
			t.Fatalf("obsolete revision was published: success=%v error=%v", ok, err)
		}
		if err := stale.Rollback(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("nullable dates completeness and algorithm version", func(t *testing.T) {
		seed(403)
		warm(403)
		exec(`UPDATE report_evidence_source_projection SET normalizer_version=999 WHERE seq=403`)
		if state(403).clean {
			t.Fatal("unknown algorithm version accepted")
		}
		warm(403)
		exec(`UPDATE report_evidence_source_projection SET source_key=NULL WHERE seq=403`)
		if state(403).clean {
			t.Fatal("incomplete scalar output accepted")
		}
		warm(403)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		revs, err := evidencesourceprojection.EnsureRowsAndLock(ctx, tx, []int{403})
		if err != nil {
			t.Fatal(err)
		}
		r := record(403)
		zero, fraction := "0000-00-00 00:00:00.000000", "2026-10-10 12:00:00.123456"
		r.SourceTimestamp, r.ObservedAt = &zero, &fraction
		r.SourceKey = "é/ключ/源  "
		if ok, err := evidencesourceprojection.Store(ctx, tx, revs[403], r); err != nil || !ok {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var z, f, key string
		if err := db.QueryRowContext(ctx, `SELECT source_timestamp,observed_at,source_key FROM report_evidence_source_projection WHERE seq=403`).Scan(&z, &f, &key); err != nil || z != zero || f != fraction || key != r.SourceKey || !state(403).clean {
			t.Fatal("nullable/zero/fractional date or source bytes lost")
		}
	})
	readCached := func(seq int) evidencesourceprojection.Record {
		t.Helper()
		var r evidencesourceprojection.Record
		var published, observed, ingested sql.NullString
		q := `SELECT seq,` + strings.Join(evidencesourceprojection.Columns(), ",") + ` FROM report_evidence_source_projection WHERE seq=?`
		if err := db.QueryRowContext(ctx, q, seq).Scan(&r.Seq, &r.SourcePlatform, &r.SourceURL, &published, &observed, &ingested, &r.IngestionTimeMissing, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.IngestedAtNull); err != nil {
			t.Fatal(err)
		}
		if published.Valid {
			r.SourceTimestamp = &published.String
		}
		if observed.Valid {
			r.ObservedAt = &observed.String
		}
		if ingested.Valid {
			r.TS = &ingested.String
		}
		return r
	}
	t.Run("actual bounded refresh matches all canonical source outputs", func(t *testing.T) {
		ids := []int{511, 512, 513, 514, 517}
		for _, seq := range ids {
			seed(seq)
			exec(`INSERT INTO report_analysis VALUES(?,'en','projectionrefresh','Projection',TRUE,'digital','2026-10-10','Refresh fixture','Own observation',0.8)`, seq)
		}
		exec(`INSERT INTO report_raw(report_seq,source_type,source_id,fetcher_id) VALUES(511,'github','github:raw-511','raw-fetcher')`)
		// Native SQL collation selects accented source before z; the Wire tuple
		// still contributes author/privacy metadata to that chosen EII identity.
		exec(`INSERT INTO external_ingest_index(seq,source,external_id,source_url,source_timestamp,created_at) VALUES(512,'z','z-512','https://example.test/loser','2026-10-01','2026-10-10'),(512,'é','é-512','https://Example.test/selected?keep=1#fragment','2026-10-02','2026-10-11')`)
		exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id,report_json,provenance_json,observed_at,submitted_at,actor_kind,channel) VALUES('refresh-512',512,'fixture-fetcher',JSON_OBJECT('digital_context',JSON_OBJECT('metadata',JSON_OBJECT('author_username',9007199254740993)),'privacy',JSON_OBJECT('contains_pii',TRUE)),JSON_OBJECT('generation_method','human_authored_public'),'2026-10-05','2026-10-10','human','web')`)
		exec(`INSERT INTO wire_submissions_raw(submission_id,report_seq,fetcher_id,report_json,provenance_json,observed_at,submitted_at,actor_kind,channel) VALUES('refresh-513-old',513,'fixture-fetcher',JSON_OBJECT('digital_context',JSON_OBJECT('platform','github','source_url','https://example.test/old')),JSON_OBJECT('generation_method','human_authored_public'),'2026-10-02','2026-10-03','human','web'),('refresh-513-new',513,'fixture-fetcher',JSON_OBJECT('digital_context',JSON_OBJECT('platform','github','source_url','https://example.test/new','metadata',JSON_OBJECT('author_username','MixedCase'))),JSON_OBJECT('generation_method','human_authored_public'),'2026-10-04','2026-10-05','human','web')`)
		exec(`UPDATE reports SET ts=NULL WHERE seq=514`)
		exec(`UPDATE reports SET id='reddit_t1_refreshfixture',ts='2025-09-05' WHERE seq=517`)
		hash := sha1.Sum([]byte("reddit:t1_refreshfixture"))
		exec(`INSERT INTO external_ingest_index(seq,source,external_id,created_at) VALUES(517,'reddit_archive',?,'2026-10-15')`, hex.EncodeToString(hash[:])[:32])

		// The untouched SQL normalization CTE is independent of the cache writer
		// and covers every output, rather than comparing two copies of its mapper.
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		cte, args := evidenceCTE(EvidenceFilter{Kind: "all"}, "ra.seq IN (511,512,513,514,517)", []interface{}{"en"})
		rows, err := tx.QueryContext(ctx, cte+`SELECT seq,source_platform,source_url,DATE_FORMAT(source_timestamp,'%Y-%m-%d %H:%i:%s.%f'),DATE_FORMAT(observed_at,'%Y-%m-%d %H:%i:%s.%f'),DATE_FORMAT(ts,'%Y-%m-%d %H:%i:%s.%f'),ingestion_time_missing,actor_kind,fetcher_id,privacy_restricted,source_key,external_source,intake_channel,generation_method,COALESCE(source_account_key,''),ts IS NULL FROM normalized`, args...)
		if err != nil {
			t.Fatal(err)
		}
		expected := map[int]evidencesourceprojection.Record{}
		for rows.Next() {
			var r evidencesourceprojection.Record
			var published, observed, ingested sql.NullString
			if err := rows.Scan(&r.Seq, &r.SourcePlatform, &r.SourceURL, &published, &observed, &ingested, &r.IngestionTimeMissing, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.IngestedAtNull); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if published.Valid {
				r.SourceTimestamp = &published.String
			}
			if observed.Valid {
				r.ObservedAt = &observed.String
			}
			if ingested.Valid {
				r.TS = &ingested.String
			}
			expected[r.Seq] = r
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(expected) != len(ids) {
			t.Fatalf("canonical source oracle incomplete: count=%d error=%v", len(expected), err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		for replay := 0; replay < 2; replay++ {
			result, err := RefreshEvidenceSourceProjection(ctx, db, []int{599, 514, 511, 513, 512, 517})
			if err != nil || result.Requested != 6 || result.Computed != 5 || result.MissingParents != 1 {
				t.Fatalf("actual refresh replay%d: %+v %v", replay, result, err)
			}
			for _, seq := range ids {
				if got := readCached(seq); !reflect.DeepEqual(got, expected[seq]) || !state(seq).clean {
					t.Fatalf("actual cache differs from canonical source normalization for seq%d: got=%+v want=%+v", seq, got, expected[seq])
				}
			}
			if state(599).clean || !state(599).dirty {
				t.Fatal("missing parent stamped clean or lost generation tombstone")
			}
		}
		if readCached(512).SourcePlatform != "é" || readCached(513).SourceURL != "https://example.test/new" || !readCached(514).IngestedAtNull || !readCached(512).PrivacyRestricted {
			t.Fatal("fixture did not exercise native winner, latest Wire, NULL ingestion and privacy")
		}
		before := state(512)
		exec(`UPDATE external_ingest_index SET source_url='https://example.test/changed' WHERE source='é' AND external_id='é-512'`)
		dirtyAfter(512, before)
		if got := readCached(512); !reflect.DeepEqual(got, expected[512]) {
			t.Fatal("invalidation partially rewrote the old cached result")
		}
		if _, err := RefreshEvidenceSourceProjection(ctx, db, []int{512}); err != nil || readCached(512).SourceURL != "https://example.test/changed" || !state(512).clean {
			t.Fatalf("dirty source did not refresh: %v", err)
		}
	})
	t.Run("actual refresh establishes snapshot only after generation lock", func(t *testing.T) {
		seed(515)
		exec(`INSERT INTO report_raw(report_seq,source_type,source_id) VALUES(515,'github','before')`)
		if _, err := RefreshEvidenceSourceProjection(ctx, db, []int{515}); err != nil {
			t.Fatal(err)
		}
		holder, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Rollback()
		if _, err := evidencesourceprojection.EnsureRowsAndLock(ctx, holder, []int{515}); err != nil {
			t.Fatal(err)
		}
		type outcome struct {
			result EvidenceSourceProjectionRefresh
			err    error
		}
		done := make(chan outcome, 1)
		go func() { r, e := RefreshEvidenceSourceProjection(ctx, db, []int{515}); done <- outcome{r, e} }()
		// Observe an actual blocked cache-row lock instead of relying on sleep
		// ordering. If a consistent read moved before the lock, it is now stale.
		waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
		defer waitCancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting int
			err := db.QueryRowContext(waitCtx, `SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE=w.ENGINE AND l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME='report_evidence_source_projection'`).Scan(&waiting)
			if err != nil {
				t.Fatalf("fixture lock observation failed: %v", err)
			}
			if waiting > 0 {
				break
			}
			select {
			case r := <-done:
				t.Fatalf("actual refresh did not wait for held generation: %+v %v", r.result, r.err)
			case <-waitCtx.Done():
				t.Fatal("actual refresh never reached generation lock")
			case <-ticker.C:
			}
		}
		if _, err := holder.ExecContext(ctx, `UPDATE report_raw SET source_id='after' WHERE report_seq=515`); err != nil {
			t.Fatal(err)
		}
		if err := holder.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case r := <-done:
			if r.err != nil || r.result.Computed != 1 {
				t.Fatalf("actual refresh after lock release: %+v %v", r.result, r.err)
			}
		case <-ctx.Done():
			t.Fatal("actual refresh did not finish after generation release")
		}
		if got := readCached(515); got.SourceKey != "source:github:after" || !state(515).clean {
			t.Fatalf("refresh stored pre-lock snapshot as current: %+v", got)
		}
	})
	t.Run("raw latest Wire winner precedes authoritative matching header", func(t *testing.T) {
		// submitted_at in the matching header is NOT NULL in production; test a
		// stale date and independently test an authoritative nullable observed_at.
		exec(`UPDATE wire_evidence_projection SET submitted_at='2026-09-01',observed_at=NULL,digital_source_url=JSON_QUOTE('https://example.test/selected-header') WHERE submission_id='refresh-513-new'`)
		if _, err := RefreshEvidenceSourceProjection(ctx, db, []int{513}); err != nil {
			t.Fatal(err)
		}
		got := readCached(513)
		if got.SourceURL != "https://example.test/selected-header" || got.SourceTimestamp != nil || got.ObservedAt != nil || !state(513).clean {
			t.Fatalf("stale/NULL header changed raw winner or lost authoritative fields: %+v", got)
		}
	})

}
