package service

import (
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
)

func TestAnalyzeReportPublishedReplaySkipsAllProcessing(t *testing.T) {
	dsn := os.Getenv("REPORT_ANALYSIS_TEST_DSN")
	if dsn == "" {
		t.Skip("REPORT_ANALYSIS_TEST_DSN not set; requires a disposable MySQL database")
	}
	dsnConfig, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test database DSN")
	}
	if !strings.HasPrefix(dsnConfig.DBName, "cleanapp_analysis_test_") {
		t.Fatal("test database must have cleanapp_analysis_test_ prefix")
	}
	host, port, err := net.SplitHostPort(dsnConfig.Addr)
	if err != nil {
		t.Fatal("test DSN requires a TCP host and port")
	}
	db, err := database.NewDatabase(&config.Config{DBUser: dsnConfig.User, DBPassword: dsnConfig.Passwd, DBHost: host, DBPort: port, DBName: dsnConfig.DBName})
	if err != nil {
		t.Fatal("cannot reach disposable test database")
	}
	defer db.Close()
	if _, err := db.GetDB().Exec(`CREATE TABLE IF NOT EXISTS report_raw (report_seq INT PRIMARY KEY, visibility VARCHAR(16) NOT NULL DEFAULT 'public', analysed_published_at TIMESTAMP NULL)`); err != nil {
		t.Fatal(err)
	}
	const seq = 82001
	if _, err := db.GetDB().Exec(`INSERT INTO report_raw (report_seq, analysed_published_at) VALUES (?, '2026-10-07 09:00:00') ON DUPLICATE KEY UPDATE analysed_published_at='2026-10-07 09:00:00'`, seq); err != nil {
		t.Fatal(err)
	}
	defer db.GetDB().Exec(`DELETE FROM report_raw WHERE report_seq=?`, seq)
	// No config, LLM, publisher, or reports table is provided. Any processing
	// beyond the completed marker would fail instead of silently duplicating it.
	svc := &Service{db: db}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- svc.AnalyzeReport(&database.Report{Seq: seq})
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var published time.Time
	if err := db.GetDB().QueryRow(`SELECT analysed_published_at FROM report_raw WHERE report_seq=?`, seq).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published.UTC().Format("2006-01-02 15:04:05") != "2026-10-07 09:00:00" {
		t.Fatal("queued replay changed publication marker")
	}
}
