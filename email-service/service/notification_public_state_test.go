package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNotificationOwnerGuardStaysCorrelated(t *testing.T) {
	for _, alias := range []string{"r", "ra", "ra2"} {
		query := notificationPublicStateSQL(alias)
		expected := "(SELECT /*+ JOIN_INDEX(nowner idx_is_public) */ COUNT(*) FROM reports_owners nowner WHERE nowner.seq=" + alias + ".seq AND nowner.owner IS NOT NULL AND nowner.owner<>'' AND nowner.is_public=FALSE)=0"
		if !strings.Contains(query, expected) || strings.Contains(query, "NOT EXISTS (SELECT 1 FROM reports_owners") {
			t.Fatal("owner guard permits whole-owner-table antijoin materialization")
		}
		if !strings.Contains(query, "NOT EXISTS (SELECT 1 FROM report_status ns WHERE ns.seq="+alias+".seq AND ns.status IS NOT NULL AND ns.status<>'active')") {
			t.Fatal("status publication policy changed")
		}
	}
}

func TestBrandSummaryUsesOwnerGuardForPendingAndTotalReports(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(_ string, actual string) error {
		if !strings.Contains(actual, notificationPublicStateSQL("ra2")) || !strings.Contains(actual, notificationPublicStateSQL("r")) || strings.Count(actual, "JOIN_INDEX(nowner idx_is_public)") != 2 {
			return fmt.Errorf("brand summary must guard pending and total scopes")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("brand summary").WillReturnRows(sqlmock.NewRows([]string{"brand_name", "brand_display_name", "new_count", "total_count", "seqs", "latest_seq", "classification", "inferred_contact_emails"}))
	if _, err := (&EmailService{db: db}).getUnprocessedReportsByBrand(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
