package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"report-listener/models"
)

// This fixture reads real MySQL NULLs rather than mocking their SQL projection.
// It refuses app credentials, remote hosts, and databases with existing tables.
func TestHistoricalReaderMySQLFixture(t *testing.T) {
	dsn := os.Getenv("HISTORICAL_READER_TEST_DSN")
	if dsn == "" {
		t.Skip("set HISTORICAL_READER_TEST_DSN for an empty local database ending _historical_reader_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_historical_reader_test") {
		t.Fatal("fixture requires a dedicated local database ending _historical_reader_test")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var existing int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("fixture requires an empty database: tables=%d error=%v", existing, err)
	}
	created := []string{}
	defer func() {
		for i := len(created) - 1; i >= 0; i-- {
			_, _ = db.Exec("DROP TABLE " + created[i])
		}
	}()
	for _, fixture := range []struct{ name, ddl string }{
		{"reports", `CREATE TABLE reports (seq INT PRIMARY KEY,public_id VARCHAR(32),ts TIMESTAMP,id VARCHAR(255),team INT,latitude FLOAT,longitude FLOAT,x FLOAT,y FLOAT,image LONGBLOB,action_id VARCHAR(32),description TEXT,UNIQUE KEY(public_id))`},
		{"report_raw", `CREATE TABLE report_raw (report_seq INT PRIMARY KEY,visibility VARCHAR(32),agent_id VARCHAR(255),source_type VARCHAR(64),source_id VARCHAR(255),fetcher_id VARCHAR(64))`},
		{"report_status", `CREATE TABLE report_status (seq INT PRIMARY KEY,status VARCHAR(32))`},
		{"reports_owners", `CREATE TABLE reports_owners (seq INT,owner VARCHAR(255),is_public BOOL,KEY(seq))`},
		{"external_ingest_index", `CREATE TABLE external_ingest_index (seq INT,source_timestamp DATETIME,source_url VARCHAR(500),source VARCHAR(64),external_id VARCHAR(255),created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,KEY(seq))`},
		{"report_email_deliveries", `CREATE TABLE report_email_deliveries (seq INT,delivery_status VARCHAR(32),sent_at TIMESTAMP NULL,KEY(seq))`},
		{"report_side_effect_policy", `CREATE TABLE report_side_effect_policy (report_seq INT PRIMARY KEY,suppress_notifications BOOL NOT NULL,policy_version INT NOT NULL DEFAULT 1)`},
		{"reports_geometry", `CREATE TABLE reports_geometry (seq INT PRIMARY KEY,geom POINT NOT NULL SRID 4326,SPATIAL KEY(geom))`},
		{"report_metadata_projection", `CREATE TABLE report_metadata_projection (seq INT PRIMARY KEY,id VARCHAR(255),public_id VARCHAR(32),ts DATETIME,latitude DOUBLE,longitude DOUBLE,has_original_media BOOL)`},
		{"report_evidence", `CREATE TABLE report_evidence (seq INT PRIMARY KEY,report_kind VARCHAR(16) NOT NULL DEFAULT 'unknown',evidence_excerpt TEXT,reported_impact TEXT,classification_reason TEXT,entity_relationship VARCHAR(16) DEFAULT 'unknown',evidence_version INT DEFAULT 0)`},
		{"wire_submissions_raw", `CREATE TABLE wire_submissions_raw (report_seq INT,report_json JSON,provenance_json JSON,observed_at TIMESTAMP NULL,submitted_at TIMESTAMP,actor_kind VARCHAR(32),channel VARCHAR(32),extensions_json JSON,KEY(report_seq))`},
		{"case_reports", `CREATE TABLE case_reports (seq INT,case_id VARCHAR(64))`},
		{"report_analysis", `CREATE TABLE report_analysis (seq INT,source VARCHAR(255),analysis_text TEXT NULL,analysis_image LONGBLOB,title VARCHAR(500),description TEXT,brand_name VARCHAR(255) DEFAULT '',brand_display_name VARCHAR(255) DEFAULT '',litter_probability FLOAT DEFAULT NULL,hazard_probability FLOAT DEFAULT NULL,digital_bug_probability FLOAT DEFAULT 0,severity_level FLOAT,summary TEXT NULL,language VARCHAR(2) DEFAULT 'en',classification ENUM('physical','digital') DEFAULT 'physical',is_valid BOOL DEFAULT TRUE,inferred_contact_emails TEXT,created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,KEY(seq),KEY idx_ra_brand_valid_seq(brand_name,is_valid,seq),FULLTEXT KEY ft_report(title,description,brand_name,brand_display_name,summary))`},
	} {
		if _, err := db.ExecContext(ctx, fixture.ddl+" ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"); err != nil {
			t.Fatal(err)
		}
		created = append(created, fixture.name)
	}
	var photoBuffer bytes.Buffer
	if err := png.Encode(&photoBuffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	photo := photoBuffer.Bytes()
	original := time.Date(2023, 7, 4, 3, 52, 48, 0, time.UTC)
	for _, seq := range []int{1, 2} {
		publicID := "rpt_existing_assessed"
		if seq == 2 {
			publicID = "rpt_historical_null"
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO reports(seq,public_id,ts,id,team,latitude,longitude,x,y,image) VALUES(?,?,?,'historical-test-reporter',0,10,20,0,0,?)`, seq, publicID, original, photo); err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{
			`INSERT INTO report_raw(report_seq,visibility) VALUES(?,'public')`,
			`INSERT INTO external_ingest_index(seq,source_timestamp,source) VALUES(?,'2023-07-04 03:52:48','dataunion-0.1.0')`,
			`INSERT INTO reports_geometry(seq,geom) VALUES(?,ST_GeomFromGeoJSON('{"type":"Point","coordinates":[20,10]}',1,4326))`,
		} {
			if _, err := db.ExecContext(ctx, query, seq); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Omit the same nullable columns as the trusted historical importer. Unknown
	// values stay NULL in storage; scalar compatibility defaults exist only on reads.
	if _, err := db.ExecContext(ctx, `INSERT INTO report_analysis(seq,source,title,description,severity_level,inferred_contact_emails) VALUES(2,'dataunion-0.1.0-recovery','Historical CleanApp report','Original photo report submitted on 2023-07-04T03:52:48.191301Z.',0,'[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO report_analysis(seq,source,title,description,severity_level,inferred_contact_emails,analysis_text,summary,litter_probability,hazard_probability) VALUES(1,'existing-analysis','Historical assessed report','Existing description',0.5,'[]','private raw analysis','Existing summary',0.125,0.25)`); err != nil {
		t.Fatal(err)
	}
	database := &Database{db: db}
	assertReports := func(t *testing.T, reports []models.ReportWithAnalysis, want int) {
		t.Helper()
		if len(reports) != want {
			t.Fatalf("expected %d fixture rows, got %d", want, len(reports))
		}
		for _, row := range reports {
			if !row.Report.Timestamp.Equal(original) || row.Report.Latitude != 10 || row.Report.Longitude != 20 || len(row.Analysis) != 1 {
				t.Fatalf("original time/location/analysis changed: %+v", row)
			}
			a := row.Analysis[0]
			if row.Report.Seq == 2 {
				if a.AnalysisText != "" || a.Summary != "" || a.LitterProbability != 0 || a.HazardProbability != 0 || a.DigitalBugProbability != 0 || a.SeverityLevel != 0 || a.Title != "Historical CleanApp report" {
					t.Fatalf("metadata-only scalar compatibility failed: %+v", a)
				}
			} else if a.Summary != "Existing summary" || a.LitterProbability != 0.125 || a.HazardProbability != 0.25 || a.SeverityLevel != 0.5 {
				t.Fatalf("assessed values changed: %+v", a)
			}
			serialized, err := json.Marshal(row)
			if err != nil || bytes.Contains(serialized, []byte("private raw analysis")) || bytes.Contains(serialized, []byte("inferred_contact_emails")) || !bytes.Contains(serialized, []byte(`"timestamp":"2023-07-04T03:52:48Z"`)) {
				t.Fatalf("public API shape/time/private field filtering failed: %s error=%v", serialized, err)
			}
		}
	}
	readers := []struct {
		name string
		read func() ([]models.ReportWithAnalysis, error)
	}{
		{"public_id", func() ([]models.ReportWithAnalysis, error) {
			var rows []models.ReportWithAnalysis
			for _, id := range []string{"rpt_historical_null", "rpt_existing_assessed"} {
				r, err := database.GetReportByPublicID(ctx, id)
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(r.Report.Image, photo) || r.Report.SourceTimestamp == nil || !r.Report.SourceTimestamp.Equal(original) || r.Report.LastEmailSentAt != nil {
					t.Fatalf("detail photo/original time/email state changed: %+v", r.Report)
				}
				if err := database.AttachPublicEvidence(ctx, []*models.ReportWithAnalysis{r}); err != nil {
					return nil, err
				}
				if r.Evidence == nil || !r.Evidence.HasOriginalMedia || r.Evidence.SourcePublishedAt == nil || !r.Evidence.SourcePublishedAt.Equal(original) || !bytes.Equal(r.Report.Image, photo) {
					t.Fatalf("public detail evidence lost original photo/date: %+v", r)
				}
				rows = append(rows, *r)
			}
			return rows, nil
		}},
		{"seq", func() ([]models.ReportWithAnalysis, error) {
			var rows []models.ReportWithAnalysis
			for _, seq := range []int{1, 2} {
				r, err := database.GetReportBySeq(ctx, seq)
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(r.Report.Image, photo) {
					t.Fatal("detail photo changed")
				}
				rows = append(rows, *r)
			}
			return rows, nil
		}},
		{"seq_batch", func() ([]models.ReportWithAnalysis, error) { return database.GetReportsBySeqs(ctx, []int{1, 2}) }},
		{"since", func() ([]models.ReportWithAnalysis, error) { return database.GetReportsSince(ctx, 0) }},
		{"recent_full", func() ([]models.ReportWithAnalysis, error) {
			r, err := database.GetLastNAnalyzedReports(ctx, 10, "physical", true)
			if err != nil {
				return nil, err
			}
			return r.([]models.ReportWithAnalysis), nil
		}},
		{"search_full", func() ([]models.ReportWithAnalysis, error) {
			r, err := database.SearchReports(ctx, "Historical", "physical", true, 10)
			if err != nil {
				return nil, err
			}
			return r.([]models.ReportWithAnalysis), nil
		}},
		{"by_reporter", func() ([]models.ReportWithAnalysis, error) {
			return database.GetLastNReportsByID(ctx, "historical-test-reporter", 2)
		}},
		{"spatial", func() ([]models.ReportWithAnalysis, error) { return database.GetReportsByLatLng(ctx, 10, 20, 10, 10) }},
		{"geometry", func() ([]models.ReportWithAnalysis, error) {
			return database.GetReportsByGeometry(ctx, `{"type":"Polygon","coordinates":[[[19,9],[21,9],[21,11],[19,11],[19,9]]]}`, "physical", 10)
		}},
		{"brand", func() ([]models.ReportWithAnalysis, error) { return database.GetReportsByBrandName(ctx, "", 10) }},
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			rows, err := reader.read()
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			// The existing full-recent reader excludes its lowest selected seq.
			// Keep this fixture scoped to NULL compatibility and ensure its
			// remaining metadata-only row actually reaches the scalar scanner.
			if reader.name == "recent_full" {
				want = 1
				if len(rows) != 1 || rows[0].Report.Seq != 2 {
					t.Fatalf("historical row missing: %+v", rows)
				}
			}
			assertReports(t, rows, want)
		})
	}
	var nulls, digitalDefault int
	if err := db.QueryRowContext(ctx, `SELECT (analysis_text IS NULL)+(summary IS NULL)+(litter_probability IS NULL)+(hazard_probability IS NULL),digital_bug_probability FROM report_analysis WHERE seq=2`).Scan(&nulls, &digitalDefault); err != nil || nulls != 4 || digitalDefault != 0 {
		t.Fatalf("reader must preserve all stored unknowns and actual digital default: nulls=%d digital=%d error=%v", nulls, digitalDefault, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO report_side_effect_policy(report_seq,suppress_notifications) VALUES(2,TRUE)`); err != nil {
		t.Fatal(err)
	}
	quiet, err := database.GetReportsSince(ctx, 0)
	if err != nil || len(quiet) != 1 || quiet[0].Report.Seq != 1 {
		t.Fatalf("notification suppression must survive reader fix: %+v error=%v", quiet, err)
	}
}
