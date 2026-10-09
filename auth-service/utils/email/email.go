package email

import (
	"cleanapp-common/mailtransport"
	"fmt"
	"strings"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

type sendGridClient interface {
	Send(*mail.SGMailV3) (*rest.Response, error)
}

type workspaceSender interface {
	Send(mailtransport.Message) (string, error)
}

// Sender sends password reset messages through the configured provider.
type Sender struct {
	client    sendGridClient
	workspace workspaceSender
	provider  string
	fromName  string
	fromEmail string
}

// NewSender creates a new email sender
func NewSender(apiKey, fromName, fromEmail string) *Sender {
	return &Sender{
		client:    sendgrid.NewSendClient(apiKey),
		provider:  "sendgrid",
		fromName:  fromName,
		fromEmail: fromEmail,
	}
}

// NewConfiguredSender preserves the legacy disabled sender when SendGrid has no
// key, while an explicitly selected Workspace sender must be configured.
func NewConfiguredSender(provider, apiKey, fromName, fromEmail string, smtpConfig mailtransport.Config) (*Sender, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "sendgrid":
		if apiKey == "" {
			return nil, nil
		}
		return NewSender(apiKey, fromName, fromEmail), nil
	case "google_workspace":
		transport, err := mailtransport.New(smtpConfig)
		if err != nil {
			return nil, fmt.Errorf("initialize google_workspace email sender: %w", err)
		}
		return &Sender{workspace: transport, provider: "google_workspace", fromName: fromName, fromEmail: fromEmail}, nil
	default:
		return nil, fmt.Errorf("unsupported EMAIL_PROVIDER %q", provider)
	}
}

func (s *Sender) Provider() string {
	return s.provider
}

// SendPasswordResetEmail sends a password reset email with the reset link
func (s *Sender) SendPasswordResetEmail(recipientEmail, resetURL string) error {
	from := mail.NewEmail(s.fromName, s.fromEmail)
	subject := "Reset Your CleanApp Password"
	to := mail.NewEmail(recipientEmail, recipientEmail)

	plainText := fmt.Sprintf(`Hello,

You have requested to reset your password for your CleanApp account.

Click the link below to reset your password:
%s

This link will expire in 1 hour.

If you did not request a password reset, please ignore this email.

Best regards,
The CleanApp Team`, resetURL)

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Reset Your Password</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #4CAF50; color: white; padding: 20px; border-radius: 5px 5px 0 0; text-align: center; }
        .content { background-color: #f9f9f9; padding: 30px; border: 1px solid #ddd; }
        .button { display: inline-block; background-color: #4CAF50; color: white; padding: 12px 30px; text-decoration: none; border-radius: 5px; margin: 20px 0; }
        .button:hover { background-color: #45a049; }
        .footer { padding: 20px; text-align: center; font-size: 0.9em; color: #666; }
        .warning { background-color: #fff3cd; border: 1px solid #ffc107; padding: 10px; border-radius: 5px; margin-top: 20px; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Password Reset</h1>
    </div>
    <div class="content">
        <p>Hello,</p>
        <p>You have requested to reset your password for your CleanApp account.</p>
        <p>Click the button below to reset your password:</p>
        <p style="text-align: center;">
            <a href="%s" class="button" style="color: white;">Reset Password</a>
        </p>
        <p>Or copy and paste this link into your browser:</p>
        <p style="word-break: break-all; color: #666;">%s</p>
        <div class="warning">
            <strong>Note:</strong> This link will expire in 1 hour.
        </div>
        <p>If you did not request a password reset, please ignore this email. Your password will remain unchanged.</p>
    </div>
    <div class="footer">
        <p>Best regards,<br>The CleanApp Team</p>
    </div>
</body>
</html>`, resetURL, resetURL)

	if s.workspace != nil {
		_, err := s.workspace.Send(mailtransport.Message{
			FromName:  s.fromName,
			FromEmail: s.fromEmail,
			To:        []string{recipientEmail},
			Subject:   subject,
			Text:      plainText,
			HTML:      htmlContent,
		})
		if err != nil {
			return fmt.Errorf("failed to send password reset email via google_workspace: %w", err)
		}
		return nil
	}

	message := mail.NewSingleEmail(from, subject, to, plainText, htmlContent)

	response, err := s.client.Send(message)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("sendgrid returned status %d: %s", response.StatusCode, response.Body)
}
