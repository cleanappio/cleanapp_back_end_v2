package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type DigitalShareAttachment struct {
	Ordinal  int
	Filename string
	MIMEType string
	SHA256   string
	Bytes    []byte
}

func (d *Database) UpsertDigitalShareMetadata(
	ctx context.Context,
	reportSeq int,
	sourceURL string,
	sourceApp string,
	platform string,
	captureMode string,
	clientCreatedAt string,
	clientSubmissionID string,
	normalizedSourceKey string,
	sharedText string,
	attachments []DigitalShareAttachment,
) error {
	if reportSeq <= 0 {
		return nil
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin digital share tx: %w", err)
	}
	defer tx.Rollback()

	sourceURL = strings.TrimSpace(sourceURL)
	sourceApp = strings.TrimSpace(sourceApp)
	platform = strings.TrimSpace(platform)
	captureMode = strings.TrimSpace(captureMode)
	clientSubmissionID = strings.TrimSpace(clientSubmissionID)
	normalizedSourceKey = strings.TrimSpace(normalizedSourceKey)
	sharedText = strings.TrimSpace(sharedText)

	if sourceURL != "" {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO report_details (seq, company_name, product_name, url)
			 VALUES (?, NULL, NULL, ?)
			 ON DUPLICATE KEY UPDATE url = VALUES(url)`,
			reportSeq,
			sourceURL,
		); err != nil {
			return fmt.Errorf("upsert report_details url: %w", err)
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO external_ingest_index (source, external_id, seq, source_timestamp, source_url)
			 VALUES (?, ?, ?, ?, ?)
			 ON DUPLICATE KEY UPDATE seq = VALUES(seq), source_timestamp = VALUES(source_timestamp), source_url = VALUES(source_url), updated_at = NOW()`,
			"digital_share",
			firstNonBlank(clientSubmissionID, fmt.Sprintf("share:%d", reportSeq)),
			reportSeq,
			nullableDBTime(parseDBRFC3339(clientCreatedAt)),
			sourceURL,
		); err != nil {
			return fmt.Errorf("upsert external_ingest_index: %w", err)
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO digital_share_reports (
			report_seq, source_url, source_app, platform, capture_mode,
			client_created_at, client_submission_id, normalized_source_key, shared_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			source_url = VALUES(source_url),
			source_app = VALUES(source_app),
			platform = VALUES(platform),
			capture_mode = VALUES(capture_mode),
			client_created_at = VALUES(client_created_at),
			client_submission_id = VALUES(client_submission_id),
			normalized_source_key = VALUES(normalized_source_key),
			shared_text = VALUES(shared_text),
			updated_at = CURRENT_TIMESTAMP`,
		reportSeq,
		nullIfBlank(sourceURL),
		nullIfBlank(sourceApp),
		nullIfBlank(platform),
		nullIfBlank(captureMode),
		nullableDBTime(parseDBRFC3339(clientCreatedAt)),
		nullIfBlank(clientSubmissionID),
		nullIfBlank(normalizedSourceKey),
		nullIfBlank(sharedText),
	); err != nil {
		return fmt.Errorf("upsert digital_share_reports: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM digital_share_attachments WHERE report_seq = ?`, reportSeq); err != nil {
		return fmt.Errorf("clear digital_share_attachments: %w", err)
	}

	for _, attachment := range attachments {
		if len(attachment.Bytes) == 0 {
			continue
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO digital_share_attachments (
				report_seq, ordinal, filename, mime_type, sha256, image
			) VALUES (?, ?, ?, ?, ?, ?)`,
			reportSeq,
			attachment.Ordinal,
			nullIfBlank(attachment.Filename),
			nullIfBlank(attachment.MIMEType),
			nullIfBlank(attachment.SHA256),
			attachment.Bytes,
		); err != nil {
			return fmt.Errorf("insert digital_share_attachment[%d]: %w", attachment.Ordinal, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit digital share tx: %w", err)
	}
	return nil
}

func (d *Database) GetReportPublicIDBySeq(ctx context.Context, seq int) (string, error) {
	var publicID sql.NullString
	if err := d.db.QueryRowContext(ctx, `SELECT public_id FROM reports WHERE seq = ?`, seq).Scan(&publicID); err != nil {
		return "", err
	}
	return publicID.String, nil
}

func parseDBRFC3339(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &parsed
}

func nullableDBTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func nullIfBlank(raw string) interface{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.TrimSpace(raw)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
