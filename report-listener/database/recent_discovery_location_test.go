package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"report-listener/models"

	"github.com/DATA-DOG/go-sqlmock"
)

func recentDiscoveryQueryMatcher(expected, actual string) error {
	if expected != "nonspatial recent selection" && expected != "legacy map selection" {
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	}
	query := strings.Join(strings.Fields(actual), " ")
	for _, required := range []string{"SELECT DISTINCT ra.seq", "ra.classification = ?", "ra.is_valid = TRUE", PublicVisibilityWhereSQL, "ORDER BY ra.seq DESC LIMIT ?"} {
		if !strings.Contains(query, required) {
			return fmt.Errorf("recent selection missing %q: %s", required, query)
		}
	}
	located := strings.Contains(query, "r.latitude BETWEEN -90 AND 90") && strings.Contains(query, "r.longitude BETWEEN -180 AND 180") && strings.Contains(query, "(r.latitude <> 0 OR r.longitude <> 0)")
	if expected == "legacy map selection" && !located {
		return fmt.Errorf("legacy map selection lost its location filter: %s", query)
	}
	if expected == "nonspatial recent selection" && (located || strings.Contains(query, "latitude") || strings.Contains(query, "longitude")) {
		return fmt.Errorf("nonspatial selection still excludes unknown location: %s", query)
	}
	return nil
}

func TestRecentPublicDiscoveryRetainsSingleUnlocatedPhysicalReport(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(recentDiscoveryQueryMatcher)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	added := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("nonspatial recent selection").WithArgs("physical", 1).
		WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(42))
	mock.ExpectQuery(`SELECT seq, public_id, ts, id, team, latitude, longitude, x, y, action_id, description FROM reports WHERE seq IN`).WithArgs(42).
		WillReturnRows(sqlmock.NewRows([]string{"seq", "public_id", "ts", "id", "team", "latitude", "longitude", "x", "y", "action_id", "description"}).
			AddRow(42, "r_unlocated", added, "human-web", 0, 0, 0, 0, 0, nil, "A blocked entrance"))
	mock.ExpectQuery(`SELECT ra.seq, ra.severity_level, ra.classification, ra.language, ra.title,.*WHERE ra.seq >= \?`).WithArgs(42).
		WillReturnRows(sqlmock.NewRows([]string{"seq", "severity_level", "classification", "language", "title", "summary", "brand_name", "brand_display_name"}).
			AddRow(42, 0.4, "physical", "en", "Entrance blocked", "A bin blocks the entrance.", "", ""))
	payload, err := (&Database{db: db}).GetRecentPublicDiscoveryReports(context.Background(), 1, "physical")
	if err != nil {
		t.Fatal(err)
	}
	reports, ok := payload.([]models.ReportWithMinimalAnalysis)
	if !ok || len(reports) != 1 || reports[0].Report.PublicID != "r_unlocated" || reports[0].Report.Latitude != 0 || reports[0].Report.Longitude != 0 || len(reports[0].Analysis) != 1 || reports[0].Analysis[0].Title != "Entrance blocked" {
		t.Fatalf("public observation lost or geolocated: %+v", payload)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRecentPhysicalReadKeepsMapLocationFilter(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(recentDiscoveryQueryMatcher)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("legacy map selection").WithArgs("physical", 12).
		WillReturnRows(sqlmock.NewRows([]string{"seq"}))
	if _, err := (&Database{db: db}).GetLastNAnalyzedReports(context.Background(), 12, "physical", false); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
