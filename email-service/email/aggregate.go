package email

import (
	"bytes"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/url"
	"regexp"
	"strings"
	"time"

	"cleanapp-common/mailtransport"
	"email-service/models"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const FeaturedReportLimit = 3
const LinkedReportLimit = 20
const MaxPreviewInputBytes = 4 << 20

var publicReportID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ReportURL never falls back to sequential IDs or untrusted source URLs.
func ReportURL(report models.BrandReportFinding) string {
	if !publicReportID.MatchString(report.PublicID) {
		return ""
	}
	domain := "physical"
	if report.Classification == "digital" {
		domain = "digital"
	}
	return "https://www.cleanapp.io/" + domain + "/report/" + url.PathEscape(report.PublicID)
}

// PrepareReportPreview bounds compressed input, decoded pixels and attachment size.
// Invalid or oversized images are omitted; the direct report link is always present.
func PrepareReportPreview(data []byte) []byte {
	if len(data) == 0 || len(data) > MaxPreviewInputBytes {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	w, h := cfg.Width, cfg.Height
	if w > 680 {
		h = h * 680 / w
		w = 680
	}
	if h > 900 {
		w = w * 900 / h
		h = 900
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	preview := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(preview, preview.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(preview, preview.Bounds(), img, img.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, preview, &jpeg.Options{Quality: 82}); err != nil || out.Len() > 512<<10 {
		return nil
	}
	return out.Bytes()
}

type aggregateReportView struct {
	Seq                                                                  int64
	Title, Summary, OriginalExcerpt, ReportedImpact, Kind, Received, URL string
	Featured, HasPreview                                                 bool
}
type aggregateView struct {
	Brand, Subject, DashboardURL, OptOutURL          string
	NewReportCount, TotalReportCount, RemainingCount int
	Reports                                          []aggregateReportView
}

func limitText(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > max {
		return string(runes[:max-1]) + "…"
	}
	return string(runes)
}

func (e *EmailSender) aggregateView(recipient string, summary *models.BrandReportSummary, optOutURL string) aggregateView {
	brand := strings.TrimSpace(summary.BrandDisplayName)
	if brand == "" {
		brand = summary.BrandName
	}
	v := aggregateView{Brand: limitText(brand, 120), NewReportCount: summary.NewReportCount, TotalReportCount: summary.TotalReportCount, DashboardURL: e.getAggregateDashboardURL(summary)}
	if parsed, err := url.Parse(optOutURL); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") {
		q := parsed.Query()
		q.Set("email", recipient)
		parsed.RawQuery = q.Encode()
		v.OptOutURL = parsed.String()
	}
	for _, report := range summary.Reports {
		link := ReportURL(report)
		if link == "" || len(v.Reports) >= LinkedReportLimit {
			continue
		}
		kind := "Report"
		if report.ReportKind == "firsthand" {
			kind = "Personal experience"
		} else if report.ReportKind == "coverage" {
			kind = "News & discussion"
		}
		title := limitText(report.Title, 180)
		if title == "" {
			title = "Reported issue"
		}
		r := aggregateReportView{Seq: report.Seq, Title: title, Summary: limitText(report.Summary, 500), OriginalExcerpt: limitText(report.OriginalExcerpt, 600), ReportedImpact: limitText(report.ReportedImpact, 300), Kind: kind, URL: link, Featured: len(v.Reports) < FeaturedReportLimit}
		if !report.ReceivedAt.IsZero() {
			r.Received = report.ReceivedAt.UTC().Format("Jan 2, 2006")
		}
		r.HasPreview = r.Featured && len(report.Image) > 0
		v.Reports = append(v.Reports, r)
	}
	v.RemainingCount = summary.NewReportCount - len(v.Reports)
	if v.RemainingCount < 0 {
		v.RemainingCount = 0
	}
	v.Subject = fmt.Sprintf("%d new report", summary.NewReportCount)
	if summary.NewReportCount != 1 {
		v.Subject += "s"
	}
	v.Subject += fmt.Sprintf(" about %s (%d total)", v.Brand, summary.TotalReportCount)
	if len(v.Reports) > 0 {
		v.Subject = limitText(v.Brand+": "+v.Reports[0].Title, 150)
		if summary.NewReportCount > 1 {
			v.Subject += fmt.Sprintf(" (+%d more)", summary.NewReportCount-1)
		}
	}
	// Subjects are plain text header values, never report-controlled new headers.
	v.Subject = strings.Join(strings.Fields(v.Subject), " ")
	return v
}

var aggregateHTML = template.Must(template.New("aggregate").Parse(`<!doctype html><html><head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="margin:0;background:#f5f7f5;font-family:Arial,sans-serif;color:#18241c;line-height:1.5"><main style="max-width:680px;margin:0 auto;padding:28px 20px;background:white">
<p style="color:#167139;font-weight:bold">CleanApp</p><h1 style="font-size:25px;margin-bottom:8px">New reports about {{.Brand}}</h1>
<p>{{.NewReportCount}} new {{if eq .NewReportCount 1}}report{{else}}reports{{end}} · {{.TotalReportCount}} total in CleanApp</p>
{{range .Reports}}{{if .Featured}}<section style="padding:24px 0;border-top:1px solid #dfe7df"><p style="font-size:13px;color:#516156">{{.Kind}}{{if .Received}} · Received {{.Received}}{{end}}</p>
<h2 style="font-size:21px;margin:8px 0"><a href="{{.URL}}" style="color:#145c30;text-decoration:none">{{.Title}}</a></h2>
{{if .Summary}}<p>{{.Summary}}</p>{{end}}{{if .ReportedImpact}}<p><strong>Reported impact:</strong> {{.ReportedImpact}}</p>{{end}}
{{if .OriginalExcerpt}}<blockquote style="margin:16px 0;padding:14px 18px;background:#eff9f1;border-left:3px solid #22a14b"><strong>From the original report</strong><br>“{{.OriginalExcerpt}}”</blockquote>{{end}}
{{if .HasPreview}}<a href="{{.URL}}"><img src="cid:report_{{.Seq}}" alt="Report image — open report for full size" style="max-width:100%;height:auto;border:1px solid #dfe7df"></a>{{end}}
<p><a href="{{.URL}}" style="color:#145c30;font-weight:bold">View report &amp; evidence →</a></p></section>{{else}}<p><a href="{{.URL}}" style="color:#145c30">{{.Title}}</a></p>{{end}}{{end}}
{{if .RemainingCount}}<p>{{.RemainingCount}} more new {{if eq .RemainingCount 1}}report{{else}}reports{{end}} on the brand page.</p>{{end}}
<p><a href="{{.DashboardURL}}" style="color:#145c30">See all reports about {{.Brand}} →</a></p>
<p style="font-size:13px;color:#516156">Reports describe what people submitted or published. CleanApp has not independently reproduced these findings.</p>
<footer style="margin-top:28px;padding-top:16px;border-top:1px solid #dfe7df;font-size:12px;color:#516156">The CleanApp team{{if .OptOutURL}} · <a href="{{.OptOutURL}}">Unsubscribe</a>{{end}}</footer>
</main></body></html>`))

func (e *EmailSender) aggregateMessage(recipient string, summary *models.BrandReportSummary, optOutURL string) mailtransport.Message {
	v := e.aggregateView(recipient, summary, optOutURL)
	var body strings.Builder
	fmt.Fprintf(&body, "%d new report(s) about %s · %d total in CleanApp.\n", v.NewReportCount, v.Brand, v.TotalReportCount)
	for _, r := range v.Reports {
		fmt.Fprintf(&body, "\n%s\n%s", r.Title, r.Kind)
		if r.Received != "" {
			fmt.Fprintf(&body, " · Received %s", r.Received)
		}
		body.WriteString("\n")
		if r.Featured {
			if r.Summary != "" {
				fmt.Fprintln(&body, r.Summary)
			}
			if r.ReportedImpact != "" {
				fmt.Fprintf(&body, "Reported impact: %s\n", r.ReportedImpact)
			}
			if r.OriginalExcerpt != "" {
				fmt.Fprintf(&body, "From the original report: “%s”\n", r.OriginalExcerpt)
			}
		}
		fmt.Fprintf(&body, "View report & evidence: %s\n", r.URL)
	}
	if v.RemainingCount > 0 {
		fmt.Fprintf(&body, "\n%d more new reports on the brand page.\n", v.RemainingCount)
	}
	fmt.Fprintf(&body, "\nSee all reports: %s\n\nReports describe what people submitted or published. CleanApp has not independently reproduced these findings.\n\nThe CleanApp team\n", v.DashboardURL)
	if v.OptOutURL != "" {
		fmt.Fprintf(&body, "Unsubscribe: %s\n", v.OptOutURL)
	}
	var html bytes.Buffer
	// The template is static and all values are strings, numbers or booleans.
	_ = aggregateHTML.Execute(&html, v)
	message := e.message(recipient, v.Subject, body.String(), html.String())
	for _, r := range summary.Reports {
		for _, view := range v.Reports {
			if view.Seq == r.Seq && view.HasPreview {
				message.Attachments = append(message.Attachments, mailtransport.Attachment{Filename: fmt.Sprintf("report-%d.jpg", r.Seq), ContentType: "image/jpeg", ContentID: fmt.Sprintf("report_%d", r.Seq), Data: r.Image, Inline: true})
			}
		}
	}
	return message
}

// SendAggregateEmailsWithResults preserves each accepted send, even if another
// recipient fails. A batch error must never erase successful delivery evidence.
func (e *EmailSender) SendAggregateEmailsWithResults(recipients []string, summary *models.BrandReportSummary, optOutURL string) []CustomEmailSendResult {
	results := make([]CustomEmailSendResult, 0, len(recipients))
	for _, recipient := range recipients {
		id, err := e.sendMessage(e.aggregateMessage(recipient, summary, optOutURL))
		r := CustomEmailSendResult{Email: recipient, Provider: e.Provider()}
		if err != nil {
			r.Status = "failed"
			r.Error = err.Error()
		} else {
			r.Status = "sent"
			r.ProviderMessageID = id
			r.SentAt = time.Now().UTC()
		}
		results = append(results, r)
	}
	return results
}

func (e *EmailSender) SendAggregateEmail(recipients []string, summary *models.BrandReportSummary, optOutURL string) error {
	failed := 0
	for _, r := range e.SendAggregateEmailsWithResults(recipients, summary, optOutURL) {
		if r.Status != "sent" {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d aggregate emails failed", failed, len(recipients))
	}
	return nil
}
