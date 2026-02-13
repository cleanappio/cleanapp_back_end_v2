package service

import (
	"strings"
	"testing"
)

func TestIsValidEmail(t *testing.T) {
	service := &EmailService{}

	testCases := []struct {
		email       string
		expected    bool
		description string
	}{
		{"user@acme.com", true, "valid email"},
		{"user.name@domain.co.uk", true, "valid email with dots and country code"},
		{"user+tag@example.org", true, "valid email with plus sign"},
		{"test@example.com", false, "placeholder/example domain should be rejected"},
		{"invalid-email", false, "invalid email without domain"},
		{"@example.com", false, "invalid email without local part"},
		{"user@", false, "invalid email without domain"},
		{"", false, "empty string"},
		{"   ", false, "whitespace only"},
		{"user name@example.com", false, "email with space"},
		{"user@example..com", false, "email with consecutive dots"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := service.isValidEmail(tc.email)
			if result != tc.expected {
				t.Errorf("isValidEmail(%q) = %v, want %v", tc.email, result, tc.expected)
			}
		})
	}
}

func TestProcessInferredEmails(t *testing.T) {
	service := &EmailService{}

	// Test the email processing logic
	testCases := []struct {
		input       string
		expected    []string
		description string
	}{
		{
			"alpha@acme.com,user@domain.org",
			[]string{"alpha@acme.com", "user@domain.org"},
			"two valid emails",
		},
		{
			"alpha@acme.com, invalid-email, user@domain.org",
			[]string{"alpha@acme.com", "user@domain.org"},
			"mixed valid and invalid emails",
		},
		{
			"  alpha@acme.com  ,  user@domain.org  ",
			[]string{"alpha@acme.com", "user@domain.org"},
			"emails with whitespace",
		},
		{
			"",
			[]string{},
			"empty string",
		},
		{
			"invalid-email, another-invalid",
			[]string{},
			"only invalid emails",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Simulate the email processing logic
			emails := []string{}
			parts := strings.Split(strings.TrimSpace(tc.input), ",")

			for _, email := range parts {
				cleanEmail := strings.TrimSpace(email)
				if cleanEmail != "" && service.isValidEmail(cleanEmail) {
					emails = append(emails, cleanEmail)
				}
			}

			if len(emails) != len(tc.expected) {
				t.Errorf("Expected %d emails, got %d", len(tc.expected), len(emails))
				return
			}

			for i, expected := range tc.expected {
				if emails[i] != expected {
					t.Errorf("Expected email[%d] = %q, got %q", i, expected, emails[i])
				}
			}
		})
	}
}
