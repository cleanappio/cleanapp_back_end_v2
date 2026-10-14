package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"report-listener/models"

	"github.com/gin-gonic/gin"
)

func evidenceContext(query string) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v3/public/discovery/by-brand?"+query, nil)
	return c
}
func TestEvidenceBrandRequestDefaultsAndExplicitFilters(t *testing.T) {
	filter, err := parseEvidenceBrandFilter(evidenceContext("brand_name=claude"))
	if err != nil {
		t.Fatal(err)
	}
	if filter.Kind != "firsthand" || filter.Sort != "signal" || filter.Offset != 0 || filter.Limit != 12 {
		t.Fatalf("defaults=%+v", filter)
	}
	filter, err = parseEvidenceBrandFilter(evidenceContext("brand_name=anthropic&kind=unknown&sort=recent&offset=12&n=999&product=claude-code"))
	if err != nil {
		t.Fatal(err)
	}
	if filter.Kind != "unknown" || filter.Sort != "recent" || filter.Offset != 12 || filter.Limit != 50 || strings.Join(filter.Brands, ",") != "claude-code,claudecode" {
		t.Fatalf("explicit scope=%+v", filter)
	}
	for _, query := range []string{"brand_name=claude&kind=bogus", "brand_name=claude&sort=bogus", "brand_name=claude&offset=-1", "brand_name=claude&product=openai", "brand_name=claude&n=0"} {
		if _, err := parseEvidenceBrandFilter(evidenceContext(query)); err == nil {
			t.Fatalf("invalid scope accepted: %s", query)
		}
	}
}
func TestPublicDiscoveryCarriesEvidenceAndRelatedFamily(t *testing.T) {
	added := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	ev := &models.PublicEvidence{ReportKind: "firsthand", IngestedAt: added}
	rec := &models.PublicRecurrence{IssueID: "issue_1", Grouping: "related_issue_family", SupportingReportCount: 2, Independence: "unassessed"}
	rows := []models.ReportWithMinimalAnalysis{{Report: models.Report{PublicID: "r_test", Timestamp: added}, Analysis: []models.MinimalAnalysis{{Classification: "digital", Language: "en", Title: "Specific issue"}}, Evidence: ev, Recurrence: rec}}
	cards, err := toPublicDiscoveryCards(rows, "en", func(classification, id string) (string, error) { return "opaque-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].Evidence != ev || cards[0].Recurrence != rec || cards[0].DiscoveryToken != "opaque-token" {
		t.Fatalf("public proof lost: %+v", cards)
	}
}
