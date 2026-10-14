package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"cleanapp-common/eiiheader"
	"github.com/go-sql-driver/mysql"
)

func TestEIIHeaderProjectionMySQLFixture(t *testing.T) {
	dsn := os.Getenv("EII_HEADER_TEST_DSN")
	if dsn == "" {
		t.Skip("set EII_HEADER_TEST_DSN for dedicated local eii_header_evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "eii_header_evidence_test" {
		t.Fatal("fixture only permits dedicated local eii_header_evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	t.Run("fresh_bootstrap", func(t *testing.T) {
		defer db.Exec(`DROP TABLE IF EXISTS fetchers`)
		defer db.Exec(`DROP TABLE IF EXISTS external_ingest_index`)
		defer db.Exec(`DROP TABLE IF EXISTS external_ingest_header_projection`)
		if err := ensureFetcherTables(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		var name, before, after string
		if err := db.QueryRow(`SHOW CREATE TABLE external_ingest_index`).Scan(&name, &before); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := eiiheader.Migrate(context.Background(), db); err != nil {
				t.Fatalf("fresh bootstrap is incompatible with header migration: %v", err)
			}
			if err := ensureFetcherTables(context.Background(), db); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.QueryRow(`SHOW CREATE TABLE external_ingest_index`).Scan(&name, &after); err != nil || before != after {
			t.Fatal("header migration/bootstrap retry changed an existing EII table")
		}
		if _, err := db.Exec(`INSERT INTO external_ingest_index (source,external_id,seq,source_url,source_timestamp,created_at) VALUES ('bootstrap','source',1,'https://example.test/source',NULL,NULL)`); err != nil {
			t.Fatal(err)
		}
		assertEIIHeaderFallbackParity(t, db)
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE external_ingest_index (source VARCHAR(64) NOT NULL,external_id VARCHAR(255) NOT NULL,seq INT NOT NULL,source_timestamp DATETIME NULL,source_url VARCHAR(500) NULL,created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,updated_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,PRIMARY KEY(source,external_id),KEY idx_seq(seq)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	defer db.Exec(`DROP TABLE external_ingest_index`)
	defer db.Exec(`DROP TABLE external_ingest_header_projection`)
	seed, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var originalMode string
	if err := seed.QueryRowContext(context.Background(), `SELECT @@SESSION.sql_mode`).Scan(&originalMode); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(context.Background(), `SET SESSION sql_mode=''`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(context.Background(), `INSERT INTO external_ingest_index VALUES ('archive','legacy-zero',1,'0000-00-00','', '0000-00-00',NULL),('twitter','legacy-null',2,NULL,NULL,NULL,NULL),('github','sibling',2,'2026-01-01','https://github.com/org/repo/issues/1','2026-02-01',NULL),('archive','not-a-report',999,NULL,NULL,NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(context.Background(), `SET SESSION sql_mode=?`, originalMode); err != nil {
		t.Fatal(err)
	}
	seed.Close()
	var name, beforeDDL, afterDDL, beforeHistory, afterHistory string
	var beforeTableID, afterTableID uint64
	const tableIdentity = `SELECT TABLE_ID FROM INFORMATION_SCHEMA.INNODB_TABLES WHERE NAME=CONCAT(DATABASE(),'/external_ingest_index')`
	if err := db.QueryRow(tableIdentity).Scan(&beforeTableID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SHOW CREATE TABLE external_ingest_index`).Scan(&name, &beforeDDL); err != nil {
		t.Fatal(err)
	}
	history := `SELECT SHA2(GROUP_CONCAT(CONCAT_WS('|',source,external_id,seq,COALESCE(DATE_FORMAT(source_timestamp,'%Y-%m-%d %H:%i:%s'),'NULL'),COALESCE(source_url,'NULL'),COALESCE(DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'),'NULL'),COALESCE(DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s'),'NULL')) ORDER BY source,external_id),256) FROM external_ingest_index`
	if err := db.QueryRow(history).Scan(&beforeHistory); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := eiiheader.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var restoredMode string
	if err := db.QueryRow(`SELECT @@SESSION.sql_mode`).Scan(&restoredMode); err != nil || restoredMode != originalMode {
		t.Fatal("migration left changed SQL mode in the fixture connection pool")
	}
	if err := db.QueryRow(tableIdentity).Scan(&afterTableID); err != nil || beforeTableID != afterTableID {
		t.Fatal("migration rebuilt the original EII table")
	}
	if err := db.QueryRow(`SHOW CREATE TABLE external_ingest_index`).Scan(&name, &afterDDL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(history).Scan(&afterHistory); err != nil || beforeDDL != afterDDL || beforeHistory != afterHistory {
		t.Fatal("migration changed parent schema, indexes or source history")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_header_projection`).Scan(&count); err != nil || count != 0 {
		t.Fatal("migration implicitly backfilled history")
	}
	assertEqual := func(seq int) {
		t.Helper()
		var mismatch int
		if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_index e LEFT JOIN external_ingest_header_projection h ON h.seq=e.seq AND h.source=e.source AND h.external_id=e.external_id WHERE e.seq=? AND (h.seq IS NULL OR NOT(BINARY e.source <=> BINARY h.source) OR NOT(BINARY e.external_id <=> BINARY h.external_id) OR NOT(e.source_url <=> h.source_url) OR NOT(e.source_timestamp <=> h.source_timestamp) OR NOT(e.created_at <=> h.created_at))`, seq).Scan(&mismatch); err != nil || mismatch != 0 {
			t.Fatalf("header differs for seq%d: mismatch%d err=%v", seq, mismatch, err)
		}
	}
	if _, err := eiiheader.Backfill(context.Background(), db, []int{1}); err != nil {
		t.Fatal(err)
	}
	assertEqual(1)
	assertEIIHeaderFallbackParity(t, db)
	if _, err := eiiheader.Backfill(context.Background(), db, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	assertEqual(2)
	assertEIIHeaderFallbackParity(t, db)
	// Copying a legacy zero date must not fail a strict parent caller which
	// updates only a URL. New invalid parent dates remain rejected by that caller.
	strict, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.ExecContext(context.Background(), `SET SESSION sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,NO_ENGINE_SUBSTITUTION'`); err != nil {
		t.Fatal(err)
	}
	if _, err := strict.ExecContext(context.Background(), `UPDATE external_ingest_index SET source_url='https://example.test/current' WHERE seq=1`); err != nil {
		t.Fatalf("legacy zero copying introduced a strict-caller intake failure: %v", err)
	}
	if _, err := strict.ExecContext(context.Background(), `INSERT INTO external_ingest_index (source,external_id,seq,source_timestamp) VALUES ('archive','invalid-new-date',4,'0000-00-00')`); err == nil {
		t.Fatal("trigger relaxed the caller's parent date validation")
	}
	if _, err := strict.ExecContext(context.Background(), `SET SESSION sql_mode=?`, originalMode); err != nil {
		t.Fatal(err)
	}
	strict.Close()
	assertEqual(1)
	exec(`UPDATE external_ingest_index SET seq=3,source='ARCHIVE',external_id='moved',source_url=NULL,source_timestamp=NULL,created_at=NULL WHERE seq=1`)
	assertEqual(3)
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_header_projection WHERE seq=1`).Scan(&count); err != nil || count != 0 {
		t.Fatal("PK/seq move retained the old header")
	}
	exec(`INSERT INTO external_ingest_index (source,external_id,seq) VALUES ('twitter','future-orphan',-10)`)
	assertEqual(-10)
	exec(`DELETE FROM external_ingest_index WHERE seq=3`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_header_projection WHERE seq=3`).Scan(&count); err != nil || count != 0 {
		t.Fatal("delete retained a header")
	}
	exec(`UPDATE external_ingest_header_projection SET source_url='stale' WHERE seq=2`)
	if _, err := eiiheader.Backfill(context.Background(), db, []int{2}); err != nil {
		t.Fatal(err)
	}
	assertEqual(2)
	assertEIIHeaderFallbackParity(t, db)
	// A fixture-only gate holds INSERT...SELECT before its transaction commits.
	exec(`CREATE TRIGGER eii_header_fixture_delay AFTER INSERT ON external_ingest_header_projection FOR EACH ROW BEGIN DO GET_LOCK('cleanapp_eii_header_fixture_gate',5); DO SLEEP(0.6); DO RELEASE_LOCK('cleanapp_eii_header_fixture_gate'); END`)
	defer db.Exec(`DROP TRIGGER IF EXISTS eii_header_fixture_delay`)
	for _, operation := range []string{"delete", "update"} {
		seq := 10
		if operation == "update" {
			seq = 11
		}
		exec(`INSERT INTO external_ingest_index (source,external_id,seq,source_url) VALUES ('fixture',?,?,'before')`, operation, seq)
		exec(`DELETE FROM external_ingest_header_projection WHERE seq=?`, seq)
		copied := make(chan error, 1)
		go func() { _, err := eiiheader.Backfill(context.Background(), db, []int{seq}); copied <- err }()
		deadline := time.Now().Add(2 * time.Second)
		for {
			var held int
			if err := db.QueryRow(`SELECT IS_USED_LOCK('cleanapp_eii_header_fixture_gate') IS NOT NULL`).Scan(&held); err != nil {
				t.Fatal(err)
			}
			if held != 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("backfill did not reach the concurrency gate")
			}
			time.Sleep(10 * time.Millisecond)
		}
		changed := make(chan error, 1)
		go func() {
			query := `DELETE FROM external_ingest_index WHERE seq=?`
			if operation == "update" {
				query = `UPDATE external_ingest_index SET source_url='after',source_timestamp=NULL,created_at=NULL WHERE seq=?`
			}
			_, err := db.Exec(query, seq)
			changed <- err
		}()
		select {
		case err := <-changed:
			t.Fatalf("%s bypassed backfill source locks: %v", operation, err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := <-copied; err != nil {
			t.Fatal(err)
		}
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
		if operation == "update" {
			assertEqual(seq)
		} else if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_header_projection WHERE seq=?`, seq).Scan(&count); err != nil || count != 0 {
			t.Fatal("backfill resurrected a deleted header")
		}
	}
	exec(`DROP TRIGGER eii_header_fixture_delay`)
	exec(`DROP TRIGGER eii_header_after_insert`)
	exec(`CREATE TRIGGER eii_header_after_insert AFTER INSERT ON external_ingest_index FOR EACH ROW SET @foreign_header=1`)
	if err := eiiheader.Migrate(context.Background(), db); err == nil {
		t.Fatal("accepted an unrelated existing named trigger")
	}
}

func assertEIIHeaderFallbackParity(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	type identity struct {
		Seq    int    `json:"seq"`
		Source string `json:"source"`
		ID     string `json:"id"`
	}
	ids := []identity{}
	expected := []string{}
	rows, err := tx.Query(`SELECT seq,source,external_id,CAST(JSON_ARRAY(source_url,DATE_FORMAT(source_timestamp,'%Y-%m-%d %H:%i:%s'),DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')) AS CHAR) FROM external_ingest_index ORDER BY source,external_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id identity
		var header string
		if err := rows.Scan(&id.Seq, &id.Source, &id.ID, &header); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		expected = append(expected, header)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	payload, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = tx.Query(`SELECT CAST(JSON_ARRAY(IF(h.seq IS NULL,e.source_url,h.source_url),DATE_FORMAT(IF(h.seq IS NULL,e.source_timestamp,h.source_timestamp),'%Y-%m-%d %H:%i:%s'),DATE_FORMAT(IF(h.seq IS NULL,e.created_at,h.created_at),'%Y-%m-%d %H:%i:%s')) AS CHAR)
 FROM JSON_TABLE(?,'$[*]' COLUMNS(ord FOR ORDINALITY,seq INT PATH '$.seq',source VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.source',id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.id')) ids
 LEFT JOIN external_ingest_header_projection h ON h.seq=ids.seq AND h.source=ids.source AND h.external_id=ids.id
 LEFT JOIN external_ingest_index e FORCE INDEX(PRIMARY) ON e.source=IF(h.seq IS NULL,ids.source,NULL) AND e.external_id=ids.id AND e.seq=ids.seq ORDER BY ids.ord`, string(payload))
	if err != nil {
		t.Fatal(err)
	}
	actual := []string{}
	for rows.Next() {
		var header string
		if err := rows.Scan(&header); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, header)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("partial/full header projection changed source fields, NULLs or fallback")
	}
}
