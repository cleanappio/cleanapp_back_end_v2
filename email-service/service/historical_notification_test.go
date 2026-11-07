package service

import (
	"context"
	"errors"
	"testing"

	"email-service/config"
	"email-service/models"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestHistoricalProcessingDoesNotSendOrForgeProcessedReceipt(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		query := mock.ExpectQuery(`SELECT policy_version, suppress_email FROM report_side_effect_policy`).WithArgs(int64(42))
		if unavailable {
			query.WillReturnError(errors.New("policy unavailable"))
		} else {
			query.WillReturnRows(sqlmock.NewRows([]string{"policy_version", "suppress_email"}).AddRow(1, true))
		}
		sender := &individualTestSender{}
		err = (&EmailService{db: db, email: sender, config: &config.Config{}}).processReport(context.Background(), models.Report{Seq: 42})
		if (err != nil) != unavailable || len(sender.to) != 0 {
			t.Fatalf("unavailable=%v err=%v sends=%v", unavailable, err, sender.to)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestHistoricalNotificationSelectorsMySQL(t *testing.T) {
	db := notificationMySQLFixture(t)
	ctx := context.Background()
	for _, query := range []string{
		`INSERT INTO reports(seq,public_id,ts) VALUES(101,'ordinary_physical','2023-07-04'),(102,'historical_physical','2023-07-04'),(103,'ordinary_digital','2023-07-04'),(104,'historical_digital','2023-07-04')`,
		`INSERT INTO report_analysis SELECT seq,'en',IF(seq<103,'','acme'),'','Metadata title','Metadata summary',IF(seq<103,'physical','digital'),'contact@company.org',TRUE FROM reports`,
		`INSERT INTO report_raw VALUES(102,'public'),(104,'public')`,
		`INSERT INTO report_evidence(seq,report_kind,entity_relationship,evidence_version) VALUES(103,'firsthand','affected',1),(104,'firsthand','affected',1)`,
		`INSERT INTO report_side_effect_policy(report_seq,source_namespace,reason) VALUES(102,'dataunion-0.1.0','historical import'),(104,'dataunion-0.1.0','historical import')`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	sender := &individualTestSender{}
	s := &EmailService{db: db, email: sender, config: &config.Config{}}
	all, err := s.getUnprocessedReports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ordinary candidate count=%d", len(all))
	}
	for _, r := range all {
		if r.Seq == 102 || r.Seq == 104 {
			t.Fatal("historical row selected for automatic email")
		}
	}
	physical, err := s.getUnprocessedBrandlessPhysicalReports(ctx, 5)
	if err != nil || len(physical) != 1 || physical[0].Seq != 101 {
		t.Fatalf("physical candidates=%v err=%v", physical, err)
	}
	brands, err := s.getUnprocessedReportsByBrand(ctx)
	if err != nil || len(brands) != 1 || brands[0].NewReportCount != 1 || brands[0].ReportSeqs[0] != 103 {
		t.Fatalf("brand candidates=%v err=%v", brands, err)
	}
	findings, err := s.getAggregateFindings(ctx, &models.BrandReportSummary{BrandName: "acme", ReportSeqs: []int64{104}})
	if err != nil || len(findings) != 0 {
		t.Fatalf("explicit historical final findings=%v err=%v", findings, err)
	}
	pending, err := s.pendingCompanyReports(ctx, []PlannedCompanyReport{{Seq: 103}, {Seq: 104}})
	if err != nil || len(pending) != 1 || pending[0].Seq != 103 {
		t.Fatalf("company pending=%v err=%v", pending, err)
	}
	if err := s.processReport(ctx, models.Report{Seq: 102}); err != nil {
		t.Fatal(err)
	}
	if len(sender.to) != 0 {
		t.Fatal("historical processing called email transport")
	}
	for _, table := range []string{"sent_reports_emails", "report_email_deliveries", "report_email_delivery_events"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("fake receipt in %s: count=%d err=%v", table, count, err)
		}
	}
}
