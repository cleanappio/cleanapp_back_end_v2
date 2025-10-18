package database

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/apex/log"
)

// GdprService handles GDPR processing operations
type GdprService struct {
	db *sql.DB
}

// NewGdprService creates a new GDPR service instance
func NewGdprService(db *sql.DB) *GdprService {
	return &GdprService{db: db}
}

// AvatarExists checks if an avatar value already exists in the users table
func (s *GdprService) AvatarExists(avatar string) (bool, error) {
	query := `SELECT COUNT(*) FROM users WHERE avatar = ?`

	var count int
	err := s.db.QueryRow(query, avatar).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if avatar exists: %w", err)
	}

	return count > 0, nil
}

// UpdateUserAvatar updates the avatar field for a specific user
func (s *GdprService) UpdateUserAvatar(userID string, obfuscatedAvatar string) error {
	query := `UPDATE users SET avatar = ? WHERE id = ?`

	_, err := s.db.Exec(query, obfuscatedAvatar, userID)
	if err != nil {
		return fmt.Errorf("failed to update avatar for user %s: %w", userID, err)
	}

	log.Infof("Updated avatar for user %s to obfuscated value", userID)
	return nil
}

// GenerateUniqueAvatar generates a unique avatar by adding asterisks until uniqueness is achieved
func (s *GdprService) GenerateUniqueAvatar(obfuscatedAvatar string) (string, error) {
	uniqueAvatar := obfuscatedAvatar
	maxAttempts := 100 // Prevent infinite loops

	// Keep adding asterisks until we get a unique avatar
	for attempts := 0; attempts < maxAttempts; attempts++ {
		exists, err := s.AvatarExists(uniqueAvatar)
		if err != nil {
			return "", fmt.Errorf("failed to check avatar uniqueness: %w", err)
		}

		if !exists {
			// Avatar is unique, return it
			if attempts > 0 {
				log.Infof("Generated unique avatar after %d attempts: '%s' -> '%s'", attempts, obfuscatedAvatar, uniqueAvatar)
			}
			return uniqueAvatar, nil
		}

		// Avatar already exists, add an asterisk
		uniqueAvatar += "*"
		log.Infof("Avatar '%s' already exists, adding asterisk (attempt %d): '%s'", obfuscatedAvatar, attempts+1, uniqueAvatar)
	}

	// If we've reached the maximum attempts, generate a unique ID-based avatar
	fallbackAvatar := fmt.Sprintf("%s_%d", obfuscatedAvatar, time.Now().UnixNano())
	log.Warnf("Reached maximum attempts for avatar uniqueness, using fallback: '%s'", fallbackAvatar)

	return fallbackAvatar, nil
}

// MarkUserProcessed marks a user as processed for GDPR
func (s *GdprService) MarkUserProcessed(userID string) error {
	query := `INSERT INTO users_gdpr (id) VALUES (?)`

	_, err := s.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to mark user %s as processed: %w", userID, err)
	}

	log.Infof("Marked user %s as GDPR processed", userID)
	return nil
}

// MarkReportProcessed marks a report as processed for GDPR
func (s *GdprService) MarkReportProcessed(seq int) error {
	query := `INSERT INTO reports_gdpr (seq) VALUES (?)`

	_, err := s.db.Exec(query, seq)
	if err != nil {
		return fmt.Errorf("failed to mark report %d as processed: %w", seq, err)
	}

	log.Infof("Marked report %d as GDPR processed", seq)
	return nil
}

// GetReportImage fetches the image data for a specific report
func (s *GdprService) GetReportImage(seq int) ([]byte, error) {
	query := `SELECT image FROM reports WHERE seq = ?`

	var imageData []byte
	err := s.db.QueryRow(query, seq).Scan(&imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to get image for report %d: %w", seq, err)
	}

	log.Infof("Retrieved image for report %d, size: %d bytes", seq, len(imageData))
	return imageData, nil
}

// UpdateReportImage updates the image data for a specific report
func (s *GdprService) UpdateReportImage(seq int, imageData []byte) error {
	query := `UPDATE reports SET image = ? WHERE seq = ?`

	_, err := s.db.Exec(query, imageData, seq)
	if err != nil {
		return fmt.Errorf("failed to update image for report %d: %w", seq, err)
	}

	log.Infof("Updated image for report %d, new size: %d bytes", seq, len(imageData))
	return nil
}

// GetPlaceholderImage reads the placeholder image file for document replacement
func (s *GdprService) GetPlaceholderImage(placeholderPath string) ([]byte, error) {
	// Read the placeholder image file
	imageData, err := os.ReadFile(placeholderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read placeholder image from %s: %w", placeholderPath, err)
	}

	log.Infof("Retrieved placeholder image from %s, size: %d bytes", placeholderPath, len(imageData))
	return imageData, nil
}

// GetProcessingStats returns statistics about GDPR processing
func (s *GdprService) GetProcessingStats() (map[string]int, error) {
	stats := make(map[string]int)

	// Count total users
	var totalUsers int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&totalUsers)
	if err != nil {
		return nil, fmt.Errorf("failed to count total users: %w", err)
	}
	stats["total_users"] = totalUsers

	// Count processed users
	var processedUsers int
	err = s.db.QueryRow("SELECT COUNT(*) FROM users_gdpr").Scan(&processedUsers)
	if err != nil {
		return nil, fmt.Errorf("failed to count processed users: %w", err)
	}
	stats["processed_users"] = processedUsers

	// Count total reports
	var totalReports int
	err = s.db.QueryRow("SELECT COUNT(*) FROM reports").Scan(&totalReports)
	if err != nil {
		return nil, fmt.Errorf("failed to count total reports: %w", err)
	}
	stats["total_reports"] = totalReports

	// Count processed reports
	var processedReports int
	err = s.db.QueryRow("SELECT COUNT(*) FROM reports_gdpr").Scan(&processedReports)
	if err != nil {
		return nil, fmt.Errorf("failed to count processed reports: %w", err)
	}
	stats["processed_reports"] = processedReports

	return stats, nil
}
