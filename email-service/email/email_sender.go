package email

import (
	"encoding/base64"
	"fmt"
	"image"
	"time"

	"email-service/config"
	"email-service/models"

	"github.com/apex/log"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	reportImgCid = "report_image"
	mapImgCid    = "map_image"
)

// EmailSender handles email sending functionality
type EmailSender struct {
	config *config.Config
	client *sendgrid.Client
}

// NewEmailSender creates a new email sender
func NewEmailSender(cfg *config.Config) *EmailSender {
	client := sendgrid.NewSendClient(cfg.SendGridAPIKey)
	return &EmailSender{
		config: cfg,
		client: client,
	}
}

// SendEmails sends emails to multiple recipients
func (e *EmailSender) SendEmails(recipients []string, reportImage, mapImage []byte) error {
	log.Infof("Sending email to %d recipients", len(recipients))

	var firstErr error
	failed := 0
	for _, recipient := range recipients {
		if err := e.sendOneEmail(recipient, reportImage, mapImage); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			log.Warnf("Error sending email to %s: %v", recipient, err)
			// Continue with other recipients
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d/%d emails failed: %v", failed, len(recipients), firstErr)
	}
	return nil
}

// SendEmailsWithAnalysis sends emails to multiple recipients with analysis data
func (e *EmailSender) SendEmailsWithAnalysis(recipients []string, reportImage, mapImage []byte, analysis *models.ReportAnalysis) error {
	log.Infof("Sending email with analysis to %d recipients", len(recipients))

	var firstErr error
	failed := 0
	for _, recipient := range recipients {
		if err := e.sendOneEmailWithAnalysis(recipient, reportImage, mapImage, analysis); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			log.Warnf("Error sending email to %s: %v", recipient, err)
			// Continue with other recipients
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d/%d emails with analysis failed: %v", failed, len(recipients), firstErr)
	}
	return nil
}

// sendOneEmail sends an email to a single recipient
func (e *EmailSender) sendOneEmail(recipient string, reportImage, mapImage []byte) error {
	from := mail.NewEmail(e.config.SendGridFromName, e.config.SendGridFromEmail)
	subject := "You got a CleanApp report"
	to := mail.NewEmail(recipient, recipient)

	hasReport := len(reportImage) > 0
	hasMap := len(mapImage) > 0

	// Create message
	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", e.getEmailText(recipient, hasReport, hasMap)))
	message.AddContent(mail.NewContent("text/html", e.getEmailHtml(recipient, hasReport, hasMap)))

	if hasReport {
		encodedReportImage := base64.StdEncoding.EncodeToString(reportImage)
		reportAttachment := mail.NewAttachment()
		reportAttachment.SetContent(encodedReportImage)
		reportAttachment.SetType("image/jpeg")
		reportAttachment.SetFilename("report.jpg")
		reportAttachment.SetDisposition("inline")
		reportAttachment.SetContentID(reportImgCid)
		message.AddAttachment(reportAttachment)
	}

	// Add map attachment only if mapImage is provided
	if hasMap {
		encodedMapImage := base64.StdEncoding.EncodeToString(mapImage)
		mapAttachment := mail.NewAttachment()
		mapAttachment.SetContent(encodedMapImage)
		mapAttachment.SetType("image/png")
		mapAttachment.SetFilename("map.png")
		mapAttachment.SetDisposition("inline")
		mapAttachment.SetContentID(mapImgCid)
		message.AddAttachment(mapAttachment)
	}

	// Send email
	start := time.Now()
	response, err := e.client.Send(message)
	if err != nil {
		return err
	}

	duration := time.Since(start)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		msgID := response.Headers["X-Message-Id"]
		log.Infof("Email accepted by SendGrid for %s (status=%d, id=%s, in %s)", recipient, response.StatusCode, msgID, duration)
		return nil
	}

	body := response.Body
	if len(body) > 512 {
		body = body[:512] + "..."
	}
	return fmt.Errorf("sendgrid returned status %d for %s (in %s): %s", response.StatusCode, recipient, duration, body)
}

