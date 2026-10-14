package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
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
	statements := []string{
		`CREATE TABLE reports (seq INT PRIMARY KEY,public_id VARCHAR(64),ts DATETIME,latitude DOUBLE,longitude DOUBLE,image BLOB)`,
		`CREATE TABLE report_analysis (seq INT,language VARCHAR(8),brand_name VARCHAR(255),brand_display_name VARCHAR(255),is_valid BOOL,classification VARCHAR(32),created_at DATETIME,title TEXT,summary TEXT,severity_level DOUBLE,report_kind VARCHAR(16),evidence_excerpt TEXT,reported_impact TEXT,classification_reason TEXT,entity_relationship VARCHAR(16),evidence_version INT)`,
		`CREATE TABLE report_raw (report_seq INT PRIMARY KEY,source_type VARCHAR(64),source_id VARCHAR(255),fetcher_id VARCHAR(64),visibility VARCHAR(16))`,
		`CREATE TABLE external_ingest_index (seq INT,source VARCHAR(64),external_id VARCHAR(255),source_url VARCHAR(512),source_timestamp DATETIME)`,
		`CREATE TABLE wire_submissions_raw (report_seq INT,report_json JSON,provenance_json JSON,observed_at DATETIME,submitted_at DATETIME,actor_kind VARCHAR(32),channel VARCHAR(32),extensions_json JSON)`,
		`CREATE TABLE report_status (seq INT,status VARCHAR(32))`,
		`CREATE TABLE reports_owners (seq INT,owner VARCHAR(64),is_public BOOL)`,
		`CREATE TABLE case_reports (seq INT,case_id VARCHAR(64))`,
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
	fixtures := []string{
		`INSERT INTO reports VALUES (1,'r_1','2026-10-14',0,0,''),(2,'r_2','2026-10-14',0,0,''),(3,'r_3','2026-10-14',0,0,''),(4,'r_4','2026-10-14',0,0,''),(5,'r_5','2026-10-14',0,0,''),(6,'r_6','2026-10-14',0,0,''),(7,'r_7','2026-10-14',0,0,''),(8,'r_8','2026-10-14',0,0,'')`,
		`INSERT INTO report_analysis VALUES
  (1,'en','claude','Claude',TRUE,'digital','2026-10-14','Failure one','AI summary',0.7,'firsthand','I see failure one','Workflow blocked','Original describes own issue','affected',1),
  (1,'fr','claude','Claude',TRUE,'digital','2026-10-14','Echec','Resume',0.7,'firsthand','I see failure one','Workflow blocked','Original describes own issue','affected',1),
  (2,'en','claudecode','Claude Code',TRUE,'digital','2026-10-14','Failure two','AI summary',0.8,'firsthand','I see failure two','Workflow blocked','Original describes own issue','affected',1),
  (3,'en','claude','Claude',TRUE,'digital','2026-10-14','Coverage','AI summary',0.9,'coverage','A news report','', 'Journalism','mentioned',1),
  (4,'en','claude','Claude',TRUE,'digital','2026-10-14','Unclassified','AI summary',0.9,'firsthand','', '', '', 'unknown',0),
  (5,'en','claude','Claude',TRUE,'digital','2026-10-14','Duplicate one','AI summary',0.7,'firsthand','I see failure one','Workflow blocked','Original describes own issue','affected',1),
  (6,'en','claude','Claude',TRUE,'digital','2026-10-14','Quarantined','AI summary',0.9,'firsthand','I see failure six','','Own issue','affected',1),
  (7,'en','claude','Claude',TRUE,'digital','2026-10-14','Private','AI summary',0.9,'firsthand','I see failure seven','','Own issue','affected',1),
 (8,'en','claude','Claude',TRUE,'digital','2026-10-14','Inactive','AI summary',0.9,'firsthand','I see failure eight','','Own issue','affected',1)`,
		`INSERT INTO external_ingest_index VALUES (1,'twitter','10','https://twitter.com/person/status/10?utm_source=share','2026-02-01'),(5,'twitter','10','https://x.com/person/status/10/','2026-02-01'),(2,'github_issue','20','https://github.com/org/repo/issues/20','2026-03-01')`,
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
	d := &Database{db: db}
	page, err := d.GetEvidenceBrandPage(context.Background(), EvidenceFilter{BrandName: "anthropic", Kind: "firsthand"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Reports) != 1 || page.KindCounts.Firsthand != 1 || page.KindCounts.Coverage != 1 || page.KindCounts.Unknown != 1 || page.KindCounts.All != 3 {
		t.Fatalf("public family/filter counts wrong: %+v", page)
	}
	rec := page.Reports[0].Recurrence
	if rec.SupportingReportCount != 2 || rec.DuplicateImportCount != 1 || rec.SourceAccountCount == nil || *rec.SourceAccountCount != 2 {
		t.Fatalf("canonical sources/accounts inflated: %+v", rec)
	}
	if page.Reports[0].Evidence.ObservedAt == nil || page.Reports[0].Evidence.ObservedAt.Day() != 28 || page.Reports[0].Evidence.SourcePublishedAt.Month() != 3 {
		t.Fatal("genuine original observation and later publication conflated")
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
	if product.Total != 1 || product.Reports[0].Recurrence.SupportingReportCount != 1 {
		t.Fatal("product filter counts included another product")
	}
}
