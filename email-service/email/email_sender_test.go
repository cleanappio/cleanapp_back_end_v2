package email

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"cleanapp-common/mailtransport"
	"email-service/config"
	"email-service/models"
)

type recordingWorkspace struct {
	messages []mailtransport.Message
	err      error
}

func (s *recordingWorkspace) Send(message mailtransport.Message) (string, error) {
	s.messages = append(s.messages, message)
	if s.err != nil {
		return "", s.err
	}
	return "<accepted@cleanapp.io>", nil
}

func (s *recordingWorkspace) Provider() string { return "google_workspace" }

func workspaceEmailSender(transport *recordingWorkspace) *EmailSender {
	return &EmailSender{
		config: &config.Config{
			EmailFromName: "CleanApp", EmailFromAddress: "info@cleanapp.io",
		},
		workspace: transport,
		provider:  "google_workspace",
	}
}

func TestWorkspaceReportEmailsPreserveContentAndInlineImages(t *testing.T) {
	transport := &recordingWorkspace{}
	sender := workspaceEmailSender(transport)
	report, mapImage := []byte("report bytes"), []byte("map bytes")
	if err := sender.SendEmails([]string{"recipient@acme.com"}, report, mapImage); err != nil {
		t.Fatal(err)
	}
	if err := sender.SendEmailsWithAnalysis([]string{"other@acme.com"}, report, mapImage, &models.ReportAnalysis{
		BrandName: "Acme", BrandReportCount: 2, Title: "Damaged equipment",
	}); err != nil {
		t.Fatal(err)
	}
	for _, message := range transport.messages {
		if message.FromEmail != "info@cleanapp.io" || message.FromName != "CleanApp" {
			t.Fatalf("wrong sender: %#v", message)
		}
		if len(message.To) != 1 || message.Text == "" || message.HTML == "" {
			t.Fatalf("missing recipient or alternative body: %#v", message)
		}
		if len(message.Attachments) != 2 {
			t.Fatalf("got %d inline attachments", len(message.Attachments))
		}
		for i, cid := range []string{reportImgCid, mapImgCid} {
			attachment := message.Attachments[i]
			if !attachment.Inline || attachment.ContentID != cid || !strings.Contains(message.HTML, "cid:"+cid) {
				t.Fatalf("lost inline image reference: %#v", attachment)
			}
		}
		if !bytes.Equal(message.Attachments[0].Data, report) || !bytes.Equal(message.Attachments[1].Data, mapImage) {
			t.Fatal("attachment data changed")
		}
	}
}

func TestWorkspaceReportWithoutImagesOmitsAttachments(t *testing.T) {
	transport := &recordingWorkspace{}
	if err := workspaceEmailSender(transport).SendEmails([]string{"recipient@acme.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	message := transport.messages[0]
	if len(message.Attachments) != 0 || strings.Contains(message.HTML, "cid:") {
		t.Fatal("missing images should not create attachments or CID references")
	}
}

func TestWorkspaceAggregateRetainsOptOutAndSubject(t *testing.T) {
	transport := &recordingWorkspace{}
	if err := workspaceEmailSender(transport).SendAggregateEmail([]string{"recipient@acme.com"}, &models.BrandReportSummary{
		BrandName: "Acme", NewReportCount: 2, TotalReportCount: 5,
	}, "https://cleanapp.io/opt-out"); err != nil {
		t.Fatal(err)
	}
	message := transport.messages[0]
	if message.Subject != "2 new reports about Acme (5 total)" ||
		!strings.Contains(message.Text, "https://cleanapp.io/opt-out") ||
		!strings.Contains(message.HTML, "https://cleanapp.io/opt-out") {
		t.Fatalf("aggregate subject or opt-out missing: %#v", message)
	}
}

func TestWorkspaceCustomSendReportsProviderAndFailure(t *testing.T) {
	transport := &recordingWorkspace{}
	sender := workspaceEmailSender(transport)
	success := sender.SendCustomEmails([]string{"recipient@acme.com"}, "Subject", "Body", "<p>Body</p>")[0]
	if success.Status != "sent" || success.Provider != "google_workspace" || success.ProviderMessageID != "<accepted@cleanapp.io>" || success.SentAt.IsZero() {
		t.Fatalf("wrong success metadata: %#v", success)
	}
	transport.err = errors.New("SMTP rejected message")
	failure := sender.SendCustomEmails([]string{"recipient@acme.com"}, "Subject", "Body", "")[0]
	if failure.Status != "failed" || failure.Provider != "google_workspace" || failure.ProviderMessageID != "" || !failure.SentAt.IsZero() || failure.Error == "" {
		t.Fatalf("SMTP failure incorrectly recorded: %#v", failure)
	}
	// The SendGrid client is nil: reaching rollback after failure would panic.
	if len(transport.messages) != 2 {
		t.Fatal("failed send unexpectedly retried")
	}
}

func TestNewEmailSenderValidatesSelectedProvider(t *testing.T) {
	if _, err := NewEmailSender(&config.Config{EmailProvider: "unknown"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if _, err := NewEmailSender(&config.Config{EmailProvider: "google_workspace", SMTP: mailtransport.Config{Host: "smtp-relay.gmail.com", Port: -1}}); err == nil {
		t.Fatal("invalid SMTP configuration accepted")
	}
	legacy, err := NewEmailSender(&config.Config{})
	if err != nil || legacy.Provider() != "sendgrid" || legacy.client == nil {
		t.Fatalf("legacy configuration should preserve SendGrid: %v", err)
	}
}

func TestSendGridRollbackPreservesAlternativesAndInlineAttachment(t *testing.T) {
	message := mailtransport.Message{
		FromName: "CleanApp", FromEmail: "info@cleanapp.io", To: []string{"recipient@acme.com"},
		Subject: "Subject", Text: "Text", HTML: "<img src=\"cid:report_image\">",
		Attachments: []mailtransport.Attachment{{
			Filename: "report.jpg", ContentType: "image/jpeg", ContentID: reportImgCid, Inline: true, Data: []byte("image"),
		}},
	}
	legacy := sendGridMessage(message)
	if legacy.From.Address != message.FromEmail || len(legacy.Content) != 2 || len(legacy.Personalizations) != 1 || len(legacy.Attachments) != 1 {
		t.Fatalf("rollback message lost content: %#v", legacy)
	}
	if legacy.Attachments[0].ContentID != reportImgCid || legacy.Attachments[0].Disposition != "inline" || legacy.Attachments[0].Content != "aW1hZ2U=" {
		t.Fatalf("rollback attachment changed: %#v", legacy.Attachments[0])
	}
}
