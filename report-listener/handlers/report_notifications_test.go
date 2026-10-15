package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"report-listener/models"
)

func TestNotificationHistoryServerSideSubscriptionGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, authorization string
		verifyErr           error
		active              bool
		billingErr          error
		want                int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "invalid token", authorization: "Bearer forged", verifyErr: errors.New("invalid signature"), want: http.StatusUnauthorized},
		{name: "non subscriber cannot spoof tier", authorization: "Bearer valid", want: http.StatusForbidden},
		{name: "billing failure fails closed", authorization: "Bearer valid", billingErr: errors.New("db unavailable"), want: http.StatusServiceUnavailable},
		{name: "verified active subscriber", authorization: "Bearer valid", active: true, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			verified, billingChecked := false, false
			router.GET("/history", func(c *gin.Context) {
				allowed := authorizeNotificationSubscriber(c, func(ctx context.Context, token string) (string, error) {
					verified = true
					if tc.verifyErr != nil {
						return "", tc.verifyErr
					}
					return "verified-subject", nil
				}, func(ctx context.Context, userID string) (bool, error) {
					billingChecked = true
					if userID != "verified-subject" {
						t.Fatalf("billing trusted request user_id: %s", userID)
					}
					return tc.active, tc.billingErr
				})
				if allowed {
					c.JSON(http.StatusOK, gin.H{"recipients": []string{"private@example.com"}})
				}
			})
			req := httptest.NewRequest(http.MethodGet, "/history?user_id=paid-user&subscription_tier=exclusive", nil)
			req.Header.Set("Authorization", tc.authorization)
			req.Header.Set("X-Subscription-Tier", "exclusive")
			req.AddCookie(&http.Cookie{Name: "is_subscriber", Value: "true"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want || response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("response %d %s cache=%s", response.Code, response.Body.String(), response.Header().Get("Cache-Control"))
			}
			if tc.want != http.StatusOK && strings.Contains(response.Body.String(), "private@example.com") {
				t.Fatal("private recipients escaped access gate")
			}
			if tc.authorization == "" && (verified || billingChecked) {
				t.Fatal("anonymous access should not query private records")
			}
			if tc.verifyErr != nil && billingChecked {
				t.Fatal("invalid token should not reach subscription lookup")
			}
		})
	}
}

func TestPublicContactProjectionStripsEmbeddedContactAndURLSecrets(t *testing.T) {
	if got := publicContactOrganization("xAI support@x.ai"); got != "xAI" {
		t.Fatalf("email remained in public organization: %q", got)
	}
	if got := publicContactWebsite("https://x.ai/contact?email=private@x.ai#secret"); got != "https://x.ai" {
		t.Fatalf("public domain leaked contact URL details: %q", got)
	}
	for _, value := range []string{"mailto:private@x.ai", "https://user:password@x.ai", "javascript:alert(1)"} {
		if got := publicContactWebsite(value); got != "" {
			t.Fatalf("unsafe public website: %q", got)
		}
	}
}

func TestPublicDetailDoesNotBypassSubscriberRecipientLog(t *testing.T) {
	report := &models.ReportWithAnalysis{
		Analysis:          []models.ReportAnalysis{{Title: "Visible finding", Summary: "Visible summary", InferredContactEmails: "private@x.ai", AnalysisText: `{"inferred_contact_emails":"private@x.ai","provider":"sendgrid"}`}},
		EscalationTargets: []models.CaseEscalationTarget{{Email: "private@x.ai"}},
		ExecutionTasks:    []models.NotifyExecutionTask{{}},
		NotifyOutcomes:    []models.NotifyOutcome{{}},
	}
	redactPublicReportContacts(report)
	result, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result), "private@x.ai") || strings.Contains(string(result), "sendgrid") || len(report.ExecutionTasks) > 0 || len(report.NotifyOutcomes) > 0 {
		t.Fatalf("public detail bypassed recipient gate: %s", result)
	}
	if report.Analysis[0].Title != "Visible finding" || report.Analysis[0].Summary != "Visible summary" {
		t.Fatal("public finding content was lost")
	}
}

func TestPublicFullBrandAndBatchResponsesCannotExposeRawContacts(t *testing.T) {
	// Evidence full_data and legacy list routes can serialize this type directly
	// without the single-detail redaction helper. Enforce privacy at its JSON
	// boundary so every reader route, including brand responses, is protected.
	report := models.ReportWithAnalysis{
		Report:   models.Report{Seq: 1280207, PublicID: "rpt_aUl-fseGsxzbWPAXbgKxGw"},
		Analysis: []models.ReportAnalysis{{Title: "Visible finding", Summary: "Visible summary", InferredContactEmails: "private@x.ai", AnalysisText: `{"inferred_contact_emails":"private@x.ai","provider":"sendgrid"}`}},
	}
	for _, envelope := range []interface{}{
		gin.H{"reports": []models.ReportWithAnalysis{report}, "count": 1},
		models.ReportBatch{Reports: []models.ReportWithAnalysis{report}, Count: 1},
		report,
	} {
		result, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"private@x.ai", "sendgrid", "inferred_contact_emails", "analysis_text"} {
			if strings.Contains(string(result), private) {
				t.Fatalf("public reader response exposed %q: %s", private, result)
			}
		}
		if !strings.Contains(string(result), "Visible finding") || !strings.Contains(string(result), "Visible summary") {
			t.Fatalf("structured report content disappeared: %s", result)
		}
	}
	if report.Analysis[0].InferredContactEmails == "" || report.Analysis[0].AnalysisText == "" {
		t.Fatal("private routing inputs must remain available in memory")
	}
}
