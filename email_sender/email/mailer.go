// email/sendgrid.go
package email

import (
	"encoding/base64"
	"flag"
	"fmt"

	"github.com/apex/log"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var (
	apiKey    = flag.String("sendgrid_api_key", "", "SendGrid API Key.")
	fromName  = flag.String("sendgrid_from_name", "CleanApp", "SendGrid From Name")
	fromEmail = flag.String("sendgrid_from_email", "", "SendGrid From email")
)

type Mailer struct{}

func (m *Mailer) SendEmails(recipients []string, reportImage, mapImage []byte) {
	log.Infof("📧 Sending email to %d recipients...", len(recipients))
	for _, r := range recipients {
		if err := sendOneEmail(r, reportImage, mapImage); err != nil {
			log.Warnf("⚠️ Error sending email to %s: %v", r, err)
		}
	}
}

func sendOneEmail(recipient string, reportImage, mapImage []byte) error {
	from := mail.NewEmail(*fromName, *fromEmail)
	subject := "CleanApp report: new activity in your area"
	to := mail.NewEmail(recipient, recipient)

	reportAttachment := buildAttachment(reportImage, "report.jpg", "image/jpg", "reportImage")
	mapAttachment := buildAttachment(mapImage, "map.png", "image/png", "mapImage")

	plainTextContent := "New reports have been filed in your area. See attached."
	htmlContent := "<p><strong>CleanApp Alert</strong></p><p>New reports have been filed in your area. Please see the attached images.</p>"

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

	client := sendgrid.NewSendClient(*apiKey)
	response, err := client.Send(message)
	if err != nil {
		return err
	}

	fmt.Println("✅ Email sent to:", recipient)
	fmt.Println("Response:", response)
	return nil
}

func buildAttachment(data []byte, filename, mimeType, contentID string) *mail.Attachment {
	encoded := base64.StdEncoding.EncodeToString(data)
	attachment := mail.NewAttachment()
	attachment.SetContent(encoded)
	attachment.SetType(mimeType)
	attachment.SetFilename(filename)
	attachment.SetDisposition("inline")
	attachment.SetContentID(contentID)
	return attachment
}
