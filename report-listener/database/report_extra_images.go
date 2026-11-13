package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Photos 2 to 4 of a report (report_extra_images). The first photo is
// reports.image. See docs/operations/report-photos.md.

// ReportExtraImageCount returns how many further photos a report has (0 to 3).
// It applies no visibility rule: call it only for a report already read under
// the public rules.
func (d *Database) ReportExtraImageCount(ctx context.Context, seq int) (int, error) {
	var count int
	if err := d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM report_extra_images WHERE seq = ? AND position BETWEEN 1 AND 3", seq).Scan(&count); err != nil {
		return 0, fmt.Errorf("count extra images: %w", err)
	}
	return count, nil
}

// GetExtraImageByPublicID returns photo number position+1 of a public report
// (position 1 to 3), under exactly the rules that govern the first photo:
// the report must be publicly visible and its media not withheld for privacy.
// It returns (nil, nil) when there is no such photo or the rules do not allow it.
func (d *Database) GetExtraImageByPublicID(ctx context.Context, publicID string, position int) ([]byte, error) {
	if position < 1 || position > 3 {
		return nil, nil
	}
	var image []byte
	err := d.db.QueryRowContext(ctx, `
		SELECT x.image
		FROM reports r
		JOIN report_extra_images x ON x.seq = r.seq AND x.position = ?
		WHERE r.public_id = ?
		AND `+publicReportCurrentVisibilitySQL+` AND `+publicReportMediaPrivacySQL, position, publicID).Scan(&image)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get extra image: %w", err)
	}
	return image, nil
}