// sendOneEmailWithAnalysis sends an email to a single recipient with analysis data
func (e *EmailSender) sendOneEmailWithAnalysis(recipient string, reportImage, mapImage []byte, analysis *models.ReportAnalysis) error {
	from := mail.NewEmail(e.config.SendGridFromName, e.config.SendGridFromEmail)

	// Create data-driven subject line: "Brand issue #N: Title"
	brandDisplay := analysis.BrandDisplayName
	if brandDisplay == "" {
		brandDisplay = analysis.BrandName
	}
	if brandDisplay == "" {
		brandDisplay = "Unknown"
	}
	
	// Truncate title to ~50 chars for subject line
	shortTitle := analysis.Title
	if len(shortTitle) > 50 {
		shortTitle = shortTitle[:47] + "..."
	}
	
	subject := fmt.Sprintf("%s issue #%d: %s", brandDisplay, analysis.BrandReportCount, shortTitle)

	to := mail.NewEmail(recipient, recipient)

	hasReport := len(reportImage) > 0
	hasMap := len(mapImage) > 0

	// Create message
	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", e.getEmailTextWithAnalysis(recipient, analysis, hasReport, hasMap)))
	message.AddContent(mail.NewContent("text/html", e.getEmailHtmlWithAnalysis(recipient, analysis, hasReport, hasMap)))

	if hasReport {
		encodedReportImage := base64.StdEncoding.EncodeToString(reportImage)
		reportAttachment := mail.NewAttachment()
		reportAttachment.SetContent(encodedReportImage)
		reportAttachment.SetType("image/jpeg")
		reportAttachment.SetFilename("report.jpg")
		reportAttachment.SetDisposition("inline")
		reportAttachment.SetContentID(reportImgCid)
		message.AddAttachment(reportAttachment)
	}

	// Add map attachment only if mapImage is provided
	if hasMap {
		encodedMapImage := base64.StdEncoding.EncodeToString(mapImage)
		mapAttachment := mail.NewAttachment()
		mapAttachment.SetContent(encodedMapImage)
		mapAttachment.SetType("image/png")
		mapAttachment.SetFilename("map.png")
		mapAttachment.SetDisposition("inline")
		mapAttachment.SetContentID(mapImgCid)
		message.AddAttachment(mapAttachment)
	}

	// Send email
	start := time.Now()
	response, err := e.client.Send(message)
	if err != nil {
		return err
	}

	duration := time.Since(start)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		msgID := response.Headers["X-Message-Id"]
		log.Infof("Email with analysis accepted by SendGrid for %s (status=%d, id=%s, in %s)", recipient, response.StatusCode, msgID, duration)
		return nil
	}
	body := response.Body
	if len(body) > 512 {
		body = body[:512] + "..."
	}
	return fmt.Errorf("sendgrid returned status %d for %s (in %s): %s", response.StatusCode, recipient, duration, body)
}

// addLabel adds text to an image
func (e *EmailSender) addLabel(img *image.RGBA, text string, x, y int) {
	point := fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}

	d := &font.Drawer{
		Dst:  img,
		Src:  image.Black,
		Face: basicfont.Face7x13,
		Dot:  point,
	}
	d.DrawString(text)
}

// getEmailText returns the plain text content for emails
func (e *EmailSender) getEmailText(recipient string, hasReport, hasMap bool) string {
	sections := ""
	if hasReport || hasMap {
		sections = "\nThis email contains:\n"
		if hasReport {
			sections += "- The report image\n"
		}
		if hasMap {
			sections += "- A map showing the location\n"
		}
	}
	return fmt.Sprintf(`Hello,

You have received a new CleanApp report.%s
Best regards,
The CleanApp Team`, sections)
}

