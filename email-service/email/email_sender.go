package email

import (
	"encoding/base64"
	"fmt"
	"image"

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

	for _, recipient := range recipients {
		if err := e.sendOneEmail(recipient, reportImage, mapImage); err != nil {
			log.Warnf("Error sending email to %s: %v", recipient, err)
			// Continue with other recipients
		}
	}

	return nil
}

// SendEmailsWithAnalysis sends emails to multiple recipients with analysis data
func (e *EmailSender) SendEmailsWithAnalysis(recipients []string, reportImage, mapImage []byte, analysis *models.ReportAnalysis) error {
	log.Infof("Sending email with analysis to %d recipients", len(recipients))

	for _, recipient := range recipients {
		if err := e.sendOneEmailWithAnalysis(recipient, reportImage, mapImage, analysis); err != nil {
			log.Warnf("Error sending email to %s: %v", recipient, err)
			// Continue with other recipients
		}
	}

	return nil
}

// sendOneEmail sends an email to a single recipient
func (e *EmailSender) sendOneEmail(recipient string, reportImage, mapImage []byte) error {
	from := mail.NewEmail(e.config.SendGridFromName, e.config.SendGridFromEmail)
	subject := "You got a CleanApp report"
	to := mail.NewEmail(recipient, recipient)

	// Encode report image
	encodedReportImage := base64.StdEncoding.EncodeToString(reportImage)

	// Create report attachment
	reportAttachment := mail.NewAttachment()
	reportAttachment.SetContent(encodedReportImage)
	reportAttachment.SetType("image/jpg")
	reportAttachment.SetFilename("report.jpg")
	reportAttachment.SetDisposition("inline")
	reportAttachment.SetContentID(reportImgCid)

	// Create message
	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", e.getEmailText(recipient)))
	message.AddContent(mail.NewContent("text/html", e.getEmailHtml(recipient)))
	message.AddAttachment(reportAttachment)

	// Add map attachment only if mapImage is provided
	if len(mapImage) > 0 {
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
	response, err := e.client.Send(message)
	if err != nil {
		return err
	}

	log.Infof("Email sent to %s! Status: %d", recipient, response.StatusCode)
	return nil
}

// sendOneEmailWithAnalysis sends an email to a single recipient with analysis data
func (e *EmailSender) sendOneEmailWithAnalysis(recipient string, reportImage, mapImage []byte, analysis *models.ReportAnalysis) error {
	from := mail.NewEmail(e.config.SendGridFromName, e.config.SendGridFromEmail)

	// Create subject with analysis title
	subject := "CleanApp Report"
	if analysis.Title != "" {
		subject = fmt.Sprintf("CleanApp Report: %s", analysis.Title)
	}

	to := mail.NewEmail(recipient, recipient)

	// Encode report image
	encodedReportImage := base64.StdEncoding.EncodeToString(reportImage)

	// Create report attachment
	reportAttachment := mail.NewAttachment()
	reportAttachment.SetContent(encodedReportImage)
	reportAttachment.SetType("image/jpg")
	reportAttachment.SetFilename("report.jpg")
	reportAttachment.SetDisposition("inline")
	reportAttachment.SetContentID(reportImgCid)

	// Create message
	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", e.getEmailTextWithAnalysis(recipient, analysis)))
	message.AddContent(mail.NewContent("text/html", e.getEmailHtmlWithAnalysis(recipient, analysis)))
	message.AddAttachment(reportAttachment)

	// Add map attachment only if mapImage is provided
	if len(mapImage) > 0 {
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
	response, err := e.client.Send(message)
	if err != nil {
		return err
	}

	log.Infof("Email with analysis sent to %s! Status: %d", recipient, response.StatusCode)
	return nil
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
func (e *EmailSender) getEmailText(recipient string) string {
	return `Hello,

You have received a new CleanApp report.

This email contains:
- The report image
- A map showing the location

Best regards,
The CleanApp Team`
}

// getEmailHtml returns the HTML content for emails
func (e *EmailSender) getEmailHtml(recipient string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>CleanApp Report</title>
</head>
<body>
    <h2>Hello,</h2>
    <p>You have received a new CleanApp report.</p>
    
    <h3>Report Image:</h3>
    <img src="cid:%s" alt="Report Image" style="max-width: 100%%; height: auto;">
    
    <h3>Location Map:</h3>
    <img src="cid:%s" alt="Map" style="max-width: 100%%; height: auto;">
    
    <p>Best regards,<br>The CleanApp Team</p>
</body>
</html>`, reportImgCid, mapImgCid)
}

// getEmailTextWithAnalysis returns the plain text content for emails with analysis data
func (e *EmailSender) getEmailTextWithAnalysis(recipient string, analysis *models.ReportAnalysis) string {
	var content string

	if analysis.Classification == "digital" {
		content = fmt.Sprintf(`Hello,

You have received a new CleanApp digital issue report with analysis.

REPORT ANALYSIS:
Title: %s
Description: %s
Type: Digital Issue

This email contains:
- The report image
- A map showing the location
- AI analysis results

Note: This is a digital issue report. Physical metrics (litter/hazard probability) are not applicable.

To unsubscribe from these emails, please visit: %s?email=%s
You can also reply to this email with "UNSUBSCRIBE" in the subject line.

Best regards,
The CleanApp Team`,
			analysis.Title,
			analysis.Description,
			e.config.OptOutURL,
			recipient)
	} else {
		content = fmt.Sprintf(`Hello,

You have received a new CleanApp report with analysis.

REPORT ANALYSIS:
Title: %s
Description: %s
Type: Physical Issue

PROBABILITY SCORES:
- Litter Probability: %.1f%%
- Hazard Probability: %.1f%%
- Severity Level: %.1f

This email contains:
- The report image
- A map showing the location
- AI analysis results

To unsubscribe from these emails, please visit: %s?email=%s
You can also reply to this email with "UNSUBSCRIBE" in the subject line.

Best regards,
The CleanApp Team`,
			analysis.Title,
			analysis.Description,
			analysis.LitterProbability*100,
			analysis.HazardProbability*100,
			analysis.SeverityLevel,
			e.config.OptOutURL,
			recipient)
	}

	return content
}

// getEmailHtmlWithAnalysis returns the HTML content for emails with analysis data
func (e *EmailSender) getEmailHtmlWithAnalysis(recipient string, analysis *models.ReportAnalysis) string {
	// Calculate gauge colors based on values
	litterColor := e.getGaugeColor(analysis.LitterProbability)
	hazardColor := e.getGaugeColor(analysis.HazardProbability)
	severityColor := e.getSeverityGaugeColor(analysis.SeverityLevel)

	// Determine if this is a digital report
	isDigital := analysis.Classification == "digital"

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>CleanApp Report: %s</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .header { background-color: #f8f9fa; padding: 20px; border-radius: 5px; margin-bottom: 20px; }
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
        <h2>CleanApp Report Analysis</h2>
        <p>A new report has been analyzed and requires your attention.</p>
    </div>
    
    <div class="analysis-section">
        <h3>Report Details</h3>
        <p><strong>Title:</strong> %s</p>
        <p><strong>Description:</strong> %s</p>
        <p><strong>Type:</strong> %s</p>
    </div>
    
    %s
    
    <div class="images">
        <div class="image-container">
            <h3>Report Image:</h3>
            <img src="cid:%s" alt="Report Image" style="max-width: 100%%; height: auto; border-radius: 5px;">
        </div>
        
        <div class="image-container">
            <h3>Location Map:</h3>
            <img src="cid:%s" alt="Map" style="max-width: 100%%; height: auto; border-radius: 5px;">
        </div>
    </div>
    
    <p><em>Best regards,<br>The CleanApp Team</em></p>
    
    <div style="margin-top: 30px; padding-top: 20px; border-top: 1px solid #eee; font-size: 0.9em; color: #666;">
        <p>To unsubscribe from these emails, please <a href="%s?email=%s" style="color: #007bff; text-decoration: none;">click here</a> or visit: %s</p>
    </div>
</body>
</html>`,
		analysis.Title,
		analysis.Title,
		analysis.Description,
		analysis.Classification,
		e.getMetricsSection(analysis, isDigital, litterColor, hazardColor, severityColor),
		reportImgCid,
		mapImgCid,
		e.config.OptOutURL,
		recipient,
		e.config.OptOutURL)
}

// getMetricsSection returns the appropriate metrics section based on report type
func (e *EmailSender) getMetricsSection(analysis *models.ReportAnalysis, isDigital bool, litterColor, hazardColor, severityColor string) string {
	if isDigital {
		// For digital reports, show a notice instead of metrics
		return ""
	}

	// For physical reports, show the metrics gauge
	return fmt.Sprintf(`
    <div class="gauge-grid">
        <div class="gauge-item">
            <div class="gauge-title">Litter Probability</div>
            <div class="gauge-container">
                <div class="gauge-fill %s" style="width: %.1f%%;"></div>
            </div>
            <div class="gauge-value">%.1f%%</div>
            <div class="gauge-label">%s</div>
        </div>
        
        <div class="gauge-item">
            <div class="gauge-title">Hazard Probability</div>
            <div class="gauge-container">
                <div class="gauge-fill %s" style="width: %.1f%%;"></div>
            </div>
            <div class="gauge-value">%.1f%%</div>
            <div class="gauge-label">%s</div>
        </div>
        
        <div class="gauge-item">
            <div class="gauge-title">Severity Level</div>
            <div class="gauge-container">
                <div class="gauge-fill %s" style="width: %.1f%%;"></div>
            </div>
            <div class="gauge-value">%.1f</div>
            <div class="gauge-label">%s</div>
        </div>
    </div>`,
		litterColor, analysis.LitterProbability*100, analysis.LitterProbability*100, e.getGaugeLabel(analysis.LitterProbability),
		hazardColor, analysis.HazardProbability*100, analysis.HazardProbability*100, e.getGaugeLabel(analysis.HazardProbability),
		severityColor, analysis.SeverityLevel*10, analysis.SeverityLevel*10, e.getSeverityGaugeLabel(analysis.SeverityLevel))
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
