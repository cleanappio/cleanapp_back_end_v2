package database

import (
	"context"
	"database/sql"
	"fmt"
)

// This media-only restriction is independent of report-page publication. The
// report_seq lookup is indexed by the Wire report foreign key; any retained
// submission requiring redaction must also prevent legacy/direct image access.
const publicReportMediaPrivacySQL = `NOT EXISTS (
	SELECT 1 FROM wire_submissions_raw media_wire
	WHERE media_wire.report_seq = r.seq
	AND (JSON_EXTRACT(media_wire.report_json,'$.privacy.contains_pii') = TRUE
	 OR JSON_EXTRACT(media_wire.report_json,'$.privacy.requires_redaction') = TRUE)
)`

func (d *Database) GetImageByPublicID(ctx context.Context, publicID string) ([]byte, error) {
	query := `
		SELECT r.image
		FROM reports r
		WHERE r.public_id = ?
		AND ` + publicReportCurrentVisibilitySQL + ` AND ` + publicReportMediaPrivacySQL

	var image []byte
	err := d.db.QueryRowContext(ctx, query, publicID).Scan(&image)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("report with public_id %s not found", publicID)
		}
		return nil, fmt.Errorf("failed to get image for report public_id %s: %w", publicID, err)
	}
	return image, nil
}

// Public media previews never transfer unbounded image blobs to the proxy.
func (d *Database) GetPublicReportMediaByPublicID(ctx context.Context, publicID string, maxBytes int) ([]byte, error) {
	var image []byte
	err := d.db.QueryRowContext(ctx, `
		SELECT CASE WHEN OCTET_LENGTH(r.image) <= ? THEN r.image ELSE NULL END
		FROM reports r WHERE r.public_id = ? AND `+publicReportCurrentVisibilitySQL+` AND `+publicReportMediaPrivacySQL, maxBytes, publicID).Scan(&image)
	if err != nil {
		return nil, err
	}
	return image, nil
}