// getEmailHtml returns the HTML content for emails
func (e *EmailSender) getEmailHtml(recipient string, hasReport, hasMap bool) string {
	imagesSection := ""
	if hasReport {
		imagesSection += fmt.Sprintf(`
    <h3>Report Image:</h3>
    <img src="cid:%s" alt="Report Image" style="max-width: 100%%; height: auto;">`, reportImgCid)
	}
	if hasMap {
		imagesSection += fmt.Sprintf(`
    <h3>Location Map:</h3>
    <img src="cid:%s" alt="Map" style="max-width: 100%%; height: auto;">`, mapImgCid)
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>CleanApp Report</title>
</head>
<body>
    <h2>Hello,</h2>
    <p>You have received a new CleanApp report.</p>%s
    <p>Best regards,<br>The CleanApp Team</p>
</body>
</html>`, imagesSection)
}

// getEmailTextWithAnalysis returns the plain text content for emails with analysis data
func (e *EmailSender) getEmailTextWithAnalysis(recipient string, analysis *models.ReportAnalysis, hasReport, hasMap bool) string {
	// Get brand display name
	brandDisplay := analysis.BrandDisplayName
	if brandDisplay == "" {
		brandDisplay = analysis.BrandName
	}
	if brandDisplay == "" {
		brandDisplay = "this product"
	}

	// Get the dashboard URL
	ctaURL := e.getDashboardURL(analysis)

	// Dynamic CTA text
	ctaText := fmt.Sprintf("View all %d reports about %s", analysis.BrandReportCount, brandDisplay)
	if analysis.BrandReportCount <= 1 {
		ctaText = fmt.Sprintf("View report about %s", brandDisplay)
	}

	// Get the AI-generated cost estimate or provide a default
	costEstimate := analysis.LegalRiskEstimate
	if costEstimate == "" {
		if analysis.Classification == "digital" {
			costEstimate = "Potential impact on user experience and brand reputation"
		} else {
			costEstimate = "Risk assessment pending - please review the report details"
		}
	}

	attachments := ""
	if hasReport || hasMap {
		attachments = "\nThis email contains:\n"
		if hasReport {
			attachments += "- The report image\n"
		}
		if hasMap {
			attachments += "- A map showing the location\n"
		}
	}

	legalRiskPercent := analysis.HazardProbability * 100

	content := fmt.Sprintf(`This is the #%d report CleanApp users have submitted about %s. Here's what they're seeing:

REPORT DETAILS:
Title: %s
Description: %s
Type: %s Issue

LEGAL RISK FACTOR: %.1f%%

ESTIMATED LIABILITY:
%s
%s
%s: %s

It takes just 30 seconds to review reports, confirm the risks, and get a fix.

---

Trash is cash,

Boris Mamlyuk
Founder, CleanApp.io
https://www.linkedin.com/in/borismamlyuk/

---

To unsubscribe from these emails, please visit: %s?email=%s
You can also reply to this email with "UNSUBSCRIBE" in the subject line.`,
		analysis.BrandReportCount,
		brandDisplay,
		analysis.Title,
		analysis.Description,
		analysis.Classification,
		legalRiskPercent,
		costEstimate,
		attachments,
		ctaText,
		ctaURL,
		e.config.OptOutURL,
		recipient)

	return content
}

// getEmailHtmlWithAnalysis returns the HTML content for emails with analysis data
func (e *EmailSender) getEmailHtmlWithAnalysis(recipient string, analysis *models.ReportAnalysis, hasReport, hasMap bool) string {
	// Calculate gauge colors based on values
	litterColor := e.getGaugeColor(analysis.LitterProbability)
	hazardColor := e.getGaugeColor(analysis.HazardProbability)
	severityColor := e.getSeverityGaugeColor(analysis.SeverityLevel)

	// Determine if this is a digital report
	isDigital := analysis.Classification == "digital"

	// Get brand display name
	brandDisplay := analysis.BrandDisplayName
	if brandDisplay == "" {
		brandDisplay = analysis.BrandName
	}
	if brandDisplay == "" {
		brandDisplay = "this product"
	}

	imagesSection := ""
	if hasReport {
		imagesSection += fmt.Sprintf(`
        <div class="image-container">
            <h3>Report Image:</h3>
            <img src="cid:%s" alt="Report Image" style="max-width: 100%%; height: auto; border-radius: 5px;">
        </div>`, reportImgCid)
	}
	if hasMap {
		imagesSection += fmt.Sprintf(`
        <div class="image-container">
            <h3>Location Map:</h3>
            <img src="cid:%s" alt="Map" style="max-width: 100%%; height: auto; border-radius: 5px;">
        </div>`, mapImgCid)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>%s issue #%d</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .header { background-color: #f8f9fa; padding: 20px; border-radius: 5px; margin-bottom: 20px; }
        .header h2 { margin: 0 0 10px 0; color: #333; }
        .header p { margin: 0; color: #555; font-size: 1.1em; }
        .report-count { font-weight: bold; color: #dc3545; }
        .brand-name { font-weight: bold; }
        .analysis-section { background-color: #e9ecef; padding: 15px; border-radius: 5px; margin: 15px 0; }
        .gauge-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 15px; margin: 20px 0; }
        .gauge-item { background-color: #fff; padding: 15px; border-radius: 8px; text-align: center; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .gauge-title { font-size: 0.9em; font-weight: bold; margin-bottom: 10px; color: #555; }
        .gauge-container { position: relative; width: 100%%; height: 60px; background: #f0f0f0; border-radius: 30px; overflow: hidden; margin: 10px 0; }
        .gauge-fill { height: 100%%; border-radius: 30px; transition: width 0.3s ease; position: relative; }
        .gauge-fill::after { content: ''; position: absolute; top: 2px; right: 2px; width: 8px; height: calc(100%% - 4px); background: rgba(255,255,255,0.3); border-radius: 4px; }
        .gauge-value { font-size: 1.3em; font-weight: bold; margin-top: 8px; }
        .gauge-label { font-size: 0.8em; color: #666; margin-top: 5px; }
        .images { margin: 20px 0; }
        .image-container { margin: 15px 0; }
        .low { background: linear-gradient(90deg, #28a745, #20c997); }
        .medium { background: linear-gradient(90deg, #ffc107, #fd7e14); }
        .high { background: linear-gradient(90deg, #dc3545, #e83e8c); }
        .digital-notice { background-color: #fff3cd; padding: 15px; border-radius: 5px; margin: 15px 0; border-left: 4px solid #ffc107; }
    </style>
</head>
<body>
    <div class="header">
        <h2>New Issue Reported</h2>
        <p>This is the <span class="report-count">#%d</span> report CleanApp users have submitted about <span class="brand-name">%s</span>. Here's what they're seeing:</p>
    </div>
    
    <div class="analysis-section">
        <h3>Report Details</h3>
        <p><strong>Title:</strong> %s</p>
        <p><strong>Description:</strong> %s</p>
        <p><strong>Type:</strong> %s</p>
    </div>
    
    %s
    
    <div class="images">%s
    </div>
    
    <div style="margin-top: 30px; padding: 20px 0; border-top: 1px solid #eee;">
        <p style="margin: 0; font-style: italic; color: #28a745;">Trash is cash,</p>
        <p style="margin: 10px 0 0 0; font-weight: bold; color: #333;">Boris Mamlyuk (<a href="https://www.linkedin.com/in/borismamlyuk/" style="color: #0077b5; text-decoration: none;">LinkedIn</a>)</p>
        <p style="margin: 0; color: #666;">Founder, <a href="https://cleanapp.io" style="color: #0077b5; text-decoration: none;">CleanApp.io</a></p>
        <p style="margin: 15px 0 0 0;"><img src="https://cleanapp.io/cleanapp-logo.png" alt="CleanApp" style="max-width: 150px; height: auto;"></p>
    </div>
    
    <div style="margin-top: 20px; padding-top: 15px; border-top: 1px solid #eee; font-size: 0.85em; color: #999;">
        <p>To unsubscribe from these emails, please <a href="%s?email=%s" style="color: #007bff; text-decoration: none;">click here</a></p>
    </div>
</body>
</html>`,
		brandDisplay,
		analysis.BrandReportCount,
		analysis.BrandReportCount,
		brandDisplay,
		analysis.Title,
		analysis.Description,
		analysis.Classification,
		e.getMetricsSection(analysis, isDigital, brandDisplay, litterColor, hazardColor, severityColor),
		imagesSection,
		e.config.OptOutURL,
		recipient)
}

