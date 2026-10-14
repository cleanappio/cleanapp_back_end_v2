package database

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"cleanapp-common/evidence"
	"github.com/go-sql-driver/mysql"
)

func TestEvidenceSourceNormalizationMySQLFixture(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_SOURCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_SOURCE_TEST_DSN for dedicated local source_normalization_evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || cfg.DBName != "source_normalization_evidence_test" {
		t.Fatal("fixture only permits dedicated local source_normalization_evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, ddl := range []string{
		`CREATE TABLE reports (seq INT PRIMARY KEY,id VARCHAR(255),public_id VARCHAR(32),ts DATETIME(6) NULL,latitude FLOAT,longitude FLOAT,image BLOB) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_metadata_projection (seq INT PRIMARY KEY,id VARCHAR(255),public_id VARCHAR(32),ts DATETIME(6) NULL,latitude FLOAT,longitude FLOAT,has_original_media BOOL) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_analysis (seq INT,language VARCHAR(2),created_at DATETIME,brand_name VARCHAR(255),brand_display_name VARCHAR(255),classification VARCHAR(32),title VARCHAR(500),summary TEXT,severity_level FLOAT,is_valid BOOL) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_raw (report_seq INT PRIMARY KEY,source_type VARCHAR(64),source_id VARCHAR(255),fetcher_id VARCHAR(64),visibility VARCHAR(16)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE external_ingest_index (seq INT,source VARCHAR(64),external_id VARCHAR(255),source_url VARCHAR(512),source_timestamp DATETIME(6),created_at DATETIME(6)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE wire_submissions_raw (report_seq INT,report_json JSON,provenance_json JSON,extensions_json JSON,observed_at DATETIME(6),submitted_at DATETIME(6),actor_kind VARCHAR(32),channel VARCHAR(32)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE report_status (seq INT,status VARCHAR(32)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE reports_owners (seq INT,owner VARCHAR(64),is_public BOOL) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE case_reports (seq INT,case_id VARCHAR(64)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
		created = append(created, strings.Fields(ddl)[2])
	}
	if err := evidence.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	created = append(created, "report_evidence")
	ptr := func(value string) *string { return &value }
	base := func(seq int) evidenceSourceNormalizationRow {
		return evidenceSourceNormalizationRow{Seq: seq, ReportID: fmt.Sprintf("original_%d", seq), ReportTS: ptr("2026-10-14 00:00:00.123456"), WireReport: json.RawMessage(`{}`), WireProvenance: json.RawMessage(`{}`), WireExtensions: json.RawMessage(`{}`)}
	}
	inputs := make([]evidenceSourceNormalizationRow, 17)
	for i := range inputs {
		inputs[i] = base(i + 1)
	}
	inputs[0].EiiSource, inputs[0].EiiID, inputs[0].EiiURL = ptr("twitter"), ptr("10"), ptr("https://twitter.com/person/status/10?utm_source=share#context")
	inputs[0].WireObserved = ptr("2026-02-01 00:00:00.000000")
	inputs[0].WireProvenance = json.RawMessage(`{"generation_method":"twitter_submitter"}`)
	inputs[0].WireReport = json.RawMessage(`{"digital_context":{"metadata":{"author_username":"Person"}}}`)
	inputs[1].EiiSource, inputs[1].EiiID, inputs[1].EiiURL = ptr("github_issue"), ptr("20"), ptr("https://github.com/org/repo/issues/20/")
	inputs[1].WireObserved = ptr("2026-02-28 00:00:00.000000")
	inputs[1].WireReport = json.RawMessage(`{"digital_context":{"metadata":{"source_published_at":"2026-03-01T00:00:00Z","author_username":"Developer"}}}`)
	inputs[1].WireProvenance = json.RawMessage(`{"generation_method":"github_issue_submitter"}`)
	inputs[2].EiiSource, inputs[2].EiiID = ptr("bluesky"), ptr("at://did:plc:person/app.bsky.feed.post/MixedCase")
	inputs[2].EiiURL = ptr("https://bsky.app/profile/person/post/MixedCase")
	inputs[2].WireReport = json.RawMessage(`{"digital_context":{"metadata":{"author_did":"did:plc:Person"}}}`)
	for _, i := range []int{3, 4} {
		inputs[i].ReportID, inputs[i].ReportTS = "reddit_t1_original", ptr("2025-09-05 00:00:00.000000")
		hash := sha1.Sum([]byte("reddit:t1_original"))
		inputs[i].EiiSource, inputs[i].EiiID = ptr("reddit_archive"), ptr(hex.EncodeToString(hash[:])[:32])
	}
	inputs[3].EiiCreated = ptr("2026-10-15 00:00:00.000000")
	inputs[5].ReportID, inputs[5].EiiSource, inputs[5].EiiID = "reddit_unmatched", ptr("reddit_archive"), ptr("unmatched")
	for _, i := range []int{6, 7, 8} {
		inputs[i].WireObserved, inputs[i].WireSubmitted = ptr("2026-10-01 00:00:00.000000"), ptr("2026-10-01 00:00:00.000000")
		inputs[i].WireActor, inputs[i].WireChannel = "human", "wire"
		inputs[i].WireProvenance = json.RawMessage(`{"generation_method":"human_submission"}`)
	}
	inputs[7].WireExtensions = json.RawMessage(`{"observation_time_supplied":true}`)
	inputs[8].WireProvenance = json.RawMessage(`{"generation_method":"shared_submission"}`)
	inputs[9].WireObserved, inputs[9].WireSubmitted = ptr("2026-02-01 00:00:00.000000"), ptr("2026-10-14 00:00:00.000000")
	inputs[9].WireProvenance = json.RawMessage(`{"generation_method":"twitter_submitter"}`)
	inputs[9].WireReport = json.RawMessage(`{"digital_context":{"platform":"twitter","metadata":{"source_published_at":null,"author_username":"Person"}}}`)
	inputs[10].RawSourceType, inputs[10].RawSourceID, inputs[10].FetcherID = ptr("reddit"), ptr("raw-id"), "fetcher"
	inputs[10].WireObserved = ptr("2026-02-01 00:00:00.000000")
	inputs[10].WireProvenance = json.RawMessage(`{"generation_method":"human_authored_public"}`)
	inputs[10].WireReport = json.RawMessage(`{"digital_context":{"platform":null,"legacy_source":"reddit","source_url":"https://example.test/fallback","metadata":{"source_url":{"unexpected":"shape"},"source_published_at":null,"author_did":null,"author_username":9007199254740993}},"privacy":{"contains_pii":true}}`)
	inputs[11].WireReport = json.RawMessage(`{"digital_context":{"platform":"x","metadata":{"source_url":null,"author_username":["Mixed","Shape"]},"source_url":"https://x.com/person/status/123/"},"privacy":{"requires_redaction":true}}`)
	inputs[12].EiiURL = ptr("data:image/png;base64,placeholder")
	inputs[12].WireReport = json.RawMessage(`{"digital_context":{"source_url":"https://example.test/masked-valid-url"}}`)
	inputs[13].WireReport = json.RawMessage(`{"digital_context":{"metadata":{"source_url":"https://Example.test/Path?keep=1#remove"}}}`)
	inputs[14].ReportTS = nil
	inputs[15].EiiPublished = ptr("0000-00-00 00:00:00.000000")
	inputs[15].ReportTS, inputs[15].WireObserved = ptr("0000-00-00 00:00:00.000000"), ptr("0000-00-00 00:00:00.000000")
	inputs[15].WireSubmitted = ptr("2026-10-14 00:00:00.000000")
	inputs[15].WireProvenance = json.RawMessage(`{"generation_method":"human_submission"}`)
	inputs[16] = inputs[3]
	inputs[16].Seq, inputs[16].EiiCreated = 17, ptr("0000-00-00 00:00:00.000000")
	var originalMode string
	if err := db.QueryRow(`SELECT @@SESSION.sql_mode`).Scan(&originalMode); err != nil {
		t.Fatal(err)
	}
	// Seed a legacy zero date without changing the mode used by normalization.
	if _, err := db.Exec(`SET SESSION sql_mode=''`); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if _, err := db.Exec(`INSERT INTO reports VALUES (?,?,?, ?,0,0,'')`, input.Seq, input.ReportID, fmt.Sprintf("r_%d", input.Seq), input.ReportTS); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO report_analysis VALUES (?,'en','2026-10-14','test','Test','digital','Generated title','Generated summary',0.5,TRUE)`, input.Seq); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO report_raw VALUES (?,?,?,?,'public')`, input.Seq, input.RawSourceType, input.RawSourceID, input.FetcherID); err != nil {
			t.Fatal(err)
		}
		if input.EiiSource != nil || input.EiiID != nil || input.EiiURL != nil || input.EiiPublished != nil {
			if _, err := db.Exec(`INSERT INTO external_ingest_index VALUES (?,?,?,?,?,?)`, input.Seq, input.EiiSource, input.EiiID, input.EiiURL, input.EiiPublished, input.EiiCreated); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO wire_submissions_raw VALUES (?,?,?,?,?,?,?,?)`, input.Seq, []byte(input.WireReport), []byte(input.WireProvenance), []byte(input.WireExtensions), input.WireObserved, input.WireSubmitted, input.WireActor, input.WireChannel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`SET SESSION sql_mode=?`, originalMode); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	actual, err := normalizeEvidenceSources(context.Background(), tx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	args := []interface{}{"en"}
	placeholders := make([]string, len(inputs))
	for i, input := range inputs {
		args = append(args, input.Seq)
		placeholders[i] = "?"
	}
	cte, args := evidenceCTE(EvidenceFilter{Kind: "all"}, "ra.seq IN ("+strings.Join(placeholders, ",")+")", args)
	query := cte + `SELECT seq,source_platform,source_url,source_timestamp,observed_at,ts,ingestion_time_missing,actor_kind,fetcher_id,privacy_restricted,source_key,external_source,intake_channel,generation_method,COALESCE(source_account_key,''),ts IS NULL FROM normalized`
	rows, err := tx.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var checked int
	for rows.Next() {
		var expected evidenceRecord
		var published, observed, ingested sql.NullTime
		if err := rows.Scan(&expected.Seq, &expected.SourcePlatform, &expected.SourceURL, &published, &observed, &ingested, &expected.IngestionTimeMissing, &expected.ActorKind, &expected.FetcherID, &expected.PrivacyRestricted, &expected.SourceKey, &expected.ExternalSource, &expected.IntakeChannel, &expected.GenerationMethod, &expected.SourceAccountKey, &expected.IngestedAtNull); err != nil {
			t.Fatal(err)
		}
		if published.Valid {
			expected.PublishedAt = &published.Time
		}
		if observed.Valid {
			expected.ObservedAt = &observed.Time
		}
		if ingested.Valid {
			expected.IngestedAt = ingested.Time
		}
		if !reflect.DeepEqual(actual[expected.Seq], expected) {
			t.Fatalf("compact metadata differs from legacy SQL for fixture sequence%d", expected.Seq)
		}
		checked++
	}
	if err := rows.Err(); err != nil || checked != len(inputs) {
		t.Fatalf("legacy source parity incomplete: checked%d err=%v", checked, err)
	}
	if actual[10].PublishedAt != nil || actual[10].ObservedAt == nil || actual[11].SourcePlatform != "null" || actual[11].SourceURL != "" || actual[11].SourceAccountKey != "null:9007199254740993" || actual[13].SourceURL != "" || !actual[15].IngestedAt.IsZero() || actual[16].PublishedAt == nil || !actual[16].PublishedAt.IsZero() || actual[16].ObservedAt == nil || !actual[16].ObservedAt.IsZero() || !actual[17].IngestedAt.IsZero() || actual[17].IngestionTimeMissing {
		t.Fatal("JSON null/object/number, URL priority or SQL NULL/zero date semantics changed")
	}
	// Preserve microseconds in source/index dates while original publication
	// parsing deliberately retains the old nineteen-character precision.
	if actual[1].IngestedAt.Nanosecond() != 123456000 || actual[2].PublishedAt == nil || !actual[2].PublishedAt.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("source timestamp precision changed")
	}
}
