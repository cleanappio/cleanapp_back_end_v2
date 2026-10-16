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
	Brand, Subject, DashboardURL, OptOutURL, Domain  string
	NewReportCount, TotalReportCount, RemainingCount int
	Reports                                          []aggregateReportView
}

// HasListed reports whether any linked reports follow the featured cards.
func (v aggregateView) HasListed() bool { return len(v.Reports) > FeaturedReportLimit }

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
	v := aggregateView{Brand: limitText(brand, 120), NewReportCount: summary.NewReportCount, TotalReportCount: summary.TotalReportCount, DashboardURL: e.getAggregateDashboardURL(summary), Domain: "Physical"}
	if summary.Classification == "digital" {
		v.Domain = "Digital"
	}
	if parsed, err := url.Parse(optOutURL); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") {
		q := parsed.Query()
		q.Set("email", recipient)
		parsed.RawQuery = q.Encode()
		v.OptOutURL = parsed.String()
	}
	for _, report := range summary.Reports {
		link := ReportURL(report)
		if link == "" {
			continue
		}
		domain := "Physical"
		if report.Classification == "digital" {
			domain = "Digital"
		}
		if v.Domain != domain {
			v.Domain = "Mixed"
		}
		if len(v.Reports) >= LinkedReportLimit {
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

// aggregateHTML is table-based with inline styles so it renders consistently in
// Gmail, Outlook and Apple Mail. html/template strips comments, so no MSO conditionals.
var aggregateHTML = template.Must(template.New("aggregate").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="x-apple-disable-message-reformatting">
<title>{{.Subject}}</title>
<style>
@media only screen and (max-width:620px){
  .wrap{width:100%!important;max-width:100%!important;border-radius:0!important}
  .pad{padding-left:20px!important;padding-right:20px!important}
  .cardpad{padding-left:18px!important;padding-right:18px!important}
  .stack{display:block!important;width:100%!important;box-sizing:border-box!important;padding:10px 0!important;border-right:0!important}
  .h1{font-size:23px!important;line-height:30px!important}
}
</style>
</head>
<body style="margin:0;padding:0;background:#eef2ee;-webkit-text-size-adjust:100%;-ms-text-size-adjust:100%">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;mso-hide:all">{{if .Reports}}{{(index .Reports 0).Title}} — {{end}}open the report and available evidence, no account needed.</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#eef2ee">
<tr><td align="center" style="padding:28px 12px">
<table role="presentation" class="wrap" width="600" cellpadding="0" cellspacing="0" border="0" style="width:600px;max-width:600px;background:#ffffff;border:1px solid #dde5dd;border-radius:14px;overflow:hidden">

<tr><td style="height:5px;background:#1f7a3f;font-size:0;line-height:0">&nbsp;</td></tr>

<tr><td class="pad" style="padding:22px 32px 0">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:19px;font-weight:800;letter-spacing:1.5px;color:#14532d;line-height:24px">CLEAN<span style="color:#7fb73c">APP</span></td>
<td align="right" style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:11px;font-weight:700;letter-spacing:1.5px;text-transform:uppercase;color:#6b7a6e;line-height:24px">Report notice</td>
</tr></table>
</td></tr>

<tr><td class="pad" style="padding:26px 32px 6px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<p style="margin:0 0 10px;font-size:12px;font-weight:700;letter-spacing:1.5px;text-transform:uppercase;color:#1f7a3f">{{.NewReportCount}} new {{if eq .NewReportCount 1}}report{{else}}reports{{end}} about {{.Brand}}</p>
<h1 class="h1" style="margin:0 0 12px;font-size:27px;line-height:34px;font-weight:800;letter-spacing:-0.3px;color:#111c15">{{if eq .NewReportCount 1}}Someone reported a problem with {{.Brand}}. We routed it to you.{{else}}{{.NewReportCount}} new reports about {{.Brand}}. We routed them to you.{{end}}</h1>
<p style="margin:0;font-size:15px;line-height:23px;color:#4b5a4f">CleanApp turns what people notice on streets, shelves and screens into structured reports, then sends each one to the party best placed to act. The original evidence is below. No account needed.</p>
</td></tr>

<tr><td class="pad" style="padding:18px 32px 24px">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border:1px solid #dde5dd;border-radius:10px;background:#f5f8f5"><tr>
<td class="stack" width="33%" align="center" valign="top" style="padding:14px 10px;border-right:1px solid #dde5dd;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"><div style="font-size:24px;line-height:28px;font-weight:800;color:#14532d">{{.NewReportCount}}</div><div style="font-size:11px;line-height:16px;font-weight:600;letter-spacing:0.8px;text-transform:uppercase;color:#6b7a6e">New reports</div></td>
<td class="stack" width="33%" align="center" valign="top" style="padding:14px 10px;border-right:1px solid #dde5dd;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"><div style="font-size:24px;line-height:28px;font-weight:800;color:#14532d">{{.TotalReportCount}}</div><div style="font-size:11px;line-height:16px;font-weight:600;letter-spacing:0.8px;text-transform:uppercase;color:#6b7a6e">Public reports</div></td>
<td class="stack" width="34%" align="center" valign="top" style="padding:14px 10px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"><div style="font-size:24px;line-height:28px;font-weight:800;color:#14532d">{{.Domain}}</div><div style="font-size:11px;line-height:16px;font-weight:600;letter-spacing:0.8px;text-transform:uppercase;color:#6b7a6e">Report type</div></td>
</tr></table>
</td></tr>

{{range $i, $r := .Reports}}{{if .Featured}}<tr><td class="pad" style="padding:0 32px 20px">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border:1px solid #dde5dd;border-radius:12px;background:#ffffff">
<tr><td class="cardpad" style="padding:22px 24px 6px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<p style="margin:0 0 12px;font-size:12px;line-height:18px;color:#6b7a6e"><span style="display:inline-block;padding:2px 9px;border-radius:999px;background:#e6f2df;color:#1f5f2e;font-weight:700;letter-spacing:0.3px">{{.Kind}}</span>{{if .Received}}&nbsp;&nbsp;Received {{.Received}}{{end}}</p>
<h2 style="margin:0 0 10px;font-size:20px;line-height:27px;font-weight:700;color:#111c15"><a href="{{.URL}}" style="color:#111c15;text-decoration:none">{{.Title}}</a></h2>
{{if .Summary}}<p style="margin:0 0 12px;font-size:15px;line-height:23px;color:#2f3a32">{{.Summary}}</p>{{end}}
{{if .ReportedImpact}}<p style="margin:0 0 12px;font-size:14px;line-height:21px;color:#2f3a32"><strong style="color:#9a3412">Reported impact:</strong> {{.ReportedImpact}}</p>{{end}}
</td></tr>
{{if .OriginalExcerpt}}<tr><td class="cardpad" style="padding:4px 24px 12px">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#f1f8ec;border-left:4px solid #7fb73c;border-radius:0 8px 8px 0"><tr><td style="padding:12px 16px 13px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<p style="margin:0 0 4px;font-size:11px;line-height:16px;font-weight:700;letter-spacing:1px;text-transform:uppercase;color:#2f6f3a">From the original report</p>
<p style="margin:0;font-size:15px;line-height:23px;font-style:italic;color:#14361f">&ldquo;{{.OriginalExcerpt}}&rdquo;</p>
</td></tr></table>
</td></tr>{{end}}
{{if .HasPreview}}<tr><td class="cardpad" style="padding:6px 24px 0"><a href="{{.URL}}" style="display:block;text-decoration:none"><img src="cid:report_{{.Seq}}" alt="Evidence attached to this report. Open the report for full size." width="100%" style="display:block;width:100%;height:auto;border:1px solid #dde5dd;border-radius:8px"></a></td></tr>{{end}}
<tr><td class="cardpad" style="padding:18px 24px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr><td bgcolor="#1f7a3f" style="border-radius:8px;background:#1f7a3f"><a href="{{.URL}}" style="display:inline-block;padding:12px 22px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;font-weight:700;line-height:18px;color:#ffffff;text-decoration:none;border-radius:8px">View report &amp; evidence &rarr;</a></td></tr></table>
{{if eq $i 0}}<p style="margin:10px 0 0;font-size:12px;line-height:17px;color:#6b7a6e">Open the full report for available evidence, location or URL, and related reports.</p>{{end}}
</td></tr>
</table>
</td></tr>{{end}}{{end}}

{{if .HasListed}}<tr><td class="pad" style="padding:0 32px 24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:1.5px;text-transform:uppercase;color:#6b7a6e">Also new</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">{{range .Reports}}{{if not .Featured}}<tr><td style="padding:9px 0;border-top:1px solid #e6ece6;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:20px"><a href="{{.URL}}" style="color:#1f5f2e;font-weight:600;text-decoration:none">{{.Title}}</a><span style="color:#8a968d;font-size:12px">&nbsp;&nbsp;{{.Kind}}{{if .Received}} &middot; {{.Received}}{{end}}</span></td></tr>{{end}}{{end}}</table>
</td></tr>{{end}}
{{if .RemainingCount}}<tr><td class="pad" style="padding:0 32px 24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:20px;color:#4b5a4f">{{.RemainingCount}} more new {{if eq .RemainingCount 1}}report is{{else}}reports are{{end}} waiting on the {{.Brand}} page.</td></tr>{{end}}

<tr><td class="pad" style="padding:26px 32px 28px;background:#14532d;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif">
<p style="margin:0 0 16px;font-size:11px;font-weight:700;letter-spacing:1.5px;text-transform:uppercase;color:#a3d47c">Why this reached you</p>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td class="stack" width="33%" valign="top" style="padding:0 14px 0 0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"><p style="margin:0 0 4px;font-size:15px;line-height:21px;font-weight:700;color:#ffffff">Noticed by people</p><p style="margin:0;font-size:13px;line-height:19px;color:#cfe3c4">Share a photo or screenshot, describe the problem, and add its location or URL. No sign-up.</p></td>
<td class="stack" width="33%" valign="top" style="padding:0 7px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"><p style="margin:0 0 4px;font-size:15px;line-height:21px;font-weight:700;color:#ffffff">Structured by AI</p><p style="margin:0;font-size:13px;line-height:19px;color:#cfe3c4">Each report is classified, summarized and linked to related reports, with the original wording kept intact.</p></td>
<td class="stack" width="34%" valign="top" style="padding:0 0 0 14px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"><p style="margin:0 0 4px;font-size:15px;line-height:21px;font-weight:700;color:#ffffff">Routed for action</p><p style="margin:0;font-size:13px;line-height:19px;color:#cfe3c4">We infer who may be able to act and send them the report, with a link to the public evidence.</p></td>
</tr></table>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin-top:20px"><tr><td style="border:1px solid #7fb73c;border-radius:8px"><a href="{{.DashboardURL}}" style="display:inline-block;padding:10px 18px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;font-weight:700;line-height:18px;color:#ffffff;text-decoration:none">See all reports about {{.Brand}} &rarr;</a></td></tr></table>
</td></tr>

<tr><td class="pad" style="padding:20px 32px 26px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:12px;line-height:18px;color:#6b7a6e">
<p style="margin:0 0 10px">Reports describe what people submitted or published; CleanApp has not independently reproduced these findings. A brand association identifies a likely subject and does not establish fault, ownership or legal responsibility.</p>
<p style="margin:0"><a href="https://www.cleanapp.io" style="color:#1f5f2e;text-decoration:none;font-weight:600">cleanapp.io</a> &middot; <a href="mailto:info@cleanapp.io" style="color:#1f5f2e;text-decoration:none">info@cleanapp.io</a>{{if .OptOutURL}} &middot; <a href="{{.OptOutURL}}" style="color:#6b7a6e;text-decoration:underline">Unsubscribe</a>{{end}}</p>
</td></tr>

</table>
</td></tr></table>
</body>
</html>`))

func (e *EmailSender) aggregateMessage(recipient string, summary *models.BrandReportSummary, optOutURL string) mailtransport.Message {
	v := e.aggregateView(recipient, summary, optOutURL)
	var body strings.Builder
	fmt.Fprintf(&body, "CLEANAPP · Report notice\n\n%d new report(s) about %s · %d public reports in total · %s\n\n", v.NewReportCount, v.Brand, v.TotalReportCount, v.Domain)
	body.WriteString("Someone reported a problem and CleanApp routed it to you. CleanApp turns what people notice on streets, shelves and screens into structured reports, then sends each one to the party best placed to act. The original evidence is linked below. No account needed.\n")
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
	fmt.Fprintf(&body, "\nSee all reports about %s: %s\n\nWhy this reached you: anyone can share a problem with no sign-up; each report is classified, summarized and linked to related reports with the original wording kept intact; CleanApp infers who may be able to act and sends them the report with a link to the public evidence.\n\nReports describe what people submitted or published; CleanApp has not independently reproduced these findings. A brand association identifies a likely subject and does not establish fault, ownership or legal responsibility.\n\ncleanapp.io · info@cleanapp.io\n", v.Brand, v.DashboardURL)
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
