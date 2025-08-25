package database

import (
	"database/sql"
	"fmt"

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

// GetUnprocessedUsers returns users that haven't been processed for GDPR
func (s *GdprService) GetUnprocessedUsers() ([]string, error) {
	query := `
		SELECT u.id 
		FROM users u 
		LEFT JOIN users_gdpr ug ON u.id = ug.id 
		WHERE ug.id IS NULL
		ORDER BY u.ts ASC
		LIMIT 100`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query unprocessed users: %w", err)
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan user ID: %w", err)
		}
		userIDs = append(userIDs, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over user rows: %w", err)
	}

	return userIDs, nil
}

// GetUserData returns user data including avatar for a specific user ID
func (s *GdprService) GetUserData(userID string) (string, error) {
	query := `SELECT avatar FROM users WHERE id = ?`

	var avatar string
	err := s.db.QueryRow(query, userID).Scan(&avatar)
	if err != nil {
		return "", fmt.Errorf("failed to get user data for %s: %w", userID, err)
	}

	return avatar, nil
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

// GetUnprocessedReports returns reports that haven't been processed for GDPR
func (s *GdprService) GetUnprocessedReports() ([]int, error) {
	query := `
		SELECT r.seq 
		FROM reports r 
		LEFT JOIN reports_gdpr rg ON r.seq = rg.seq 
		WHERE rg.seq IS NULL
		ORDER BY r.ts ASC
		LIMIT 100`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query unprocessed reports: %w", err)
	}
	defer rows.Close()

	var reportSeqs []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, fmt.Errorf("failed to scan report seq: %w", err)
		}
		reportSeqs = append(reportSeqs, seq)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over report rows: %w", err)
	}

	return reportSeqs, nil
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
