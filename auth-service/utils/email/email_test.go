package email

import (
	"cleanapp-common/mailtransport"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type workspaceRecorder struct {
	message mailtransport.Message
	err     error
}

func (r *workspaceRecorder) Send(message mailtransport.Message) (string, error) {
	r.message = message
	return "workspace-message", r.err
}

type sendGridRecorder struct {
	message  *mail.SGMailV3
	response *rest.Response
	err      error
}

func (r *sendGridRecorder) Send(message *mail.SGMailV3) (*rest.Response, error) {
	r.message = message
	return r.response, r.err
}

func workspaceConfig() mailtransport.Config {
	return mailtransport.Config{Host: "smtp-relay.gmail.com", Port: 587, Timeout: 30 * time.Second}
}

func TestWorkspaceSenderInitializesWithoutSendGridKey(t *testing.T) {
	sender, err := NewConfiguredSender("google_workspace", "", "CleanApp", "info@cleanapp.io", workspaceConfig())
	if err != nil {
		t.Fatal(err)
	}
	if sender == nil || sender.workspace == nil || sender.client != nil || sender.Provider() != "google_workspace" {
		t.Fatal("expected initialized Workspace sender without SendGrid client")
	}
}

func TestSenderInitializationRejectsInvalidSelectedTransport(t *testing.T) {
	for _, provider := range []string{"google_workspace", "unknown"} {
		if _, err := NewConfiguredSender(provider, "", "CleanApp", "info@cleanapp.io", mailtransport.Config{}); err == nil {
			t.Fatalf("expected error for %q", provider)
		}
	}
}

func TestLegacySendGridInitialization(t *testing.T) {
	for _, provider := range []string{"", "sendgrid"} {
		disabled, err := NewConfiguredSender(provider, "", "CleanApp", "info@cleanapp.io", mailtransport.Config{})
		if err != nil || disabled != nil {
			t.Fatalf("legacy sender without key must remain disabled: %v", err)
		}
		sender, err := NewConfiguredSender(provider, "test-only-sendgrid-key", "CleanApp", "info@cleanapp.io", mailtransport.Config{})
		if err != nil || sender == nil || sender.client == nil || sender.workspace != nil || sender.Provider() != "sendgrid" {
			t.Fatalf("legacy configured sender did not select SendGrid: %v", err)
		}
	}
}

func TestWorkspacePasswordResetPreservesMessageAndPropagatesFailure(t *testing.T) {
	recorder := &workspaceRecorder{}
	sender := &Sender{workspace: recorder, provider: "google_workspace", fromName: "CleanApp", fromEmail: "info@cleanapp.io"}
	resetURL := "https://cleanapp.io/reset-password?token=test-only-reset-token"
	if err := sender.SendPasswordResetEmail("recipient@example.com", resetURL); err != nil {
		t.Fatal(err)
	}
	message := recorder.message
	if message.FromName != "CleanApp" || message.FromEmail != "info@cleanapp.io" || len(message.To) != 1 || message.To[0] != "recipient@example.com" || message.Subject != "Reset Your CleanApp Password" {
		t.Fatalf("unexpected password reset envelope: %+v", message)
	}
	if !strings.Contains(message.Text, resetURL) || !strings.Contains(message.HTML, `href="`+resetURL+`"`) || !strings.Contains(message.Text, "expire in 1 hour") {
		t.Fatal("password reset content was not preserved")
	}
	transportErr := errors.New("SMTP rejected delivery")
	recorder.err = transportErr
	if err := sender.SendPasswordResetEmail("recipient@example.com", resetURL); !errors.Is(err, transportErr) {
		t.Fatalf("expected SMTP delivery error to propagate, got %v", err)
	}
}

func TestSendGridPasswordResetRollbackAndRejection(t *testing.T) {
	recorder := &sendGridRecorder{response: &rest.Response{StatusCode: 202}}
	sender := &Sender{client: recorder, provider: "sendgrid", fromName: "CleanApp", fromEmail: "info@cleanapp.io"}
	if err := sender.SendPasswordResetEmail("recipient@example.com", "https://cleanapp.io/reset-password?token=test-only-reset-token"); err != nil {
		t.Fatal(err)
	}
	if recorder.message == nil || recorder.message.From.Address != "info@cleanapp.io" || recorder.message.Subject != "Reset Your CleanApp Password" || len(recorder.message.Content) != 2 {
		t.Fatal("SendGrid rollback message did not retain sender and both body formats")
	}
	recorder.response = &rest.Response{StatusCode: 401, Body: "expired account"}
	if err := sender.SendPasswordResetEmail("recipient@example.com", "https://cleanapp.io/reset-password"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected SendGrid rejection to propagate, got %v", err)
	}
}