// getMetricsSection returns the Legal Risk Factor section with AI cost estimate
func (e *EmailSender) getMetricsSection(analysis *models.ReportAnalysis, isDigital bool, brandDisplay, litterColor, hazardColor, severityColor string) string {
	// Get the Legal Risk Factor gauge (based on hazard probability)
	legalRiskColor := hazardColor
	legalRiskValue := analysis.HazardProbability * 100
	legalRiskLabel := e.getGaugeLabel(analysis.HazardProbability)

	// Get the AI-generated cost estimate or provide a default
	costEstimate := analysis.LegalRiskEstimate
	if costEstimate == "" {
		if isDigital {
			costEstimate = "Potential impact on user experience and brand reputation"
		} else {
			costEstimate = "Risk assessment pending - please review the report details"
		}
	}

	// Generate CTA button URL based on report type
	ctaURL := e.getDashboardURL(analysis)
	
	// Dynamic CTA text: "View all N reports about Brand"
	ctaText := fmt.Sprintf("View all %d reports about %s", analysis.BrandReportCount, brandDisplay)
	if analysis.BrandReportCount <= 1 {
		ctaText = fmt.Sprintf("View report about %s", brandDisplay)
	}

	return fmt.Sprintf(`
    <div style="margin: 20px 0;">
        <div style="background-color: #fff; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
            <div style="font-size: 0.9em; font-weight: bold; margin-bottom: 10px; color: #555;">Legal Risk Factor</div>
            <div style="position: relative; width: 100%%; height: 40px; background: #f0f0f0; border-radius: 20px; overflow: hidden; margin: 10px 0;">
                <div class="%s" style="height: 100%%; width: %.1f%%; border-radius: 20px;"></div>
            </div>
            <div style="display: flex; justify-content: space-between; align-items: center;">
                <div style="font-size: 1.5em; font-weight: bold;">%.1f%%</div>
                <div style="font-size: 0.9em; color: #666;">%s</div>
            </div>
        </div>
    </div>

    <div style="background-color: #fff3cd; padding: 15px; border-radius: 5px; margin: 15px 0; border-left: 4px solid #ffc107;">
        <p style="margin: 0; font-weight: bold; color: #856404;">💰 Estimated Liability</p>
        <p style="margin: 5px 0 0 0; color: #856404;">%s</p>
    </div>

    <div style="text-align: center; margin: 25px 0;">
        <a href="%s" style="display: inline-block; background-color: #28a745; color: white; padding: 15px 40px; text-decoration: none; border-radius: 5px; font-weight: bold; font-size: 1.1em;">%s</a>
        <p style="font-size: 0.85em; color: #666; margin-top: 10px;">It takes just 30 seconds to review reports, confirm the risks, and get a fix.</p>
    </div>`,
		legalRiskColor, legalRiskValue, legalRiskValue, legalRiskLabel,
		costEstimate,
		ctaURL, ctaText)
}

