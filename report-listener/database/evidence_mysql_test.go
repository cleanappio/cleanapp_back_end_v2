package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"report-listener/models"
)

// This fixture is intentionally opt-in and refuses nonlocal/non-test databases.
// CI can run it on MySQL 8+ to check real window/JSON/CTE semantics. It never uses
// application DB credentials or contacts a production server.
func TestEvidenceMySQLFixture(t *testing.T) {
	dsn := os.Getenv("EVIDENCE_TEST_DSN")
	if dsn == "" {
		t.Skip("set EVIDENCE_TEST_DSN to a dedicated local MySQL 8+ database ending _evidence_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_evidence_test") {
		t.Fatal("fixture only permits dedicated local databases ending _evidence_test")
	}
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Match production: legacy report/index tables use unicode_ci, while Wire,
	// ownership/status and case tables use 0900_ai_ci. JSON extraction is binary.
	statements := []string{
		`CREATE TABLE reports (seq INT PRIMARY KEY,public_id VARCHAR(32),ts DATETIME,latitude DOUBLE,longitude DOUBLE,image BLOB,id VARCHAR(255) NOT NULL DEFAULT '') DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_analysis (seq INT,language VARCHAR(2),brand_name VARCHAR(255),brand_display_name VARCHAR(255),is_valid BOOL,classification VARCHAR(32),created_at DATETIME,title VARCHAR(500),summary TEXT,severity_level FLOAT,FULLTEXT KEY fixture_original_fulltext (title,summary)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE report_raw (report_seq INT PRIMARY KEY,source_type VARCHAR(64),source_id VARCHAR(255),fetcher_id VARCHAR(64),visibility VARCHAR(16)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE external_ingest_index (seq INT NOT NULL,source VARCHAR(64) NOT NULL,external_id VARCHAR(255) NOT NULL,source_url VARCHAR(500),source_timestamp DATETIME,created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,PRIMARY KEY(source,external_id,seq),KEY idx_seq(seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE fetchers (fetcher_id VARCHAR(64) PRIMARY KEY) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE wire_submissions_raw (fetcher_id VARCHAR(64) NULL,submission_id VARCHAR(64) NOT NULL DEFAULT (UUID()) PRIMARY KEY,report_seq INT,report_json JSON,provenance_json JSON,observed_at TIMESTAMP NULL,submitted_at TIMESTAMP NOT NULL,actor_kind VARCHAR(32) NOT NULL DEFAULT 'machine',channel VARCHAR(32) NOT NULL DEFAULT 'wire',extensions_json JSON,KEY fixture_wire_report_seq(report_seq)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE report_status (seq INT,status VARCHAR(32)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE reports_owners (seq INT,owner VARCHAR(64),is_public BOOL) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE case_reports (seq INT,case_id VARCHAR(64)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	// A pre-existing table is an error rather than silently overwriting data.
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		created = append(created, strings.Fields(statement)[2])
	}
	// Evidence migration may create its own table, but must never rebuild the
	// legacy FULLTEXT analysis table or rewrite existing multilingual history.
	if _, err := db.Exec(`INSERT INTO report_analysis VALUES (900,'en','historytest','History',TRUE,'digital','2025-09-01','Legacy original analysis','Retained legacy summary',0.4)`); err != nil {
		t.Fatal(err)
	}
	showCreate := func(table string) string {
		t.Helper()
		var name, definition string
		if err := db.QueryRow("SHOW CREATE TABLE "+table).Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		return definition
	}
	legacyAnalysisSchema, legacyReportSchema := showCreate("report_analysis"), showCreate("reports")
	var existingEvidenceTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_evidence'`).Scan(&existingEvidenceTables); err != nil {
		t.Fatal(err)
	}
	if existingEvidenceTables != 0 {
		t.Fatal("fixture refuses a pre-existing report_evidence table")
	}
	created = append(created, "report_evidence")
	for attempt := 0; attempt < 2; attempt++ {
		if err := evidence.Migrate(context.Background(), db); err != nil {
			t.Fatalf("separate evidence migration attempt%d: %v", attempt+1, err)
		}
		if showCreate("report_analysis") != legacyAnalysisSchema || showCreate("reports") != legacyReportSchema {
			t.Fatal("evidence migration altered a legacy table or its FULLTEXT/index definitions")
		}
		var title, summary string
		var historyRows int
		if err := db.QueryRow(`SELECT COUNT(*),MAX(title),MAX(summary) FROM report_analysis WHERE seq=900`).Scan(&historyRows, &title, &summary); err != nil {
			t.Fatal(err)
		}
		if historyRows != 1 || title != "Legacy original analysis" || summary != "Retained legacy summary" {
			t.Fatal("evidence migration rewrote legacy analysis history")
		}
	}
	if err := ensureEvidenceBrandCoreIndex(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	legacyAnalysisSchema = showCreate("report_analysis")
	fixtures := []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES (1,'r_1','2026-10-14',0,0,''),(2,'r_2','2026-10-14',0,0,''),(3,'r_3','2026-10-14',0,0,''),(4,'r_4','2026-10-14',0,0,''),(5,'r_5','2026-10-14',0,0,''),(6,'r_6','2026-10-14',0,0,''),(7,'r_7','2026-10-14',0,0,''),(8,'r_8','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES
  (1,'en','claude','Claude',TRUE,'digital','2026-10-14','Failure one','AI summary',0.7),
  (1,'fr','claude','Claude',TRUE,'digital','2026-10-14','Echec','Resume',0.7),
  (2,'en','claudecode','Claude Code',TRUE,'digital','2026-10-14','Failure two','AI summary',0.8),
  (3,'en','claude','Claude',TRUE,'digital','2026-10-14','Coverage','AI summary',0.9),
  (4,'en','claude','Claude',TRUE,'digital','2026-10-14','Unclassified','AI summary',0.9),
  (5,'en','claude','Claude',TRUE,'digital','2026-10-14','Duplicate one','AI summary',0.7),
  (6,'en','claude','Claude',TRUE,'digital','2026-10-14','Quarantined','AI summary',0.9),
  (7,'en','claude','Claude',TRUE,'digital','2026-10-14','Private','AI summary',0.9),
  (8,'en','claude','Claude',TRUE,'digital','2026-10-14','Inactive','AI summary',0.9)`,
		`INSERT INTO report_evidence (seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES
  (1,'firsthand','I see failure one','Workflow blocked','Original describes own issue','affected',1),
  (2,'firsthand','I see failure two','Workflow blocked','Original describes own issue','affected',1),
  (3,'coverage','A news report','','Journalism','mentioned',1),
  (5,'firsthand','I see failure one','Workflow blocked','Original describes own issue','affected',1),
  (6,'firsthand','I see failure six','','Own issue','affected',1),
  (7,'firsthand','I see failure seven','','Own issue','affected',1),
  (8,'firsthand','I see failure eight','','Own issue','affected',1)`,
		`INSERT INTO external_ingest_index (seq,source,external_id,source_url,source_timestamp) VALUES (1,'twitter','10','https://twitter.com/person/status/10?utm_source=share','2026-02-01'),(5,'twitter','10','https://x.com/person/status/10/','2026-02-01'),(2,'github_issue','20','https://github.com/org/repo/issues/20','2026-03-01')`,
		`INSERT INTO report_raw VALUES (6,'twitter','60','fetcher','quarantine')`,
		`INSERT INTO report_status VALUES (8,'resolved')`,
		`INSERT INTO reports_owners VALUES (7,'private-user',FALSE)`,
		`INSERT INTO case_reports VALUES (1,'family_1'),(2,'family_1')`,
		`INSERT INTO wire_submissions_raw (report_seq,report_json,provenance_json,observed_at,submitted_at,actor_kind,channel) VALUES (1,'{"digital_context":{"metadata":{"author_username":"person"}}}','{"generation_method":"twitter_submitter"}','2026-02-01','2026-10-14','human','wire'),(2,'{"digital_context":{"metadata":{"author_username":"developer","source_published_at":"2026-03-01T00:00:00Z"}}}','{"generation_method":"github_issue_submitter"}','2026-02-28','2026-10-14','machine','wire'),(5,'{"digital_context":{"metadata":{"author_username":"person"}}}','{"generation_method":"twitter_submitter"}','2026-02-01','2026-10-14','human','wire')`,
	}
	for _, statement := range fixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var existingHeaderTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='external_ingest_header_projection'`).Scan(&existingHeaderTables); err != nil {
		t.Fatal(err)
	}
	if existingHeaderTables != 0 {
		t.Fatal("fixture refuses a pre-existing external_ingest_header_projection table")
	}
	created = append(created, "external_ingest_header_projection")
	legacySourceSchema := showCreate("external_ingest_index")
	for attempt := 0; attempt < 2; attempt++ {
		if err := eiiheader.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		if showCreate("external_ingest_index") != legacySourceSchema {
			t.Fatal("source header migration altered its parent")
		}
	}
	var initiallyProjected int
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_header_projection`).Scan(&initiallyProjected); err != nil || initiallyProjected != 0 {
		t.Fatal("header migration must retain unprojected history without an implicit backfill")
	}

	var existingWireTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='wire_evidence_projection'`).Scan(&existingWireTables); err != nil || existingWireTables != 0 {
		t.Fatal("fixture refuses a pre-existing Wire evidence projection table")
	}
	created = append(created, "wire_evidence_projection")
	legacyWireSchema := showCreate("wire_submissions_raw")
	for attempt := 0; attempt < 2; attempt++ {
		if err := wiremeta.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		if showCreate("wire_submissions_raw") != legacyWireSchema {
			t.Fatal("Wire projection migration altered its parent")
		}
	}
	var initiallyProjectedWire int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection`).Scan(&initiallyProjectedWire); err != nil || initiallyProjectedWire != 0 {
		t.Fatal("Wire projection migration must not copy historical payloads")
	}

	// Existing history is not implicitly copied by a startup migration. The
	// conditional fallback must retain it, and partial deterministic backfill
	// must not alter scoped visibility, source deduplication or family counts.
	var existingMetadataTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='report_metadata_projection'`).Scan(&existingMetadataTables); err != nil {
		t.Fatal(err)
	}
	if existingMetadataTables != 0 {
		t.Fatal("fixture refuses a pre-existing report_metadata_projection table")
	}
	created = append(created, "report_metadata_projection")
	// Retain an old zero-date row before the metadata triggers exist. It remains
	// unprojected historical input, so its raw date presence must survive fallback.
	var originalMode string
	if err := db.QueryRow(`SELECT @@SESSION.sql_mode`).Scan(&originalMode); err != nil {
		t.Fatal(err)
	}
	zeroMode := strings.ReplaceAll(strings.ReplaceAll(originalMode, "NO_ZERO_DATE", ""), "NO_ZERO_IN_DATE", "")
	if _, err := db.Exec(`SET SESSION sql_mode=?`, zeroMode); err != nil {
		t.Fatal(err)
	}
	_, zeroErr := db.Exec(`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES(107,'r_107','0000-00-00 00:00:00',0,0,'')`)
	if _, err := db.Exec(`SET SESSION sql_mode=?`, originalMode); err != nil {
		t.Fatal(err)
	}
	if zeroErr != nil {
		t.Fatal(zeroErr)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := reportmeta.Migrate(context.Background(), db); err != nil {
			t.Fatalf("metadata projection migration attempt%d: %v", attempt+1, err)
		}
		if showCreate("reports") != legacyReportSchema || showCreate("report_analysis") != legacyAnalysisSchema {
			t.Fatal("metadata projection migration altered a legacy table definition")
		}
	}

	d := &Database{db: db}
	created = append(created, evidencesourceprojection.Table)
	if err := evidencesourceprojection.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	checkProjected := func(f EvidenceFilter) {
		t.Helper()
		f, err := NormalizeEvidenceFilter(f)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := d.getEvidenceBrandPageProjected(context.Background(), f)
		if err != nil {
			t.Fatalf("projected reader: %v", err)
		}
		reference, err := d.getEvidenceBrandPageSQL(context.Background(), f)
		if err != nil || !reflect.DeepEqual(actual, reference) {
			t.Fatalf("projected/reference complete public parity differs: %v\nactual=%+v\nreference=%+v", err, actual, reference)
		}
	}
	page, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Overview.TotalReports != 5 {
		t.Fatalf("overview must count visible raw reports, not 3 grouped entries: %+v", page.Overview)
	}
	if page.Total != 1 || len(page.Reports) != 1 || page.KindCounts.Firsthand != 1 || page.KindCounts.Coverage != 1 || page.KindCounts.Unknown != 1 || page.KindCounts.All != 3 {
		t.Fatalf("public family/filter counts wrong: %+v", page)
	}
	checkProjected(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})

	if _, err := db.Exec(`INSERT INTO report_metadata_projection (seq,id,public_id,ts,latitude,longitude,has_original_media) SELECT seq,id,public_id,ts,latitude,longitude,LENGTH(image)>0 FROM reports WHERE MOD(seq,2)=0`); err != nil {
		t.Fatal(err)
	}
	mixed, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if !evidencePagesEqualIgnoringClock(page, mixed) {
		t.Fatal("partial metadata projection changed public page, family counts, source dates or privacy")
	}
	if _, err := eiiheader.Backfill(context.Background(), db, []int{1}); err != nil {
		t.Fatal(err)
	}
	partialHeader, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if !evidencePagesEqualIgnoringClock(page, partialHeader) {
		t.Fatal("partial source header projection changed source selection, fields, dates or exact counts")
	}
	checkProjected(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	var headerRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM external_ingest_header_projection`).Scan(&headerRows); err != nil || headerRows != 1 {
		t.Fatal("fixture must exercise mixed projected/unprojected source keys")
	}
	var projectedRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_metadata_projection`).Scan(&projectedRows); err != nil || projectedRows != 4 {
		t.Fatalf("fixture must exercise projected and unprojected history: rows=%d err=%v", projectedRows, err)
	}

	if _, err := wiremeta.Backfill(context.Background(), db, []int{1}); err != nil {
		t.Fatal(err)
	}
	partialWire, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if err != nil || !evidencePagesEqualIgnoringClock(page, partialWire) {
		t.Fatalf("partial Wire projection changed exact public semantics: %v", err)
	}
	checkProjected(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	var wireHeaderRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wire_evidence_projection`).Scan(&wireHeaderRows); err != nil || wireHeaderRows != 1 {
		t.Fatal("fixture must exercise mixed projected and unprojected Wire submissions")
	}

	// A report FK can change the parent link without firing Wire triggers. A
	// stale copied report_seq must be ignored by the authoritative ID/seq guard.
	if _, err := db.Exec(`UPDATE wire_evidence_projection SET report_seq=999 WHERE report_seq=1`); err != nil {
		t.Fatal(err)
	}
	mismatchedWire, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if err != nil || !evidencePagesEqualIgnoringClock(page, mismatchedWire) {
		t.Fatalf("stale Wire link bypassed original metadata fallback: %v", err)
	}
	checkProjected(EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if _, err := wiremeta.Backfill(context.Background(), db, []int{1}); err != nil {
		t.Fatal(err)
	}

	rec := page.Reports[0].Recurrence
	if rec.SupportingReportCount != 2 || rec.DuplicateImportCount != 1 || rec.SourceAccountCount == nil || *rec.SourceAccountCount != 2 {
		t.Fatalf("canonical sources/accounts inflated: %+v", rec)
	}
	if page.Reports[0].Evidence.ObservedAt == nil || page.Reports[0].Evidence.ObservedAt.Day() != 28 || page.Reports[0].Evidence.SourcePublishedAt.Month() != 3 {
		t.Fatal("genuine original observation and later publication conflated")
	}
	beyond, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand", Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	if beyond.Overview.TotalReports != 5 {
		t.Fatal("offset changed overview total")
	}
	if beyond.Total != 1 || beyond.KindCounts.All != 3 || len(beyond.Reports) != 0 || beyond.HasMore {
		t.Fatalf("empty offset page lost exact source counts or exposed a synthetic row: %+v", beyond)
	}
	empty, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "absentbrand"})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Overview.TotalReports != 0 || empty.Overview.ReportsLast30Days != 0 || empty.Overview.LatestReportAt != nil || empty.Overview.AsOf.IsZero() {
		t.Fatal("empty scope overview missing or synthetic")
	}
	if empty.Total != 0 || empty.KindCounts.All != 0 || len(empty.Reports) != 0 || empty.HasMore {
		t.Fatalf("empty scope exposed a synthetic count row: %+v", empty)
	}
	twitter, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Product: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if twitter.Reports[0].Evidence.ObservedAt != nil {
		t.Fatal("historical indexer publication became incident date")
	}
	for _, kind := range []string{"coverage", "unknown", "all"} {
		filtered, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: kind, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if filtered.Overview.TotalReports != 5 {
			t.Fatalf("kind %s changed all-kind overview total", kind)
		}
		if filtered.Total == 0 {
			t.Fatalf("%s lost existing material", kind)
		}
		if kind != "all" && filtered.Reports[0].Evidence.ReportKind != kind {
			t.Fatalf("%s leaked another kind", kind)
		}
	}
	product, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Product: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	if product.Overview.TotalReports != 1 {
		t.Fatal("product overview included another product")
	}
	if product.Total != 1 || product.Reports[0].Recurrence.SupportingReportCount != 1 {
		t.Fatal("product filter counts included another product")
	}

	unenriched, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unenriched.Reports) != 1 || unenriched.Reports[0].Report.Seq != 4 || unenriched.Reports[0].Evidence.EvidenceVersion != 0 || unenriched.Reports[0].Evidence.Excerpt != nil {
		t.Fatal("missing evidence row did not remain unknown/version0")
	}
	// English and French summaries remain independent history, while their one
	// source evidence record changes consistently for both list and detail reads.
	translationFixtures := []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES (27,'r_27','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES (27,'en','translationtest','Translation test',TRUE,'digital','2026-10-14','English issue title','English generated summary',0.5),(27,'fr','translationtest','Translation test',TRUE,'digital','2026-10-14','Titre de probleme','Resume francais',0.5)`,
		`INSERT INTO report_evidence (seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES (27,'firsthand','I observed the app failure','Workflow blocked','Own observation','affected',1)`,
	}
	for _, statement := range translationFixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for language, title := range map[string]string{"en": "English issue title", "fr": "Titre de probleme"} {
		translated, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "translationtest", Language: language, Kind: "firsthand"})
		if err != nil {
			t.Fatal(err)
		}
		if translated.Total != 1 || len(translated.Reports) != 1 || translated.Reports[0].Analysis[0].Title != title || translated.Reports[0].Evidence.Excerpt == nil || *translated.Reports[0].Evidence.Excerpt != "I observed the app failure" {
			t.Fatalf("analysis language changed immutable source evidence for%s", language)
		}
	}
	coverageFields := evidence.Fields{ReportKind: "coverage", EvidenceExcerpt: "A published service outage account", ClassificationReason: "Secondhand account", EntityRelationship: "mentioned"}
	if err := evidence.Save(context.Background(), db, 27, coverageFields, evidence.Version); err != nil {
		t.Fatal(err)
	}
	// MySQL evaluates duplicate-key assignments in order. A stale failed retry
	// must preserve every completed field, not only its version number.
	if err := evidence.Save(context.Background(), db, 27, evidence.Unknown("Stale failed retry"), 0); err != nil {
		t.Fatal(err)
	}
	var stored evidence.Fields
	var storedVersion int
	if err := db.QueryRow(`SELECT report_kind,COALESCE(evidence_excerpt,''),COALESCE(reported_impact,''),COALESCE(classification_reason,''),entity_relationship,evidence_version FROM report_evidence WHERE seq=27`).Scan(&stored.ReportKind, &stored.EvidenceExcerpt, &stored.ReportedImpact, &stored.ClassificationReason, &stored.EntityRelationship, &storedVersion); err != nil {
		t.Fatal(err)
	}
	if stored != coverageFields || storedVersion != evidence.Version {
		t.Fatalf("stale version0 retry overwrote completed per-report evidence: fields=%+v version=%d", stored, storedVersion)
	}
	// A completed unknown is versioned evidence, distinct from an absent row.
	if err := evidence.Save(context.Background(), db, 4, evidence.Unknown("Original is unavailable"), evidence.Version); err != nil {
		t.Fatal(err)
	}
	finalUnknown, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if len(finalUnknown.Reports) != 1 || finalUnknown.Reports[0].Report.Seq != 4 || finalUnknown.Reports[0].Evidence.EvidenceVersion != evidence.Version || finalUnknown.Reports[0].Evidence.ReportKind != "unknown" {
		t.Fatal("successful unknown enrichment was treated as missing/version0")
	}
	translatedCoverage, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "translationtest", Language: "fr", Kind: "coverage"})
	if err != nil {
		t.Fatal(err)
	}
	if translatedCoverage.Total != 1 || translatedCoverage.KindCounts.Firsthand != 0 || translatedCoverage.KindCounts.Coverage != 1 || translatedCoverage.Reports[0].Analysis[0].Title != "Titre de probleme" {
		t.Fatal("source-only evidence update did not consistently change filtered counts/translations")
	}
	var evidenceRows, analysisRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_evidence WHERE seq=27`).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM report_analysis WHERE seq=27`).Scan(&analysisRows); err != nil {
		t.Fatal(err)
	}
	if evidenceRows != 1 || analysisRows != 2 {
		t.Fatal("source evidence was duplicated across translated analysis rows")
	}
	translatedDetails := []*models.ReportWithAnalysis{{Report: models.Report{Seq: 27}}}
	if err := d.AttachPublicEvidence(context.Background(), translatedDetails); err != nil {
		t.Fatal(err)
	}
	if translatedDetails[0].Evidence == nil || translatedDetails[0].Evidence.ReportKind != "coverage" || translatedDetails[0].Evidence.ReportedImpact != nil {
		t.Fatal("detail evidence disagreed with updated per-report source evidence")
	}

	// Only the archive importer's stable ID hash proves that its reports.ts
	// means original publication, while index.created_at records acquisition.
	archiveFixtures := []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image,id) VALUES (22,'r_22','2025-09-05',0,0,'','reddit_t1_original'),(23,'r_23','2026-10-16',0,0,'','reddit_unmatched'),(24,'r_24','2026-10-17',0,0,'','reddit_t1_other_source'),(25,'r_25','2025-09-03',0,0,'','reddit_t3_explicit'),(26,'r_26','2025-09-01',0,0,'','reddit_t1_missing_ingest')`,
		`INSERT INTO report_analysis VALUES
  (22,'en','archivetimetest','Archive time test',TRUE,'digital','2026-10-14','Retained summary','AI summary',0.5),
  (23,'en','archivetimetest','Archive time test',TRUE,'digital','2026-10-14','Unmatched ID','AI summary',0.5),
  (24,'en','archivetimetest','Archive time test',TRUE,'digital','2026-10-14','Other source','AI summary',0.5),
  (25,'en','archivetimetest','Archive time test',TRUE,'digital','2026-10-14','Explicit publication','AI summary',0.5),
  (26,'en','archivetimetest','Archive time test',TRUE,'digital','2026-10-14','Missing indexing time','AI summary',0.5)`,
		`INSERT INTO report_evidence (seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES
  (22,'unknown','','','Original missing','unknown',1),
  (23,'unknown','','','Original missing','unknown',1),
  (24,'unknown','','','Original missing','unknown',1),
  (25,'unknown','','','Original missing','unknown',1),
  (26,'unknown','','','Original missing','unknown',1)`,
		`INSERT INTO external_ingest_index VALUES (22,'reddit_archive',LEFT(SHA1('reddit:t1_original'),32),'',NULL,'2026-10-15'),(23,'reddit_archive','unmatched','',NULL,'2026-10-15'),(24,'twitter',LEFT(SHA1('reddit:t1_other_source'),32),'',NULL,'2026-10-15'),(25,'reddit_archive',LEFT(SHA1('reddit:t3_explicit'),32),'','2025-08-07','2026-10-18'),(26,'reddit_archive',LEFT(SHA1('reddit:t1_missing_ingest'),32),'',NULL,NULL)`,
	}
	for _, statement := range archiveFixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	archivePage, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "archivetimetest", Kind: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if archivePage.Total != 5 || archivePage.KindCounts.Unknown != 5 || archivePage.KindCounts.Firsthand != 0 {
		t.Fatalf("archive metadata promoted missing originals or lost entries: %+v", archivePage.KindCounts)
	}
	noFirsthand, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "archivetimetest", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if noFirsthand.Total != 0 || noFirsthand.KindCounts.Unknown != 5 || noFirsthand.KindCounts.All != 5 || len(noFirsthand.Reports) != 0 {
		t.Fatalf("empty firsthand filter lost accessible unknown facet counts: %+v", noFirsthand)
	}
	archiveBySeq := map[int]*models.PublicEvidence{}
	for _, report := range archivePage.Reports {
		archiveBySeq[report.Report.Seq] = report.Evidence
	}
	original := archiveBySeq[22]
	if original.SourcePublishedAt == nil || original.SourcePublishedAt.Format("2006-01-02") != "2025-09-05" || original.IngestedAt == nil || original.IngestedAt.Format("2006-01-02") != "2026-10-15" || original.ObservedAt != nil {
		t.Fatalf("archive publication and indexing dates conflated: %+v", original)
	}
	for _, seq := range []int{23, 24} {
		unchanged := archiveBySeq[seq]
		if unchanged.SourcePublishedAt != nil || unchanged.IngestedAt == nil || unchanged.IngestedAt.Year() != 2026 || unchanged.IngestedAt.Day() != seq-7 {
			t.Fatalf("unproven archive path changed timestamps for%d: %+v", seq, unchanged)
		}
	}
	explicit := archiveBySeq[25]
	if explicit.SourcePublishedAt == nil || explicit.SourcePublishedAt.Format("2006-01-02") != "2025-08-07" || explicit.IngestedAt == nil || explicit.IngestedAt.Format("2006-01-02") != "2026-10-18" {
		t.Fatalf("explicit source publication lost precedence: %+v", explicit)
	}
	missing := archiveBySeq[26]
	if missing.SourcePublishedAt == nil || missing.SourcePublishedAt.Format("2006-01-02") != "2025-09-01" || missing.IngestedAt != nil {
		t.Fatalf("null archive indexing time invented an ingestion date: %+v", missing)
	}
	details := []*models.ReportWithAnalysis{{Report: models.Report{Seq: 22}}, {Report: models.Report{Seq: 26}}}
	if err := d.AttachPublicEvidence(context.Background(), details); err != nil {
		t.Fatal(err)
	}
	if details[0].Evidence == nil || details[0].Evidence.SourcePublishedAt == nil || details[0].Evidence.IngestedAt == nil || !details[0].Evidence.SourcePublishedAt.Equal(*original.SourcePublishedAt) || !details[0].Evidence.IngestedAt.Equal(*original.IngestedAt) || details[1].Evidence.IngestedAt != nil {
		t.Fatal("detail/archive list timestamps disagree")
	}

	// Case-sensitive source identifiers and URL paths must not collapse under
	// either legacy unicode_ci or modern 0900_ai_ci table collations.
	identityFixtures := []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES (17,'r_17','2026-10-14',0,0,''),(18,'r_18','2026-10-14',0,0,''),(19,'r_19','2026-10-14',0,0,''),(20,'r_20','2026-10-14',0,0,''),(21,'r_21','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES
  (17,'en','identitytest','Identity test',TRUE,'digital','2026-10-14','Upper path','AI summary',0.5),
  (18,'en','identitytest','Identity test',TRUE,'digital','2026-10-14','Lower path','AI summary',0.5),
  (19,'en','identitytest','Identity test',TRUE,'digital','2026-10-14','Upper ID','AI summary',0.5),
  (20,'en','identitytest','Identity test',TRUE,'digital','2026-10-14','Lower ID','AI summary',0.5),
  (21,'en','identitytest','Identity test',TRUE,'digital','2026-10-14','Duplicate upper path','AI summary',0.5)`,
		`INSERT INTO report_evidence (seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES
  (17,'firsthand','Original upper path','','Own observation','affected',1),
  (18,'firsthand','Original lower path','','Own observation','affected',1),
  (19,'firsthand','Original upper ID','','Own observation','affected',1),
  (20,'firsthand','Original lower ID','','Own observation','affected',1),
  (21,'firsthand','Original upper path','','Own observation','affected',1)`,
		`INSERT INTO external_ingest_index (seq,source,external_id,source_url,source_timestamp) VALUES (17,'archive','url-upper','https://example.com/Path',NULL),(18,'archive','url-lower','https://example.com/path',NULL),(19,'reddit','ABC','',NULL),(20,'reddit','abc','',NULL),(21,'archive','url-upper','https://example.com/Path',NULL)`,
		`INSERT INTO report_raw VALUES (19,'reddit','ABC','fetcher','public'),(20,'reddit','abc','fetcher','public')`,
	}
	for _, statement := range identityFixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	identityPage, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "identitytest", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if identityPage.Total != 4 || identityPage.KindCounts.Firsthand != 4 || len(identityPage.Reports) != 4 {
		t.Fatalf("case-sensitive originals collapsed: total=%d firsthand=%d reports=%d", identityPage.Total, identityPage.KindCounts.Firsthand, len(identityPage.Reports))
	}
	duplicates := 0
	for _, report := range identityPage.Reports {
		duplicates += report.Recurrence.DuplicateImportCount
	}
	if duplicates != 1 {
		t.Fatalf("identical source URL imports not collapsed: duplicates=%d", duplicates)
	}

	// Equal evidence completeness favors entity relevance. A strong singleton
	// must outrank a larger family whose reported impact is undocumented.
	rankingFixtures := []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES (9,'r_9','2026-10-14',0,0,''),(10,'r_10','2026-10-14',0,0,''),(11,'r_11','2026-10-14',0,0,''),(12,'r_12','2026-10-14',0,0,''),(13,'r_13','2026-10-14',0,0,''),(14,'r_14','2026-10-14',0,0,''),(15,'r_15','2026-10-14',0,0,''),(16,'r_16','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES
  (9,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Strong singleton','AI summary',0.5),
  (10,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Weak family one','AI summary',0.9),
  (11,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Weak family two','AI summary',0.9),
  (12,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Weak family three','AI summary',0.9),
  (13,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Dependency','AI summary',1.0),
  (14,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Mentioned','AI summary',1.0),
  (15,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Strong family dependency','AI summary',0.99),
  (16,'en','ranktest','Rank test',TRUE,'digital','2026-10-14','Strong family affected','AI summary',0.1)`,
		`INSERT INTO report_evidence (seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES
  (9,'firsthand','I observed a failure','Work blocked','Own observation','affected',1),
  (10,'firsthand','I observed a failure','','Own observation','affected',1),
  (11,'firsthand','I observed a failure','','Own observation','affected',1),
  (12,'firsthand','I observed a failure','','Own observation','affected',1),
  (13,'firsthand','I observed a failure','Work blocked','Own observation','dependency',1),
  (14,'firsthand','I observed a failure','Work blocked','Own observation','mentioned',1),
  (15,'firsthand','I observed a failure','Work blocked','Own observation','dependency',1),
  (16,'firsthand','I observed a failure','Work blocked','Own observation','affected',1)`,
		`INSERT INTO case_reports VALUES (10,'rank_weak'),(11,'rank_weak'),(12,'rank_weak'),(15,'rank_strong'),(16,'rank_strong')`,
	}
	for _, statement := range rankingFixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	ranked, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "ranktest", Kind: "firsthand", Sort: "signal"})
	if err != nil {
		t.Fatal(err)
	}
	wanted := []int{16, 9, 13, 14, 12}
	if len(ranked.Reports) != len(wanted) {
		t.Fatalf("ranked entries=%d want%d", len(ranked.Reports), len(wanted))
	}
	for i, seq := range wanted {
		if ranked.Reports[i].Report.Seq != seq {
			t.Fatalf("signal rank[%d]=%d want%d", i, ranked.Reports[i].Report.Seq, seq)
		}
	}
	if ranked.Reports[0].Evidence.EntityRelationship != "affected" || ranked.Reports[0].Recurrence.SupportingReportCount != 2 || ranked.Reports[len(wanted)-1].Recurrence.SupportingReportCount != 3 {
		t.Fatal("family representative/relevance/source-count semantics changed")
	}

	// Redaction flags apply to each source sample, not only to the selected
	// representative. Families and their source counts survive even when every
	// source is restricted and no public supporting sample can be returned.
	privacyFixtures := []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES (28,'r_28','2026-10-14',0,0,''),(29,'r_29','2026-10-14',0,0,''),(30,'r_30','2026-10-14',0,0,''),(31,'r_31','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES
  (28,'en','privacytest','Privacy test',TRUE,'digital','2026-10-14','Public source title','AI summary',0.5),
  (29,'en','privacytest','Privacy test',TRUE,'digital','2026-10-14','Restricted sibling title','AI summary',0.9),
  (30,'en','privacytest','Privacy test',TRUE,'digital','2026-10-14','Restricted family title one','AI summary',0.5),
  (31,'en','privacytest','Privacy test',TRUE,'digital','2026-10-14','Restricted family title two','AI summary',0.5)`,
		`INSERT INTO report_evidence (seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES
  (28,'firsthand','Public original source','Workflow blocked','Own observation','affected',1),
  (29,'firsthand','Restricted original source','Workflow blocked','Own observation','affected',1),
  (30,'firsthand','Restricted original source one','Workflow blocked','Own observation','affected',1),
  (31,'firsthand','Restricted original source two','Workflow blocked','Own observation','affected',1)`,
		`INSERT INTO external_ingest_index (seq,source,external_id,source_url,source_timestamp) VALUES (28,'archive','public','https://example.com/public-source',NULL),(29,'archive','restricted-sibling','https://example.com/restricted-sibling',NULL),(30,'archive','restricted-family-one','https://example.com/restricted-family-one',NULL),(31,'archive','restricted-family-two','https://example.com/restricted-family-two',NULL)`,
		`INSERT INTO wire_submissions_raw (report_seq,report_json,provenance_json,submitted_at,actor_kind,channel) VALUES (29,'{"privacy":{"contains_pii":true}}','{}','2026-10-14','machine','wire'),(30,'{"privacy":{"requires_redaction":true}}','{}','2026-10-14','machine','wire'),(31,'{"privacy":{"contains_pii":true}}','{}','2026-10-14','machine','wire')`,
		`INSERT INTO case_reports VALUES (28,'privacy_mixed'),(29,'privacy_mixed'),(30,'privacy_all'),(31,'privacy_all')`,
	}
	for _, statement := range privacyFixtures {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	privacyPage, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "privacytest", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if privacyPage.Total != 2 || privacyPage.KindCounts.Firsthand != 2 || privacyPage.KindCounts.All != 2 || len(privacyPage.Reports) != 2 {
		t.Fatalf("redacted supporting sources removed families/counts: total=%d counts=%+v reports=%d", privacyPage.Total, privacyPage.KindCounts, len(privacyPage.Reports))
	}
	for _, report := range privacyPage.Reports {
		recurrence := report.Recurrence
		if recurrence.CaseID == nil || recurrence.SupportingReportCount != 2 || !recurrence.CountsExact {
			t.Fatalf("redacted source sample changed exact family counts: %+v", recurrence)
		}
		switch *recurrence.CaseID {
		case "privacy_mixed":
			if report.Report.Seq != 28 || len(recurrence.SupportingSources) != 1 || recurrence.SupportingSources[0].Title != "Public source title" || stringValue(recurrence.SupportingSources[0].SourceURL) != "https://example.com/public-source" {
				t.Fatalf("restricted sibling title or URL leaked into public recurrence: %+v", recurrence.SupportingSources)
			}
		case "privacy_all":
			if recurrence.SupportingSources == nil || len(recurrence.SupportingSources) != 0 {
				t.Fatalf("all-restricted family must retain empty public source samples: %+v", recurrence.SupportingSources)
			}
		default:
			t.Fatalf("unexpected privacy fixture family: %s", *recurrence.CaseID)
		}
	}
	// Production stores severity as FLOAT. SQL's comparisons use its actual
	// stored value, rather than the rounded text representation sent by drivers.
	for _, statement := range []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES (101,'r_101',NULL,0,0,''),(102,'r_102','2026-10-14',0,0,''),(103,'r_103','2026-10-14',0,0,''),(104,'r_104','2026-10-14',0,0,''),(105,'r_105','2026-10-14',0,0,''),(106,'r_106','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES (101,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','Null original date','Summary',0.4),(102,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','Near medium','Summary',0.39999998),(103,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','Near medium high','Summary',0.40000004),(104,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','High boundary','Summary',0.7),(105,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','Near high','Summary',0.69999994),(106,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','Above high','Summary',0.70000005)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var sqlHigh, sqlMedium int
	if _, err := db.Exec(`INSERT INTO report_analysis VALUES(107,'en','floatboundary','Boundary',TRUE,'digital','2026-10-14','Zero original date','Summary',0.4)`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT SUM(severity_level>=0.7),SUM(severity_level>=0.4 AND severity_level<0.7) FROM report_analysis WHERE brand_name='floatboundary'`).Scan(&sqlHigh, &sqlMedium); err != nil {
		t.Fatal(err)
	}
	boundaries, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "floatboundary", Kind: "all", Sort: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	if boundaries.High != sqlHigh || boundaries.Medium != sqlMedium {
		t.Fatalf("stored FLOAT threshold counts differ from SQL: high=%d/%d medium=%d/%d", boundaries.High, sqlHigh, boundaries.Medium, sqlMedium)
	}
	if last := boundaries.Reports[len(boundaries.Reports)-1]; last.Report.Seq != 101 || last.Report.Timestamp.Year() != 1970 || last.Evidence.IngestedAt != nil {
		t.Fatal("NULL date must rank last and retain the public epoch fallback")
	}
	if zero := boundaries.Reports[len(boundaries.Reports)-2]; zero.Report.Seq != 107 || !zero.Report.Timestamp.IsZero() || zero.Evidence.IngestedAt != nil {
		t.Fatal("stored zero date must precede NULL and retain its existing public value")
	}
	// Inline image URIs must not become source identities or enter the compact
	// JSON normalizer. A nonHTTP higher-priority node still blocks fallback;
	// literal JSON/string null remains eligible for the original URL fallback.
	for _, statement := range []string{
		`INSERT INTO reports (seq,public_id,ts,latitude,longitude,image) VALUES(108,'r_108','2026-10-14',0,0,''),(109,'r_109','2026-10-14',0,0,''),(110,'r_110','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES(108,'en','inlineuritest','Inline',TRUE,'digital','2026-10-14','Inline image observation','Summary',0.5),(109,'en','inlineuritest','Inline',TRUE,'digital','2026-10-14','Blocked fallback','Summary',0.5),(110,'en','inlineuritest','Inline',TRUE,'digital','2026-10-14','Literal null fallback','Summary',0.5)`,
		`INSERT INTO wire_submissions_raw(report_seq,report_json,provenance_json,submitted_at,actor_kind,channel) VALUES(108,JSON_OBJECT('evidence_bundle',JSON_ARRAY(JSON_OBJECT('uri',CONCAT('data:image/png;base64,',REPEAT('A',8388609))))),'{}','2026-10-14','machine','wire'),(109,'{"digital_context":{"metadata":{"source_url":"data:placeholder"}},"evidence_bundle":[{"uri":"https://example.com/blocked"}]}','{}','2026-10-14','machine','wire'),(110,'{"digital_context":{"metadata":{"source_url":"null"}},"evidence_bundle":[{"uri":"https://example.com/allowed"}]}','{}','2026-10-14','machine','wire')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	inlinePage, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "inlineuritest", Kind: "all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range inlinePage.Reports {
		switch item.Report.Seq {
		case 108, 109:
			if item.Evidence.SourceURL != nil {
				t.Fatal("inline URI or blocked fallback became a source link")
			}
		case 110:
			if stringValue(item.Evidence.SourceURL) != "https://example.com/allowed" {
				t.Fatal("literal null source URL lost its original fallback")
			}
		}
	}
	var retainedURILength int
	if err := db.QueryRow(`SELECT CHAR_LENGTH(JSON_UNQUOTE(JSON_EXTRACT(report_json,'$.evidence_bundle[0].uri'))) FROM wire_submissions_raw WHERE report_seq=108`).Scan(&retainedURILength); err != nil || retainedURILength < 8388609 {
		t.Fatal("read projection changed retained original payload")
	}
	var projectedURILength int
	if err := db.QueryRow(`SELECT CHAR_LENGTH(JSON_UNQUOTE(evidence_uri)) FROM wire_evidence_projection WHERE report_seq=108`).Scan(&projectedURILength); err != nil || projectedURILength > 128 {
		t.Fatal("transactional URI projection copied oversized inline media")
	}

	for _, statement := range []string{
		`INSERT INTO reports(seq,public_id,ts,latitude,longitude,image) VALUES(121,'r_121','2026-10-14',0,0,''),(122,'r_122','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES(121,'en','inlineuritest','Wire',TRUE,'digital','2026-10-14','Latest Wire observation','Summary',0.5),(122,'en','inlineuritest','Wire',TRUE,'digital','2026-10-14','Tied Wire observation','Summary',0.5)`,
		`INSERT INTO wire_submissions_raw(submission_id,report_seq,report_json,provenance_json,submitted_at,actor_kind,channel) VALUES('wire-old',121,'{"digital_context":{"metadata":{"source_url":"https://example.com/older"}}}','{"generation_method":"human_submission"}','2026-10-14 08:00:00','human','web'),('wire-new',121,'{"digital_context":{"metadata":{"source_url":"https://example.com/newer"}}}','{"generation_method":"human_submission"}','2026-10-14 09:00:00','human','web'),('wire-tie-alpha',122,'{"digital_context":{"metadata":{"source_url":"https://example.com/tie","author_username":"tie-alpha"}}}','{"generation_method":"human_submission"}','2026-10-14 09:00:00','human','web'),('wire-tie-zeta',122,'{"digital_context":{"metadata":{"source_url":"https://example.com/tie","author_username":"tie-zeta"}}}','{"generation_method":"shared_submission"}','2026-10-14 09:00:00','human','web')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// The first pass must use only the existing secondary/implicit primary key,
	// even when the retained original JSON is much larger than the sort buffer.
	var wirePlan string
	if err := db.QueryRow(`EXPLAIN FORMAT=JSON SELECT ws.submission_id,ws.report_seq FROM `+evidenceIDsTable+` STRAIGHT_JOIN wire_submissions_raw ws ON ws.report_seq=ids.seq ORDER BY ws.submission_id`, evidenceIDsJSON([]int{108, 121, 122})).Scan(&wirePlan); err != nil {
		t.Fatal(err)
	}
	var wirePlanValue interface{}
	if err := json.Unmarshal([]byte(wirePlan), &wirePlanValue); err != nil {
		t.Fatal(err)
	}
	var coveredWirePlan func(interface{}) bool
	coveredWirePlan = func(value interface{}) bool {
		switch value := value.(type) {
		case map[string]interface{}:
			if value["table_name"] == "ws" && value["using_index"] == true && value["access_type"] == "ref" {
				return true
			}
			for _, child := range value {
				if coveredWirePlan(child) {
					return true
				}
			}
		case []interface{}:
			for _, child := range value {
				if coveredWirePlan(child) {
					return true
				}
			}
		}
		return false
	}
	if !coveredWirePlan(wirePlanValue) {
		t.Fatal("Wire singleton pass is not a covered secondary lookup")
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	wireSources := map[int]evidenceSourceNormalizationRow{108: {Seq: 108}, 121: {Seq: 121}, 122: {Seq: 122}}
	if err := loadEvidenceWireSources(context.Background(), tx, []int{108, 121, 122}, wireSources); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var newest struct {
		DigitalContext struct {
			Metadata struct {
				SourceURL string `json:"source_url"`
			} `json:"metadata"`
		} `json:"digital_context"`
	}
	if err := json.Unmarshal(wireSources[121].WireReport, &newest); err != nil || newest.DigitalContext.Metadata.SourceURL != "https://example.com/newer" {
		t.Fatal("multiple Wire rows did not select latest submission")
	}
	var tiedSource struct {
		DigitalContext struct {
			Metadata struct {
				Author string `json:"author_username"`
			} `json:"metadata"`
		} `json:"digital_context"`
	}
	var tiedProvenance struct {
		Method string `json:"generation_method"`
	}
	if json.Unmarshal(wireSources[122].WireReport, &tiedSource) != nil || json.Unmarshal(wireSources[122].WireProvenance, &tiedProvenance) != nil {
		t.Fatal("invalid tied Wire metadata")
	}
	if !((tiedSource.DigitalContext.Metadata.Author == "tie-alpha" && tiedProvenance.Method == "human_submission") || (tiedSource.DigitalContext.Metadata.Author == "tie-zeta" && tiedProvenance.Method == "shared_submission")) {
		t.Fatal("tied Wire metadata mixed different original submissions")
	}
	if len(wireSources[108].WireReport) > 4096 {
		t.Fatal("singleton oversized media entered compact metadata")
	}
	for _, statement := range []string{
		`INSERT INTO reports(seq,public_id,ts,latitude,longitude,image) VALUES(112,'r_112','2026-01-01',0,0,''),(113,'r_113','2026-02-01',0,0,''),(114,'r_114','2026-01-01',0,0,''),(115,'r_115','2026-02-01',0,0,''),(116,'r_116','2026-01-01',0,0,''),(117,'r_117','2026-02-01',0,0,''),(118,'r_118','2026-01-01',0,0,'')`,
		`INSERT INTO report_analysis VALUES(112,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','Known zero family','Summary',0),(113,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','NULL family','Summary',NULL),(114,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','Known negative family','Summary',-0.1),(115,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','NULL negative family','Summary',NULL),(116,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','Known zero singleton','Summary',0),(117,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','NULL singleton','Summary',NULL),(118,'en','nullseverity','Severity',TRUE,'digital','2026-10-14','Known negative singleton','Summary',-0.1),(NULL,'en','orphananalysis','Orphan',TRUE,'digital','2026-10-14','No report link','Summary',1),(9999,'en','orphananalysis','Orphan',TRUE,'digital','2026-10-14','Missing parent','Summary',1)`,
		`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) SELECT seq,'firsthand','I observed an issue','Workflow blocked','Own observation','affected',1 FROM report_analysis WHERE brand_name='nullseverity'`,
		`INSERT INTO case_reports VALUES(112,'severity_zero_family'),(113,'severity_zero_family'),(114,'severity_negative_family'),(115,'severity_negative_family')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	nullSeverity, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "nullseverity", Kind: "firsthand", Sort: "signal"})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{112, 114, 116, 118, 117} {
		if len(nullSeverity.Reports) != 5 || nullSeverity.Reports[i].Report.Seq != want {
			t.Fatalf("SQL NULL severity ordering[%d] differs", i)
		}
	}
	for _, statement := range []string{
		`INSERT INTO reports(seq,public_id,ts,latitude,longitude,image) VALUES(119,'r_119','2026-10-14',0,0,''),(120,'r_120','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES(119,'en','tiedanalysis','Low display',TRUE,'digital','2026-10-14','Low title','Low summary',0.2),(119,'en','tiedanalysis','High display',TRUE,'digital','2026-10-14','High title','High summary',0.9),(120,NULL,'tiedanalysis','Nullable display',TRUE,'digital',NULL,'Nullable title','Nullable summary',NULL),(120,NULL,'tiedanalysis','Zero display',TRUE,'digital',NULL,'Zero title','Zero summary',0)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	tied, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "tiedanalysis", Kind: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tied.Reports) != 2 {
		t.Fatal("tied analyses lost public reports")
	}
	for _, item := range tied.Reports {
		a := item.Analysis[0]
		var coherent int
		if err := db.QueryRow(`SELECT COUNT(*) FROM report_analysis WHERE seq=? AND COALESCE(language,'')=? AND brand_name=? AND classification=? AND COALESCE(CAST(severity_level AS DOUBLE),0)=? AND title=? AND summary=? AND brand_display_name=?`, item.Report.Seq, a.Language, a.BrandName, a.Classification, a.SeverityLevel, a.Title, a.Summary, a.BrandDisplayName).Scan(&coherent); err != nil || coherent < 1 {
			t.Fatal("tied analysis returned metadata and display fields from different rows")
		}
	}
	// The independent adapter and executable SQL reference share every public
	// field, facet, source/family count and page boundary across mixed scopes.
	for _, brand := range []string{"anthropic", "translationtest", "archivetimetest", "identitytest", "ranktest", "privacytest", "floatboundary", "inlineuritest", "nullseverity", "orphananalysis", "absentbrand"} {
		for _, kind := range []string{"firsthand", "coverage", "unknown", "all"} {
			for _, sortBy := range []string{"signal", "recent", "recurrence"} {
				for _, offset := range []int{0, 1, 100} {
					filter := EvidenceFilter{BrandName: brand, Kind: kind, Sort: sortBy, Offset: offset, Limit: 2, Language: "fr", asOf: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
					actual, err := d.GetEvidenceBrandPage(context.Background(), filter)
					if err != nil {
						t.Fatalf("bulk %s/%s/%s/%d: %v", brand, kind, sortBy, offset, err)
					}
					reference, err := d.getEvidenceBrandPageSQL(context.Background(), filter)
					if err != nil {
						t.Fatalf("SQL reference %s/%s/%s/%d: %v", brand, kind, sortBy, offset, err)
					}
					if !reflect.DeepEqual(actual, reference) {
						t.Fatalf("bulk/reference parity %s/%s/%s/%d differs: actual=%+v reference=%+v", brand, kind, sortBy, offset, actual, reference)
					}
					checkProjected(filter)
				}
			}
		}
	}
	// Run after the historical parity matrix so these new identities do not
	// change existing fixtures' expected totals or family representatives.
	t.Run("ExpandedCompanyAliases", func(t *testing.T) {
		seq := 2000
		expected := map[string]map[int]bool{}
		aliasSeq := map[string]int{}
		seed := func(brand string) int {
			t.Helper()
			seq++
			if _, err := db.Exec(`INSERT INTO reports(seq,public_id,ts,latitude,longitude,image) VALUES(?,?,'2026-10-15',0,0,'')`, seq, fmt.Sprintf("alias_%d", seq)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO report_analysis VALUES(?,'en',?, ?,TRUE,'digital','2026-10-15','Alias issue','Summary',0.5)`, seq, brand, brand); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO report_evidence(seq,report_kind,evidence_excerpt,reported_impact,classification_reason,entity_relationship,evidence_version) VALUES(?,'firsthand','I observed an issue','Blocked','Own observation','affected',1)`, seq); err != nil {
				t.Fatal(err)
			}
			return seq
		}
		for _, company := range []string{"anthropic", "openai", "xai"} {
			expected[company] = map[int]bool{}
			for _, alias := range expectedEvidenceCompanyAliases()[company] {
				id := seed(alias)
				expected[company][id], aliasSeq[alias] = true, id
			}
		}
		for _, unrelated := range []string{"x", "twitter", "x.com", "notopenai", "grok-news", "anthropic-partner"} {
			seed(unrelated)
		}
		// Alias expansion must still apply all existing publication guards.
		private, inactive, quarantined, invalid, physical := seed("claude.ai"), seed("openai_api"), seed("grok.com"), seed("claude_code"), seed("x.ai")
		for _, statement := range []string{
			fmt.Sprintf(`INSERT INTO reports_owners VALUES(%d,'private-owner',FALSE)`, private),
			fmt.Sprintf(`INSERT INTO report_status VALUES(%d,'resolved')`, inactive),
			fmt.Sprintf(`INSERT INTO report_raw VALUES(%d,'sensor','id','fetcher','quarantine')`, quarantined),
			fmt.Sprintf(`UPDATE report_analysis SET is_valid=FALSE WHERE seq=%d`, invalid),
			fmt.Sprintf(`UPDATE report_analysis SET classification='physical' WHERE seq=%d`, physical),
		} {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
		check := func(filter EvidenceFilter, want map[int]bool) {
			t.Helper()
			filter.Kind, filter.Sort, filter.Limit = "firsthand", "recent", 50
			filter.asOf = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
			page, err := d.GetEvidenceBrandPage(context.Background(), filter)
			if err != nil {
				t.Fatal(err)
			}
			got := map[int]bool{}
			for _, item := range page.Reports {
				if item.Report.Seq > 2000 {
					got[item.Report.Seq] = true
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("company/product alias query %s/%s: got %v want %v", filter.BrandName, filter.Product, got, want)
			}
			reference, err := d.getEvidenceBrandPageSQL(context.Background(), filter)
			if err != nil || !reflect.DeepEqual(page, reference) {
				t.Fatalf("alias scope adapter/SQL counts or privacy differ: %v", err)
			}
		}
		for _, company := range []string{"anthropic", "openai", "xai"} {
			check(EvidenceFilter{BrandName: company}, expected[company])
		}
		for _, product := range []struct {
			company, name string
			aliases       []string
		}{
			{"anthropic", "claude", []string{"claude", "claudeai", "claude.ai", "claude ai", "@claudeai"}},
			{"anthropic", "claude_code", []string{"claude-code", "claudecode", "claude_code", "claude code"}},
			{"openai", "openai_api", []string{"openaiapi", "openai-api", "openai_api", "openai api"}},
			{"openai", "chat gpt", []string{"chatgpt", "chat-gpt", "chat gpt"}},
			{"xai", "grok", []string{"grok", "grok.com"}},
		} {
			want := map[int]bool{}
			for _, alias := range product.aliases {
				want[aliasSeq[alias]] = true
			}
			check(EvidenceFilter{BrandName: product.company, Product: product.name}, want)
		}
	})
	t.Run("PublicBrandPreview", func(t *testing.T) { testPublicBrandPreviewMySQL(t, db) })
	t.Run("PublicDetailMixedMetadataCollations", func(t *testing.T) { testPublicDetailMixedMetadataCollationsMySQL(t, db) })
	t.Run("ProjectedMalformedPublicIDs", func(t *testing.T) { testProjectedEvidenceMalformedPublicIDsMySQL(t, db) })
	t.Run("ProjectedFamilyRanks", func(t *testing.T) { testProjectedEvidenceSlimRanksMySQL(t, db) })
	t.Run("ProjectedSourceWinnerIdentity", func(t *testing.T) { testProjectedEvidenceSourceWinnerIdentityMySQL(t, db) })
	t.Run("ProjectedPreferredAnalysisStream", func(t *testing.T) { testProjectedEvidencePreferredAnalysisStreamMySQL(t, db) })
	t.Run("ProjectedPublicationBlockerParity", func(t *testing.T) { testProjectedEvidencePublicationBlockerParityMySQL(t, db) })
	t.Run("ProjectedOptionalOwnerIndex", func(t *testing.T) { testProjectedEvidenceOptionalOwnerIndexMySQL(t, db) })
	t.Run("ProjectedLookupTablesUseHashJoins", func(t *testing.T) { testProjectedEvidenceHashJoinPlanMySQL(t, db) })
}

func evidencePagesEqualIgnoringClock(a, b EvidenceBrandPage) bool {
	a.Overview.AsOf = time.Time{}
	b.Overview.AsOf = time.Time{}
	return reflect.DeepEqual(a, b)
}
