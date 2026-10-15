package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"report-listener/models"
)

func TestPublicDetailEvidenceFailureCannotSerializeRawReader(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v3/report?public_id=rpt_aUl-fseGsxzbWPAXbgKxGw", nil)
	report := &models.ReportWithAnalysis{Analysis: []models.ReportAnalysis{{Title: "private-raw-title"}}}
	ready := attachPublicDetailEvidence(c, report, func(_ context.Context, input []*models.ReportWithAnalysis) error {
		if len(input) != 1 || input[0] != report {
			t.Fatal("public projection lost its exact report")
		}
		return errors.New("private-database-error")
	})
	if ready {
		c.JSON(http.StatusOK, report)
	}
	if ready || response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private-") || response.Header().Get("Cache-Control") != "private, no-store, max-age=0" {
		t.Fatalf("privacy projection failure exposed or cached raw reader: %d %s", response.Code, response.Body.String())
	}
}
