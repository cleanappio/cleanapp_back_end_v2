package processor

import (
	"fmt"
	"gdpr-process-service/openai"

	"github.com/apex/log"
)

// GdprProcessor handles the actual GDPR processing logic
type GdprProcessor struct {
	openaiClient *openai.Client
}

// NewGdprProcessor creates a new GDPR processor instance
func NewGdprProcessor(openaiClient *openai.Client) *GdprProcessor {
	return &GdprProcessor{
		openaiClient: openaiClient,
	}
}

// ProcessUser processes a single user for GDPR compliance
func (p *GdprProcessor) ProcessUser(userID string, avatar string, updateAvatar func(string, string) error, generateUniqueAvatar func(string) (string, error)) error {
	log.Infof("Processing user %s for GDPR compliance", userID)

	if avatar == "" {
		log.Infof("User %s has no avatar to process", userID)
		return nil
	}

	// Process avatar text for PII detection and obfuscation
	obfuscatedAvatar, err := p.openaiClient.DetectAndObfuscatePII(avatar)
	if err != nil {
		return fmt.Errorf("failed to process avatar for user %s: %w", userID, err)
	}

	log.Infof("User %s avatar processed: original='%s', obfuscated='%s'", userID, avatar, obfuscatedAvatar)

	// Check if the avatar was changed (obfuscated)
	if obfuscatedAvatar != avatar {
		log.Infof("Avatar changed for user %s, generating unique avatar", userID)

		// Generate a unique avatar by adding asterisks if needed
		uniqueAvatar, err := generateUniqueAvatar(obfuscatedAvatar)
		if err != nil {
			return fmt.Errorf("failed to generate unique avatar for user %s: %w", userID, err)
		}

		if uniqueAvatar != obfuscatedAvatar {
			log.Infof("Generated unique avatar for user %s: '%s' -> '%s'", userID, obfuscatedAvatar, uniqueAvatar)
		}

		// Update the user's avatar in the database
		if err := updateAvatar(userID, uniqueAvatar); err != nil {
			return fmt.Errorf("failed to update avatar for user %s: %w", userID, err)
		}

		log.Infof("Successfully updated avatar for user %s in database with unique value", userID)
	} else {
		log.Infof("No PII detected in avatar for user %s, no update needed", userID)
	}

	return nil
}

// ProcessReport processes a single report for GDPR compliance
// This is a placeholder function that will be implemented later
func (p *GdprProcessor) ProcessReport(seq int) error {
	log.Infof("Processing report %d for GDPR compliance (placeholder)", seq)

	// TODO: Implement actual GDPR processing logic
	// This could include:
	// - Image data handling
	// - Location data anonymization
	// - Metadata processing
	// - Data retention enforcement

	return nil
}
