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
	apiKey    = flag.String("sendgrid_api_key", "secret", "SendGrid API Key.")
	fromName  = flag.String("sendgrid_from_name", "CleanApp Info", "SendGrid From Name")
	fromEmail = flag.String("sendgrid_from_email", "info@cleanapp.io", "SendGrid From email")
)

func SendEmails(recipients []string, reportImage []byte) {
	log.Infof("!!!Sending email to %d recipients!!!", len(recipients))
	for _, r := range recipients {
		if err := sendOneEmail(r, reportImage); err != nil {
			log.Warnf("Error sending email to %s: %w", err)
		}
	}
}

func sendOneEmail(recipient string, reportImage []byte) error {
	from := mail.NewEmail(*fromName, *fromEmail)
	subject := "Report from CleanApp community member"
	to := mail.NewEmail(recipient, recipient)

	encodedImage := base64.StdEncoding.EncodeToString(reportImage)

	attachment := mail.NewAttachment()
	attachment.SetContent(encodedImage)
	attachment.SetType("image/jpg")
	attachment.SetFilename("report.jpg")
	attachment.SetDisposition("inline")
	attachment.SetContentID(reportImgCid)

	plainTextContent := getEmailText(recipient)
	htmlContent := getEmailHtml(recipient)

	message := mail.NewV3Mail()
	message.SetFrom(from)
	message.Subject = subject

	p := mail.NewPersonalization()
	p.AddTos(to)
	message.AddPersonalizations(p)

	message.AddContent(mail.NewContent("text/plain", plainTextContent))
	message.AddContent(mail.NewContent("text/html", htmlContent))
	message.AddAttachment(attachment)

	client := sendgrid.NewSendClient(*apiKey)

	response, err := client.Send(message)
	if err != nil {
		return err
	}

	fmt.Println("Email Sent!")
	fmt.Println("Status Code:", response.StatusCode)
	fmt.Println("Response Body:", response.Body)
	fmt.Println("Response Headers:", response.Headers)

	return nil
}
