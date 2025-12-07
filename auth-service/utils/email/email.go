package email

import (
	"fmt"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

// Sender handles sending emails via SendGrid
type Sender struct {
	client    *sendgrid.Client
	fromName  string
	fromEmail string
}

// NewSender creates a new email sender
func NewSender(apiKey, fromName, fromEmail string) *Sender {
	return &Sender{
		client:    sendgrid.NewSendClient(apiKey),
		fromName:  fromName,
		fromEmail: fromEmail,
	}
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