// getDashboardURL generates the appropriate dashboard URL based on report type
func (e *EmailSender) getDashboardURL(analysis *models.ReportAnalysis) string {
	baseURL := "https://cleanapp.io"

	if analysis.Classification == "digital" {
		// For digital reports, link to brand-specific dashboard
		brandSlug := analysis.BrandName
		if brandSlug == "" {
			brandSlug = "reports"
		}
		return fmt.Sprintf("%s/digital/%s", baseURL, brandSlug)
	}

	// For physical reports, link to the general reports dashboard
	// The location-based filtering would be handled by the dashboard itself
	return fmt.Sprintf("%s/reports", baseURL)
}

// getGaugeColor returns the CSS class for gauge color based on value
func (e *EmailSender) getGaugeColor(value float64) string {
	if value < 0.3 {
		return "low"
	} else if value < 0.7 {
		return "medium"
	} else {
		return "high"
	}
}

// getGaugeLabel returns a descriptive label based on the value
func (e *EmailSender) getGaugeLabel(value float64) string {
	if value < 0.3 {
		return "Low"
	} else if value < 0.7 {
		return "Medium"
	} else {
		return "High"
	}
}

// getSeverityGaugeColor returns the CSS class for severity gauge color based on 0-10 scale
func (e *EmailSender) getSeverityGaugeColor(value float64) string {
	if value < 3.0 {
		return "low"
	} else if value < 7.0 {
		return "medium"
	} else {
		return "high"
	}
}

// getSeverityGaugeLabel returns a descriptive label for severity based on 0-10 scale
func (e *EmailSender) getSeverityGaugeLabel(value float64) string {
	if value < 3.0 {
		return "Low"
	} else if value < 7.0 {
		return "Medium"
	} else {
		return "High"
	}
}
