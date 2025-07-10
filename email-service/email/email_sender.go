package email

import (
	"encoding/base64"
	"fmt"
	"image"

	"email-service/config"
	"email-service/models"

	"github.com/apex/log"
	geojson "github.com/paulmach/go.geojson"
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

	// Encode images
	encodedReportImage := base64.StdEncoding.EncodeToString(reportImage)
	encodedMapImage := base64.StdEncoding.EncodeToString(mapImage)

	// Create attachments
	reportAttachment := mail.NewAttachment()
	reportAttachment.SetContent(encodedReportImage)
	reportAttachment.SetType("image/jpg")
	reportAttachment.SetFilename("report.jpg")
	reportAttachment.SetDisposition("inline")
	reportAttachment.SetContentID(reportImgCid)

	mapAttachment := mail.NewAttachment()
	mapAttachment.SetContent(encodedMapImage)
	mapAttachment.SetType("image/png")
	mapAttachment.SetFilename("map.png")
	mapAttachment.SetDisposition("inline")
	mapAttachment.SetContentID(mapImgCid)

	// Create email content
	plainTextContent := e.getEmailText(recipient)
	htmlContent := e.getEmailHtml(recipient)

	// Create message
	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", plainTextContent))
	message.AddContent(mail.NewContent("text/html", htmlContent))
	message.AddAttachment(reportAttachment)
	message.AddAttachment(mapAttachment)

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

	// Encode images
	encodedReportImage := base64.StdEncoding.EncodeToString(reportImage)
	encodedMapImage := base64.StdEncoding.EncodeToString(mapImage)

	// Create attachments
	reportAttachment := mail.NewAttachment()
	reportAttachment.SetContent(encodedReportImage)
	reportAttachment.SetType("image/jpg")
	reportAttachment.SetFilename("report.jpg")
	reportAttachment.SetDisposition("inline")
	reportAttachment.SetContentID(reportImgCid)

	mapAttachment := mail.NewAttachment()
	mapAttachment.SetContent(encodedMapImage)
	mapAttachment.SetType("image/png")
	mapAttachment.SetFilename("map.png")
	mapAttachment.SetDisposition("inline")
	mapAttachment.SetContentID(mapImgCid)

	// Create email content with analysis data
	plainTextContent := e.getEmailTextWithAnalysis(recipient, analysis)
	htmlContent := e.getEmailHtmlWithAnalysis(recipient, analysis)

	// Create message
	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", plainTextContent))
	message.AddContent(mail.NewContent("text/html", htmlContent))
	message.AddAttachment(reportAttachment)
	message.AddAttachment(mapAttachment)

	// Send email
	response, err := e.client.Send(message)
	if err != nil {
		return err
	}

	log.Infof("Email with analysis sent to %s! Status: %d", recipient, response.StatusCode)
	return nil
}

// GeneratePolygonImage generates a polygon image for the given feature
func (e *EmailSender) GeneratePolygonImage(feature *geojson.Feature, reportLat, reportLon float64) ([]byte, error) {
	// This is a simplified version - in a real implementation, you would:
	// 1. Parse the GeoJSON feature
	// 2. Create a map image with the polygon drawn on it
	// 3. Mark the report location on the map
	// 4. Return the image as bytes

	// For now, we'll create a simple placeholder image
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))

	// Draw a simple background
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, image.White)
		}
	}

	// Add some text
	e.addLabel(img, "Map View", 10, 20)
	e.addLabel(img, fmt.Sprintf("Report at: %.4f, %.4f", reportLat, reportLon), 10, 40)

	// Encode as PNG
	var buf []byte
	// In a real implementation, you would encode the image here
	// For now, return empty bytes
	return buf, nil
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
	content := fmt.Sprintf(`Hello,

You have received a new CleanApp report with analysis.

REPORT ANALYSIS:
Title: %s
Description: %s

PROBABILITY SCORES:
- Litter Probability: %.1f%%
- Hazard Probability: %.1f%%
- Severity Level: %.1f%%

SUMMARY:
%s

This email contains:
- The report image
- A map showing the location
- AI analysis results

Best regards,
The CleanApp Team`,
		analysis.Title,
		analysis.Description,
		analysis.LitterProbability*100,
		analysis.HazardProbability*100,
		analysis.SeverityLevel*100,
		analysis.Summary)

	return content
}

// getEmailHtmlWithAnalysis returns the HTML content for emails with analysis data
func (e *EmailSender) getEmailHtmlWithAnalysis(recipient string, analysis *models.ReportAnalysis) string {
	// Calculate gauge colors based on values
	litterColor := e.getGaugeColor(analysis.LitterProbability)
	hazardColor := e.getGaugeColor(analysis.HazardProbability)
	severityColor := e.getGaugeColor(analysis.SeverityLevel)

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
        .summary { background-color: #d1ecf1; padding: 15px; border-radius: 5px; margin: 15px 0; }
        .images { margin: 20px 0; }
        .image-container { margin: 15px 0; }
        .low { background: linear-gradient(90deg, #28a745, #20c997); }
        .medium { background: linear-gradient(90deg, #ffc107, #fd7e14); }
        .high { background: linear-gradient(90deg, #dc3545, #e83e8c); }
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
    </div>
    
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
            <div class="gauge-value">%.1f%%</div>
            <div class="gauge-label">%s</div>
        </div>
    </div>
    
    <div class="summary">
        <h3>Analysis Summary</h3>
        <p>%s</p>
    </div>
    
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
</body>
</html>`,
		analysis.Title,
		analysis.Title,
		analysis.Description,
		litterColor, analysis.LitterProbability*100, analysis.LitterProbability*100, e.getGaugeLabel(analysis.LitterProbability),
		hazardColor, analysis.HazardProbability*100, analysis.HazardProbability*100, e.getGaugeLabel(analysis.HazardProbability),
		severityColor, analysis.SeverityLevel*10, analysis.SeverityLevel*10, e.getGaugeLabel(analysis.SeverityLevel),
		analysis.Summary,
		reportImgCid,
		mapImgCid)
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
