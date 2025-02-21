package email

import (
	"flag"
	"fmt"

	"github.com/apex/log"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var (
	apiKey = flag.String("sendgrid_api_key", "secret", "SendGrid API Key.")
	fromName = flag.String("sendgrid_from_name", "CleanApp Info", "SendGrid From Name")
	fromEmail = flag.String("sendgrid_from_email", "info@cleanapp.io", "SendGrid From email")
)

func SendEmails(recipients []string) {
	log.Infof("!!!Sending email to %d recipients!!!", len(recipients))
	for _, r := range recipients {
		if err := sendOneEmail(r); err != nil {
			log.Warnf("Error sending email to %s: %w", err)
		}
	} 
}

func sendOneEmail(recipient string) error {
	from := mail.NewEmail(*fromName, *fromEmail)
	subject := "Report from CleanApp community member"
	to := mail.NewEmail(recipient, recipient)
	plainTextContent := `
		Test Header

		Test Body
	`
	htmlContent := "<b>Test Header</b><br><br>Test Body"

	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	client := sendgrid.NewSendClient(*apiKey) // Replace with your API Key

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