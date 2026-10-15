package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"cleanapp-common/mailtransport"
	"email-service/models"
)

func reportNotificationFixture() *models.BrandReportSummary {
	return &models.BrandReportSummary{BrandName: "grok", BrandDisplayName: "Grok", Classification: "digital", NewReportCount: 1, TotalReportCount: 301, ReportSeqs: []int64{1280207}, Reports: []models.BrandReportFinding{{Seq: 1280207, PublicID: "rpt_aUl-fseGsxzbWPAXbgKxGw", Classification: "digital", Title: "Outdated Model Version Reference on x.ai Landing Page", Summary: "The reporter observed that the x.ai landing page still described Grok 4.6 as its newest model after Grok 4.7 launched.", OriginalExcerpt: "grok 4.7 launched, yet 4.6 is still on the landing page.", ReportKind: "firsthand", ReceivedAt: time.Date(2026, 9, 21, 18, 31, 21, 0, time.UTC)}}}
}

func TestAggregateLeadsWithActualReportAndDirectEvidence(t *testing.T) {
	transport := &recordingWorkspace{}
	sender := workspaceEmailSender(transport)
	summary := reportNotificationFixture()
	if path := os.Getenv("EMAIL_NOTIFICATION_PREVIEW_IMAGE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		summary.Reports[0].Image = PrepareReportPreview(data)
	}
	if err := sender.SendAggregateEmail([]string{"support@company.com"}, summary, "https://cleanapp.io/opt-out"); err != nil {
		t.Fatal(err)
	}
	m := transport.messages[0]
	link := "https://www.cleanapp.io/digital/report/rpt_aUl-fseGsxzbWPAXbgKxGw"
	if !strings.HasPrefix(m.Subject, "Grok: Outdated Model Version") {
		t.Fatalf("subject hides finding: %s", m.Subject)
	}
	for _, body := range []string{m.Text, m.HTML} {
		for _, required := range []string{summary.Reports[0].Title, summary.Reports[0].OriginalExcerpt, link, "Sep 21, 2026", "Personal experience", "Unsubscribe"} {
			if !strings.Contains(body, required) {
				t.Fatalf("missing %q from notification", required)
			}
		}
	}
	// Optional local artifact generation uses a recording transport, never SMTP.
	if path := os.Getenv("EMAIL_NOTIFICATION_PREVIEW_PATH"); path != "" {
		preview := m.HTML
		for _, a := range m.Attachments {
			preview = strings.ReplaceAll(preview, "cid:"+a.ContentID, "data:"+a.ContentType+";base64,"+base64.StdEncoding.EncodeToString(a.Data))
		}
		if err := os.WriteFile(path, []byte(preview), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAggregateEscapesSourceTextAndEncodesUnsubscribe(t *testing.T) {
	summary := reportNotificationFixture()
	summary.BrandDisplayName = `ACME <script>alert(1)</script>`
	summary.Reports[0].Title = "Bad <img src=x onerror=alert(1)>\r\nBcc: victim@company.com"
	summary.Reports[0].OriginalExcerpt = `</blockquote><script>alert("x")</script>`
	m := workspaceEmailSender(&recordingWorkspace{}).aggregateMessage("a+b&c@company.com", summary, "https://cleanapp.io/opt-out?campaign=reports")
	if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, "<img src=x") || strings.Contains(m.HTML, "</blockquote><script>") {
		t.Fatal("report content became executable markup")
	}
	if !strings.Contains(m.HTML, "&lt;script&gt;") || !strings.Contains(m.HTML, "&lt;img") {
		t.Fatal("escaped source wording was lost")
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		t.Fatal("header injection permitted")
	}
	if !strings.Contains(m.Text, "campaign=reports&email=a%2Bb%26c%40company.com") {
		t.Fatalf("unsubscribe query was corrupted: %s", m.Text)
	}
	for _, id := range []string{"", "../../secret", "rpt_bad?email=x", `rpt_\"onclick=x`} {
		if ReportURL(models.BrandReportFinding{PublicID: id}) != "" {
			t.Fatalf("unsafe public ID accepted: %q", id)
		}
	}
}

func TestAggregateBoundsDetailedReportsLinksAndAttachments(t *testing.T) {
	summary := &models.BrandReportSummary{BrandName: "Acme", NewReportCount: 25, TotalReportCount: 100, Classification: "digital"}
	for i := 1; i <= 25; i++ {
		summary.Reports = append(summary.Reports, models.BrandReportFinding{Seq: int64(i), PublicID: fmt.Sprintf("rpt_%d", i), Title: fmt.Sprintf("Issue %d", i), OriginalExcerpt: fmt.Sprintf("Original quote %d", i), Classification: "digital", Image: []byte("already prepared preview")})
	}
	m := workspaceEmailSender(&recordingWorkspace{}).aggregateMessage("support@company.com", summary, "https://cleanapp.io/opt-out")
	if len(m.Attachments) != 3 {
		t.Fatalf("got %d previews", len(m.Attachments))
	}
	if strings.Count(m.HTML, "From the original report") != 3 {
		t.Fatal("detailed evidence is not capped at three")
	}
	if !strings.Contains(m.Text, "/report/rpt_20\n") || strings.Contains(m.Text, "/report/rpt_21\n") || !strings.Contains(m.Text, "5 more new reports") {
		t.Fatal("direct link cap/remaining count incorrect")
	}
	for i, a := range m.Attachments {
		if !a.Inline || a.ContentID != fmt.Sprintf("report_%d", i+1) || !strings.Contains(m.HTML, "cid:"+a.ContentID) {
			t.Fatal("preview not inline or image URL corrupted")
		}
	}
}

type partialAggregateTransport struct{ calls int }

func (s *partialAggregateTransport) Provider() string { return "google_workspace" }
func (s *partialAggregateTransport) Send(m mailtransport.Message) (string, error) {
	s.calls++
	if s.calls == 2 {
		return "", errors.New("rejected")
	}
	return "accepted-first", nil
}

func TestAggregateRetainsAcceptedResultWhenAnotherRecipientFails(t *testing.T) {
	transport := &partialAggregateTransport{}
	sender := workspaceEmailSender(&recordingWorkspace{})
	sender.workspace = transport
	r := sender.SendAggregateEmailsWithResults([]string{"a@company.com", "b@company.com"}, reportNotificationFixture(), "https://cleanapp.io/opt-out")
	if len(r) != 2 || r[0].Status != "sent" || r[0].ProviderMessageID != "accepted-first" || r[0].SentAt.IsZero() || r[1].Status != "failed" || r[1].ProviderMessageID != "" || !r[1].SentAt.IsZero() {
		t.Fatalf("incorrect recipient outcomes: %#v", r)
	}
}

func TestPrepareReportPreviewBoundsImagesAndKeepsUsefulOriginal(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1400, 1800))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	preview := PrepareReportPreview(data.Bytes())
	cfg, format, err := image.DecodeConfig(bytes.NewReader(preview))
	if err != nil || format != "jpeg" || cfg.Width > 680 || cfg.Height > 900 || len(preview) > 512<<10 {
		t.Fatalf("invalid preview bounds: %#v, %s, %v", cfg, format, err)
	}
	if PrepareReportPreview([]byte("invalid image")) != nil || PrepareReportPreview(make([]byte, MaxPreviewInputBytes+1)) != nil {
		t.Fatal("invalid/unbounded image accepted")
	}
}
