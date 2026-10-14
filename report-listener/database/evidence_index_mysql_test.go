package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestEvidenceIndexMySQLFixture(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_INDEX_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_INDEX_TEST_DSN for dedicated local classification_cover_evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "classification_cover_evidence_test" {
		t.Fatal("fixture only permits dedicated local classification_cover_evidence_test")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	// Match production: no declared primary key, Dynamic rows, a pre-existing
	// FULLTEXT index, exact scope/core types and independent translation history.
	exec(`CREATE TABLE report_analysis (seq INT NOT NULL,brand_name VARCHAR(255) NULL,is_valid TINYINT(1) NULL,classification ENUM('physical','digital') NULL,language VARCHAR(2),created_at TIMESTAMP NULL,severity_level FLOAT,title TEXT,description TEXT,brand_display_name TEXT,summary TEXT,FULLTEXT KEY ft_report(title,description,brand_name,brand_display_name,summary),KEY idx_ra_brand_valid_seq(brand_name,is_valid,seq)) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	defer db.Exec(`DROP TABLE report_analysis`)
	exec(`INSERT INTO report_analysis VALUES (1,'openai',TRUE,'digital','en','2026-10-14',0.7,'Original title','Retained description','OpenAI','Retained summary'),(1,'openai',TRUE,'digital','fr','2026-10-14',0.7,'Titre','Description originale','OpenAI','Resume'),(2,'openai',TRUE,'physical','en','2026-10-14',0.4,'Physical report','Retained physical description','OpenAI','Physical summary'),(3,'openai',FALSE,'digital','en','2026-10-14',0.1,'Invalid report','Retained invalid description','OpenAI','Invalid summary')`)
	tableIdentity := func() (int64, int64) {
		t.Helper()
		var id, space int64
		if err := db.QueryRow(`SELECT TABLE_ID,SPACE FROM INFORMATION_SCHEMA.INNODB_TABLES WHERE NAME=CONCAT(DATABASE(),'/report_analysis')`).Scan(&id, &space); err != nil {
			t.Fatal(err)
		}
		return id, space
	}
	historyHash := func() string {
		t.Helper()
		var hash string
		if err := db.QueryRow(`SELECT SHA2(GROUP_CONCAT(CONCAT_WS('|',seq,brand_name,is_valid,classification,language,created_at,severity_level,title,description,brand_display_name,summary) ORDER BY seq,language SEPARATOR '\n'),256) FROM report_analysis`).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		return hash
	}
	beforeID, beforeSpace := tableIdentity()
	beforeHistory := historyHash()
	exec(`SET SESSION lock_wait_timeout=45`)
	for attempt := 0; attempt < 2; attempt++ {
		if err := ensureEvidenceBrandCoreIndex(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		id, space := tableIdentity()
		if id != beforeID || space != beforeSpace || historyHash() != beforeHistory {
			t.Fatal("secondary index migration rebuilt the table or rewrote history")
		}
		var restoredTimeout int
		if err := db.QueryRow(`SELECT @@SESSION.lock_wait_timeout`).Scan(&restoredTimeout); err != nil || restoredTimeout != 45 {
			t.Fatalf("metadata lock guard leaked into pooled session: %d err=%v", restoredTimeout, err)
		}
	}
	var fulltextColumns string
	if err := db.QueryRow(`SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_analysis' AND INDEX_NAME='ft_report' AND INDEX_TYPE='FULLTEXT'`).Scan(&fulltextColumns); err != nil || fulltextColumns != "title,description,brand_name,brand_display_name,summary" {
		t.Fatal("legacy FULLTEXT definition changed")
	}
	var searchableRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_analysis WHERE MATCH(title,description,brand_name,brand_display_name,summary) AGAINST('Retained' IN BOOLEAN MODE)`).Scan(&searchableRows); err != nil || searchableRows != 3 {
		t.Fatal("legacy FULLTEXT history is no longer searchable")
	}
	var planJSON string
	if err := db.QueryRow(`EXPLAIN FORMAT=JSON SELECT seq,language,created_at,severity_level FROM report_analysis FORCE INDEX(idx_ra_brand_valid_class_core) WHERE brand_name IN ('openai','chatgpt') AND is_valid=TRUE AND classification='digital'`).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatal(err)
	}
	var covering bool
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if value["table_name"] == "report_analysis" && value["key"] == evidenceBrandCoreIndex && value["using_index"] == true {
				covering = true
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(plan)
	if !covering {
		t.Fatal("new index did not cover the exact digital scope/core read")
	}
	// A named foreign/wrong definition must remain untouched and fail closed.
	exec(`ALTER TABLE report_analysis DROP INDEX idx_ra_brand_valid_class_core,ALGORITHM=INPLACE,LOCK=NONE`)
	exec(`ALTER TABLE report_analysis ADD INDEX idx_ra_brand_valid_class_core(brand_name,is_valid,seq),ALGORITHM=INPLACE,LOCK=NONE`)
	if err := ensureEvidenceBrandCoreIndex(context.Background(), db); err == nil {
		t.Fatal("migration silently accepted/replaced a wrong named index")
	}
	// An unrelated open reader transaction must cause a bounded metadata-lock
	// failure, rather than an indefinite queue or an algorithm/copy fallback.
	exec(`ALTER TABLE report_analysis DROP INDEX idx_ra_brand_valid_class_core,ALGORITHM=INPLACE,LOCK=NONE`)
	holder, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	tx, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var heldRows int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM report_analysis`).Scan(&heldRows); err != nil {
		t.Fatal(err)
	}
	boundedCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	start := time.Now()
	err = ensureEvidenceBrandCoreIndex(boundedCtx, db)
	cancel()
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1205 || time.Since(start) < 4*time.Second || time.Since(start) > 10*time.Second {
		t.Fatalf("metadata lock wait was not bounded at five seconds: duration=%s err=%v", time.Since(start), err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var restoredTimeout, installedIndexes int
	if err := db.QueryRow(`SELECT @@SESSION.lock_wait_timeout`).Scan(&restoredTimeout); err != nil || restoredTimeout != 45 {
		t.Fatal("failed metadata lock migration did not restore pooled session")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_analysis' AND INDEX_NAME='idx_ra_brand_valid_class_core'`).Scan(&installedIndexes); err != nil || installedIndexes != 0 || historyHash() != beforeHistory {
		t.Fatal("failed metadata lock migration changed history or installed a partial index")
	}
	if err := ensureEvidenceBrandCoreIndex(context.Background(), db); err != nil {
		t.Fatal("index migration could not retry after reader transaction released its lock")
	}
}
