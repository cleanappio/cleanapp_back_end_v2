package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"cleanapp-common/wiremeta"
	"github.com/go-sql-driver/mysql"
)

// Parent definition is the audited production Wire schema, including its
// unrelated indexes and the fetcher CASCADE/report SET NULL contracts.
const wireMetaFixtureParent = `CREATE TABLE wire_submissions_raw (
 submission_id VARCHAR(64) NOT NULL,
 receipt_id VARCHAR(64) NOT NULL,
 fetcher_id VARCHAR(64) NOT NULL,
 key_id CHAR(36) DEFAULT NULL,
 actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',
 channel VARCHAR(32) NOT NULL DEFAULT 'wire',
 auth_method VARCHAR(32) NOT NULL DEFAULT 'api_key',
 source_id VARCHAR(255) NOT NULL,
 schema_version VARCHAR(32) NOT NULL,
 submitted_at TIMESTAMP NOT NULL,
 observed_at TIMESTAMP NULL DEFAULT NULL,
 agent_id VARCHAR(128) NOT NULL,
 lane VARCHAR(16) NOT NULL,
 material_hash CHAR(64) NOT NULL,
 submission_quality FLOAT NOT NULL DEFAULT '0',
 risk_score FLOAT NOT NULL DEFAULT '0',
 report_seq INT DEFAULT NULL,
 agent_json JSON DEFAULT NULL,
 provenance_json JSON DEFAULT NULL,
 report_json JSON DEFAULT NULL,
 dedupe_json JSON DEFAULT NULL,
 delivery_json JSON DEFAULT NULL,
 extensions_json JSON DEFAULT NULL,
 created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
 PRIMARY KEY(submission_id),
 UNIQUE KEY uniq_wire_receipt_id(receipt_id),
 UNIQUE KEY uniq_wire_fetcher_source(fetcher_id,source_id),
 KEY idx_wire_fetcher_created(fetcher_id,created_at),
 KEY idx_wire_lane_created(lane,created_at),
 KEY fk_wire_submission_report(report_seq),
 KEY idx_wire_actor_channel_created(actor_kind,channel,created_at),
 CONSTRAINT fk_wire_submission_fetcher FOREIGN KEY(fetcher_id) REFERENCES fetchers(fetcher_id) ON DELETE CASCADE,
 CONSTRAINT fk_wire_submission_report FOREIGN KEY(report_seq) REFERENCES reports(seq) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func TestWireMetaProjectionMySQLFixture(t *testing.T) {
	dsn := os.Getenv("WIRE_META_TEST_DSN")
	if dsn == "" {
		t.Skip("set WIRE_META_TEST_DSN for dedicated local wire_meta_evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "wire_meta_evidence_test" {
		t.Fatal("fixture only permits dedicated local wire_meta_evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(6)
	var version string
	if err := db.QueryRow(`SELECT VERSION()`).Scan(&version); err != nil || !(strings.HasPrefix(version, "8.0.44") || strings.HasPrefix(version, "8.4.")) {
		t.Fatal("Wire projection fixture requires actual MySQL8.0.44 or 8.4")
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, table := range []struct{ name, ddl string }{
		{"fetchers", `CREATE TABLE fetchers(fetcher_id VARCHAR(64) NOT NULL PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`},
		{"reports", `CREATE TABLE reports(seq INT NOT NULL PRIMARY KEY) ENGINE=InnoDB`},
		{"wire_submissions_raw", wireMetaFixtureParent},
	} {
		exec(table.ddl)
		created = append(created, table.name)
	}
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='wire_evidence_projection'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("fixture refuses preexisting projection")
	}
	exec(`INSERT INTO fetchers VALUES('fixture'),('cascade'),('concurrent-cascade')`)
	exec(`INSERT INTO reports VALUES(1),(2),(3),(4),(10),(11),(12),(13)`)
	insert := func(id, fetcher string, seq int, submitted string, observed any, report, provenance, extensions any) {
		t.Helper()
		exec(`INSERT INTO wire_submissions_raw(submission_id,receipt_id,fetcher_id,source_id,schema_version,submitted_at,observed_at,agent_id,lane,material_hash,report_seq,report_json,provenance_json,extensions_json) VALUES(?,?,?,?,'v1',?,?,'fixture','digital',REPEAT('a',64),?,?,?,?)`, id, "receipt-"+id, fetcher, "source-"+id, submitted, observed, seq, report, provenance, extensions)
	}
	seed, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var originalMode, globalMode string
	var originalMetadata, originalRow int
	if err := seed.QueryRowContext(context.Background(), `SELECT @@SESSION.sql_mode,@@GLOBAL.sql_mode,@@SESSION.lock_wait_timeout,@@SESSION.innodb_lock_wait_timeout`).Scan(&originalMode, &globalMode, &originalMetadata, &originalRow); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(context.Background(), `SET SESSION sql_mode=''`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(context.Background(), `INSERT INTO wire_submissions_raw(submission_id,receipt_id,fetcher_id,source_id,schema_version,submitted_at,observed_at,agent_id,lane,material_hash,report_seq,report_json) VALUES('legacy-zero','receipt-zero','fixture','source-zero','v1','0000-00-00','0000-00-00','fixture','digital',REPEAT('a',64),1,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(context.Background(), `SET SESSION sql_mode=?`, originalMode); err != nil {
		t.Fatal(err)
	}
	seed.Close()
	insert("nullable", "fixture", 2, "2026-10-01", nil, `{"digital_context":{"platform":null,"legacy_source":"null","metadata":{"source_url":null,"author_username":9223372036854775807}},"privacy":{"contains_pii":true,"requires_redaction":"true"}}`, `{"generation_method":{"kind":"human"}}`, `{"observation_time_supplied":[true]}`)
	largeDoc, _ := json.Marshal(map[string]any{"digital_context": map[string]any{"platform": "github", "metadata": map[string]any{"source_url": "", "source_published_at": "2026-01-01", "author_did": map[string]any{"id": "did:fixture"}}, "source_url": " HTTPS://example.test/source ", "url": "null", "author_handle": []string{"fixture"}}, "evidence_bundle": []map[string]any{{"uri": "data:image/png;base64," + strings.Repeat("A", 1024*1024)}}})
	insert("large-uri", "fixture", 3, "2026-10-02", "2026-10-01", string(largeDoc), nil, nil)
	insert("absent", "fixture", 4, "2026-10-03", nil, `{}`, nil, nil)
	insert("numeric", "fixture", 3, "2026-10-03", nil, `{"digital_context":{"metadata":{"author_did":1,"source_published_at":null}}}`, nil, nil)
	exec(`CREATE TRIGGER wire_original_before BEFORE INSERT ON wire_submissions_raw FOR EACH ROW SET @wire_original_before=COALESCE(@wire_original_before,0)+1`)
	var name, beforeDDL, afterDDL, beforeHistory, afterHistory string
	var beforeID, afterID uint64
	const identity = `SELECT TABLE_ID FROM INFORMATION_SCHEMA.INNODB_TABLES WHERE NAME=CONCAT(DATABASE(),'/wire_submissions_raw')`
	const history = `SELECT SHA2(GROUP_CONCAT(SHA2(CAST(JSON_ARRAY(submission_id,receipt_id,fetcher_id,source_id,DATE_FORMAT(submitted_at,'%Y-%m-%d %H:%i:%s'),DATE_FORMAT(observed_at,'%Y-%m-%d %H:%i:%s'),actor_kind,channel,report_seq,report_json,provenance_json,extensions_json) AS CHAR),256) ORDER BY submission_id),256) FROM wire_submissions_raw`
	if err := db.QueryRow(identity).Scan(&beforeID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SHOW CREATE TABLE wire_submissions_raw`).Scan(&name, &beforeDDL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(history).Scan(&beforeHistory); err != nil {
		t.Fatal(err)
	}
	// A failed migration may already have created the child. Register cleanup
	// before calling it so retries cannot leave a child FK blocking parent drop.
	created = append(created, wiremeta.Table)
	for i := 0; i < 2; i++ {
		if err := wiremeta.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(identity).Scan(&afterID); err != nil || afterID != beforeID {
		t.Fatal("projection rebuilt parent table")
	}
	if err := db.QueryRow(`SHOW CREATE TABLE wire_submissions_raw`).Scan(&name, &afterDDL); err != nil || afterDDL != beforeDDL {
		t.Fatal("projection changed parent schema/FKs/indexes")
	}
	if err := db.QueryRow(history).Scan(&afterHistory); err != nil || afterHistory != beforeHistory {
		t.Fatal("projection changed original source history")
	}
	assertSession := func() {
		t.Helper()
		var mode, global string
		var metadata, row int
		if err := db.QueryRow(`SELECT @@SESSION.sql_mode,@@GLOBAL.sql_mode,@@SESSION.lock_wait_timeout,@@SESSION.innodb_lock_wait_timeout`).Scan(&mode, &global, &metadata, &row); err != nil || mode != originalMode || global != globalMode || metadata != originalMetadata || row != originalRow {
			t.Fatal("projection changed caller/pool/global session validation or lock settings")
		}
	}
	assertSession()
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("migration implicitly backfilled history")
	}
	if _, err := wiremeta.Backfill(context.Background(), db, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	assertWireMetaFallbackParity(t, db)
	if _, err := wiremeta.Backfill(context.Background(), db, []int{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	assertWireMetaFallbackParity(t, db)
	assertSession()
	var nullNode, literalNode, jsonInteger, generation, observation []byte
	if err := db.QueryRow(`SELECT digital_source_url,platform,author_username,generation_method,observation_time_supplied FROM wire_evidence_projection WHERE submission_id='nullable'`).Scan(&nullNode, &literalNode, &jsonInteger, &generation, &observation); err != nil {
		t.Fatal(err)
	}
	if nullNode != nil || string(literalNode) != "null" || string(jsonInteger) != "9223372036854775807" || string(generation) != `{"kind": "human"}` || string(observation) != "[true]" {
		t.Fatal("projection conflated SQL NULL/JSON null or lost exact JSON objects/arrays/integers")
	}
	var blocker, httpURI, stringNull, mediaURI []byte
	if err := db.QueryRow(`SELECT metadata_source_url,digital_source_url,digital_url,evidence_uri FROM wire_evidence_projection WHERE submission_id='large-uri'`).Scan(&blocker, &httpURI, &stringNull, &mediaURI); err != nil {
		t.Fatal(err)
	}
	if string(blocker) != `"__non_http_source_uri__"` || string(mediaURI) != `"__non_http_source_uri__"` || string(stringNull) != `"null"` || string(httpURI) != `" HTTPS://example.test/source "` {
		t.Fatal("URI gate changed absent/null/HTTP/blocker semantics or copied inline media")
	}
	assertNumeric := func(id, kind string) {
		t.Helper()
		var parentKind, childKind, nullKind string
		if err := db.QueryRow(`SELECT JSON_TYPE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.author_did')),JSON_TYPE(h.author_did),JSON_TYPE(h.source_published_at) FROM wire_submissions_raw ws JOIN wire_evidence_projection h ON h.submission_id=ws.submission_id WHERE ws.submission_id=?`, id).Scan(&parentKind, &childKind, &nullKind); err != nil || parentKind != "INTEGER" || childKind != kind || nullKind != "NULL" {
			t.Fatalf("exact numeric/JSON-null type differs: parent=%s child=%s null=%s err=%v", parentKind, childKind, nullKind, err)
		}
	}
	// An equal-value 1 -> 1.0 JSON UPDATE is a no-op. Force a different value
	// first so this fixture stores a real DOUBLE corruption for repair.
	corruptNumeric := func(id string) {
		t.Helper()
		exec(`UPDATE wire_evidence_projection SET author_did=CAST('2.0' AS JSON) WHERE submission_id=?`, id)
		exec(`UPDATE wire_evidence_projection SET author_did=CAST('1.0' AS JSON) WHERE submission_id=?`, id)
		assertNumeric(id, "DOUBLE")
	}
	corruptNumeric("numeric")
	if _, err := wiremeta.Backfill(context.Background(), db, []int{3}); err != nil {
		t.Fatal(err)
	}
	assertNumeric("numeric", "INTEGER")
	assertWireMetaFallbackParity(t, db)
	// Child FK moves its ID before AFTER UPDATE, so deleting only OLD.id no
	// longer reaches this corrupted row. NEW-key replacement must repair it.
	corruptNumeric("numeric")
	exec(`UPDATE wire_submissions_raw SET submission_id='numeric-moved' WHERE submission_id='numeric'`)
	assertNumeric("numeric-moved", "INTEGER")
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection WHERE BINARY submission_id='numeric'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("numeric PK move retained OLD identity")
	}
	assertWireMetaFallbackParity(t, db)
	strict, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.ExecContext(context.Background(), `SET SESSION sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,NO_ENGINE_SUBSTITUTION'`); err != nil {
		t.Fatal(err)
	}
	if _, err := strict.ExecContext(context.Background(), `UPDATE wire_submissions_raw SET actor_kind='human',report_json=JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',true)) WHERE submission_id='legacy-zero'`); err != nil {
		t.Fatalf("copying historical zero timestamps failed a strict parent update: %v", err)
	}
	if _, err := strict.ExecContext(context.Background(), `UPDATE wire_submissions_raw SET submitted_at='0000-00-00' WHERE submission_id='absent'`); err == nil {
		t.Fatal("projection relaxed strict caller parent date validation")
	}
	if _, err := strict.ExecContext(context.Background(), `SET SESSION sql_mode=?`, originalMode); err != nil {
		t.Fatal(err)
	}
	strict.Close()
	assertWireMetaFallbackParity(t, db)
	exec(`UPDATE wire_submissions_raw SET submission_id='MOVED-NULL',report_seq=3,observed_at=NULL,actor_kind='human',channel='app',report_json=NULL,provenance_json=NULL,extensions_json=NULL WHERE submission_id='nullable'`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection WHERE BINARY submission_id='nullable'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("PK move left an old projection identity")
	}
	assertWireMetaFallbackParity(t, db)
	exec(`DELETE FROM wire_submissions_raw WHERE submission_id='MOVED-NULL'`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection WHERE submission_id='MOVED-NULL'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("parent delete left metadata")
	}
	insert("after-migration", "fixture", 3, "2026-10-03", nil, `{"privacy":{"requires_redaction":true}}`, nil, nil)
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME='wire_original_before'`).Scan(&existing); err != nil || existing != 1 {
		t.Fatal("migration replaced an unrelated original trigger")
	}
	exec(`UPDATE wire_evidence_projection SET report_seq=999,requires_redaction=NULL WHERE submission_id='after-migration'`)
	assertWireMetaFallbackParity(t, db) // mismatch header falls back to exact live parent
	if _, err := wiremeta.Backfill(context.Background(), db, []int{3}); err != nil {
		t.Fatal(err)
	}
	assertWireMetaFallbackParity(t, db)
	insert("set-null", "fixture", 4, "2026-10-03", nil, `{"privacy":{"contains_pii":true}}`, nil, nil)
	exec(`DELETE FROM reports WHERE seq=4`)
	var staleSeq int
	var rawSeq sql.NullInt64
	if err := db.QueryRow(`SELECT ws.report_seq,h.report_seq FROM wire_submissions_raw ws JOIN wire_evidence_projection h ON h.submission_id=ws.submission_id WHERE ws.submission_id='set-null'`).Scan(&rawSeq, &staleSeq); err != nil || rawSeq.Valid || staleSeq != 4 {
		t.Fatal("fixture did not exercise skipped trigger during report SET NULL")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_submissions_raw FORCE INDEX(fk_wire_submission_report) WHERE report_seq=4`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("live original identity guard exposed stale report metadata after SET NULL")
	}
	assertWireMetaFallbackParity(t, db)
	insert("fetcher-cascade", "cascade", 3, "2026-10-03", nil, `{}`, nil, nil)
	exec(`DELETE FROM fetchers WHERE fetcher_id='cascade'`)
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection WHERE submission_id='fetcher-cascade'`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("fetcher parent cascade skipped projection deletion")
	}
	// Hold projection INSERT before commit while parent locks must protect the
	// copy against direct and cascaded source writes/deletion.
	exec(`CREATE TRIGGER wire_meta_fixture_delay AFTER INSERT ON wire_evidence_projection FOR EACH ROW BEGIN DO GET_LOCK('cleanapp_wire_meta_fixture_gate',5); DO SLEEP(0.6); DO RELEASE_LOCK('cleanapp_wire_meta_fixture_gate'); END`)
	for i, operation := range []string{"update", "delete", "fetcher-cascade", "report-set-null"} {
		seq := 10 + i
		id := "concurrent-" + operation
		fetcher := "fixture"
		if operation == "fetcher-cascade" {
			fetcher = "concurrent-cascade"
		}
		insert(id, fetcher, seq, "2026-10-04", nil, `{"privacy":{"contains_pii":false}}`, nil, nil)
		exec(`DELETE FROM wire_evidence_projection WHERE submission_id=?`, id)
		copied := make(chan error, 1)
		go func() { _, err := wiremeta.Backfill(context.Background(), db, []int{seq}); copied <- err }()
		deadline := time.Now().Add(2 * time.Second)
		for {
			var held int
			if err := db.QueryRow(`SELECT IS_USED_LOCK('cleanapp_wire_meta_fixture_gate') IS NOT NULL`).Scan(&held); err != nil {
				t.Fatal(err)
			}
			if held != 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("copy did not reach concurrency gate")
			}
			time.Sleep(10 * time.Millisecond)
		}
		changed := make(chan error, 1)
		go func() {
			var err error
			switch operation {
			case "update":
				_, err = db.Exec(`UPDATE wire_submissions_raw SET report_json=JSON_OBJECT('privacy',JSON_OBJECT('contains_pii',true)),observed_at=NULL WHERE submission_id=?`, id)
			case "delete":
				_, err = db.Exec(`DELETE FROM wire_submissions_raw WHERE submission_id=?`, id)
			case "fetcher-cascade":
				_, err = db.Exec(`DELETE FROM fetchers WHERE fetcher_id='concurrent-cascade'`)
			case "report-set-null":
				_, err = db.Exec(`DELETE FROM reports WHERE seq=?`, seq)
			}
			changed <- err
		}()
		select {
		case err := <-changed:
			t.Fatalf("%s bypassed locking current copy: %v", operation, err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := <-copied; err != nil {
			t.Fatal(err)
		}
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
		if operation == "delete" || operation == "fetcher-cascade" {
			if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection WHERE submission_id=?`, id).Scan(&existing); err != nil || existing != 0 {
				t.Fatal("copy resurrected deleted source metadata")
			}
		} else {
			assertWireMetaFallbackParity(t, db)
		}
	}
	exec(`DROP TRIGGER wire_meta_fixture_delay`)
	assertSession()
	// Physical replacement is allowed only for a derived child with no
	// downstream FK consumers, never for a business-record parent.
	defer db.Exec(`DROP TABLE IF EXISTS fixture_wire_consumer`)
	exec(`CREATE TABLE fixture_wire_consumer(id INT PRIMARY KEY,submission_id VARCHAR(64) NOT NULL,FOREIGN KEY(submission_id) REFERENCES wire_evidence_projection(submission_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	if err := wiremeta.Migrate(context.Background(), db); err == nil {
		t.Fatal("accepted incoming child FK incompatible with exact replacement")
	}
	exec(`DROP TABLE fixture_wire_consumer`)
	// A real parent metadata lock must fail in five seconds without killing
	// another session, changing parent identity, or silently using unbounded DDL.
	exec(`DROP TRIGGER wire_evidence_after_update`)
	holder, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var heldID string
	if err := holder.QueryRow(`SELECT submission_id FROM wire_submissions_raw LIMIT 1`).Scan(&heldID); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = wiremeta.Migrate(context.Background(), db)
	elapsed := time.Since(started)
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1205 || elapsed < 4*time.Second || elapsed > 9*time.Second {
		holder.Rollback()
		t.Fatalf("metadata wait was not bounded: elapsed=%s err=%v", elapsed, err)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := wiremeta.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSession()
	if err := db.QueryRow(identity).Scan(&afterID); err != nil || afterID != beforeID {
		t.Fatal("migration retry rebuilt parent")
	}
	exec(`DROP TRIGGER wire_evidence_after_insert`)
	exec(`CREATE TRIGGER wire_evidence_after_insert AFTER INSERT ON wire_submissions_raw FOR EACH ROW SET @foreign_wire_projection=1`)
	if err := wiremeta.Migrate(context.Background(), db); err == nil {
		t.Fatal("accepted unrelated existing named trigger")
	}
}

func assertWireMetaFallbackParity(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	type identity struct {
		ID  string `json:"id"`
		Seq int    `json:"seq"`
	}
	ids := []identity{}
	expected := []string{}
	nodes := wiremeta.NodeExpressions("ws")
	scalar := []string{"DATE_FORMAT(ws.observed_at,'%Y-%m-%d %H:%i:%s')", "DATE_FORMAT(ws.submitted_at,'%Y-%m-%d %H:%i:%s')", "ws.actor_kind", "ws.channel"}
	rows, err := tx.Query(`SELECT ws.submission_id,ws.report_seq,CAST(JSON_ARRAY(` + strings.Join(append(scalar, nodes...), ",") + `) AS CHAR) FROM wire_submissions_raw ws FORCE INDEX(PRIMARY) WHERE ws.report_seq IS NOT NULL ORDER BY ws.submission_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id identity
		var fields string
		if err := rows.Scan(&id.ID, &id.Seq, &fields); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		expected = append(expected, fields)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	payload, _ := json.Marshal(ids)
	selected := []string{"DATE_FORMAT(IF(h.submission_id IS NULL,ws.observed_at,h.observed_at),'%Y-%m-%d %H:%i:%s')", "DATE_FORMAT(IF(h.submission_id IS NULL,ws.submitted_at,h.submitted_at),'%Y-%m-%d %H:%i:%s')", "IF(h.submission_id IS NULL,ws.actor_kind,h.actor_kind)", "IF(h.submission_id IS NULL,ws.channel,h.channel)"}
	for i, name := range wiremeta.NodeColumns() {
		selected = append(selected, "IF(h.submission_id IS NULL,"+nodes[i]+",h."+name+")")
	}
	rows, err = tx.Query(`SELECT ids.ord,CAST(JSON_ARRAY(`+strings.Join(selected, ",")+`) AS CHAR)
 FROM JSON_TABLE(?,'$[*]' COLUMNS(ord FOR ORDINALITY,id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.id',seq INT PATH '$.seq')) ids
 LEFT JOIN wire_evidence_projection h ON h.submission_id=ids.id AND h.report_seq=ids.seq
 LEFT JOIN wire_submissions_raw ws FORCE INDEX(PRIMARY) ON ws.submission_id=IF(h.submission_id IS NULL,ids.id,NULL) AND ws.report_seq=ids.seq`, string(payload))
	if err != nil {
		t.Fatal(err)
	}
	actual := make([]string, len(ids))
	for rows.Next() {
		var ordinal int
		var fields string
		if err := rows.Scan(&ordinal, &fields); err != nil {
			t.Fatal(err)
		}
		if ordinal < 1 || ordinal > len(actual) || actual[ordinal-1] != "" {
			t.Fatal("fallback returned duplicate/invalid identity ordinal")
		}
		actual[ordinal-1] = fields
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("partial/mismatch/full projection changed exact live scalar/node fields or nullable JSON")
	}
}
