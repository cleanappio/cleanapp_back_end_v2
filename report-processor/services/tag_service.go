package services

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// TagService handles tag operations
type TagService struct {
	db *sql.DB
}

// NewTagService creates a new tag service
func NewTagService(db *sql.DB) *TagService {
	return &TagService{db: db}
}

// EnsureTagTables creates the necessary tag tables
func (ts *TagService) EnsureTagTables(ctx context.Context) error {
	// Create tags table
	createTagsTable := `
	CREATE TABLE IF NOT EXISTS tags (
		id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		canonical_name VARCHAR(255) NOT NULL UNIQUE,
		display_name VARCHAR(255) NOT NULL,
		usage_count INT UNSIGNED DEFAULT 0,
		last_used_at TIMESTAMP NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_canonical_name (canonical_name),
		INDEX idx_usage_count (usage_count DESC),
		INDEX idx_last_used (last_used_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

	if _, err := ts.db.ExecContext(ctx, createTagsTable); err != nil {
		return fmt.Errorf("failed to create tags table: %w", err)
	}

	// Create report_tags table
	createReportTagsTable := `
	CREATE TABLE IF NOT EXISTS report_tags (
		report_seq INT NOT NULL,
		tag_id INT UNSIGNED NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (report_seq, tag_id),
		INDEX idx_tag_id (tag_id),
		INDEX idx_report_seq (report_seq),
		FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
	) ENGINE=InnoDB`

	if _, err := ts.db.ExecContext(ctx, createReportTagsTable); err != nil {
		return fmt.Errorf("failed to create report_tags table: %w", err)
	}

	// Create user_tag_follows table
	createUserTagFollowsTable := `
	CREATE TABLE IF NOT EXISTS user_tag_follows (
		user_id VARCHAR(256) NOT NULL,
		tag_id INT UNSIGNED NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, tag_id),
		INDEX idx_user_id (user_id),
		INDEX idx_tag_id (tag_id),
		FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
	) ENGINE=InnoDB`

	if _, err := ts.db.ExecContext(ctx, createUserTagFollowsTable); err != nil {
		return fmt.Errorf("failed to create user_tag_follows table: %w", err)
	}

	log.Println("Tag tables ensured successfully")
	return nil
}

// normalizeTag normalizes a tag string
func (ts *TagService) normalizeTag(input string) (canonical, display string, err error) {
	// Trim whitespace
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", "", fmt.Errorf("tag cannot be empty")
	}

	// Remove leading # if present
	withoutHash := strings.TrimPrefix(trimmed, "#")

	// Unicode NFKC normalization
	t := transform.Chain(norm.NFKC, runes.Remove(runes.In(unicode.Mn)))
	normalized, _, err := transform.String(t, withoutHash)
	if err != nil {
		return "", "", fmt.Errorf("failed to normalize tag: %w", err)
	}

	// Convert to lowercase
	canonical = strings.ToLower(normalized)

	// Validate length
	if len(canonical) < 1 || len(canonical) > 64 {
		return "", "", fmt.Errorf("tag must be between 1 and 64 characters")
	}

	// Basic character validation - allow letters, numbers, spaces, and common punctuation
	for _, r := range canonical {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsSpace(r) && !strings.ContainsRune(".-_", r) {
			return "", "", fmt.Errorf("tag contains invalid characters")
		}
	}

	return canonical, withoutHash, nil
}

// upsertTag creates or updates a tag
func (ts *TagService) upsertTag(ctx context.Context, canonical, display string) (int64, error) {
	// Try to insert the tag
	insertQuery := `
		INSERT INTO tags (canonical_name, display_name, usage_count, last_used_at) 
		VALUES (?, ?, 0, NULL)
		ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`

	result, err := ts.db.ExecContext(ctx, insertQuery, canonical, display)
	if err != nil {
		return 0, fmt.Errorf("failed to upsert tag: %w", err)
	}

	// Get the tag ID
	tagID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get tag ID: %w", err)
	}

	return tagID, nil
}

// incrementTagUsage increments the usage count for a tag
func (ts *TagService) incrementTagUsage(ctx context.Context, tagID int64) error {
	updateQuery := `
		UPDATE tags 
		SET usage_count = usage_count + 1, last_used_at = NOW() 
		WHERE id = ?`

	_, err := ts.db.ExecContext(ctx, updateQuery, tagID)
	if err != nil {
		return fmt.Errorf("failed to increment tag usage: %w", err)
	}

	return nil
}

// AddTagsToReport adds tags to a report
func (ts *TagService) AddTagsToReport(ctx context.Context, reportSeq int, tagStrings []string) ([]string, error) {
	var addedTags []string

	for _, tagString := range tagStrings {
		// Normalize the tag
		canonical, display, err := ts.normalizeTag(tagString)
		if err != nil {
			log.Printf("Failed to normalize tag '%s': %v", tagString, err)
			continue // Skip invalid tags
		}

		// Upsert the tag
		tagID, err := ts.upsertTag(ctx, canonical, display)
		if err != nil {
			log.Printf("Failed to upsert tag '%s': %v", canonical, err)
			continue
		}

		// Add to report_tags (ignore if already exists)
		insertReportTagQuery := `
			INSERT IGNORE INTO report_tags (report_seq, tag_id) 
			VALUES (?, ?)`

		_, err = ts.db.ExecContext(ctx, insertReportTagQuery, reportSeq, tagID)
		if err != nil {
			log.Printf("Failed to add tag to report: %v", err)
			continue
		}

		// Increment usage count
		if err := ts.incrementTagUsage(ctx, tagID); err != nil {
			log.Printf("Failed to increment tag usage: %v", err)
			// Don't fail the whole operation for this
		}

		addedTags = append(addedTags, canonical)
	}

	return addedTags, nil
}

// GetTagsForReport gets all tags for a specific report
func (ts *TagService) GetTagsForReport(ctx context.Context, reportSeq int) ([]map[string]interface{}, error) {
	query := `
		SELECT t.id, t.canonical_name, t.display_name, t.usage_count, t.last_used_at, t.created_at
		FROM tags t
		INNER JOIN report_tags rt ON t.id = rt.tag_id
		WHERE rt.report_seq = ?
		ORDER BY t.usage_count DESC`

	rows, err := ts.db.QueryContext(ctx, query, reportSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags for report: %w", err)
	}
	defer rows.Close()

	var tags []map[string]interface{}
	for rows.Next() {
		var id int64
		var canonicalName, displayName string
		var usageCount int64
		var lastUsedAt, createdAt sql.NullTime

		if err := rows.Scan(&id, &canonicalName, &displayName, &usageCount, &lastUsedAt, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}

		tag := map[string]interface{}{
			"id":            id,
			"canonical_name": canonicalName,
			"display_name":  displayName,
			"usage_count":   usageCount,
		}

		if lastUsedAt.Valid {
			tag["last_used_at"] = lastUsedAt.Time
		}
		if createdAt.Valid {
			tag["created_at"] = createdAt.Time
		}

		tags = append(tags, tag)
	}

	return tags, nil
}
