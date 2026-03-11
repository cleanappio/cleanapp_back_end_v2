package database

import (
	"context"
	"database/sql"
	"fmt"
)

func (d *Database) GetImageByPublicID(ctx context.Context, publicID string) ([]byte, error) {
	query := fmt.Sprintf(`
		SELECT r.image
		FROM reports r
		LEFT JOIN report_raw rr ON r.seq = rr.report_seq
		WHERE r.public_id = ?
		AND %s
	`, PublicVisibilityWhereSQL)

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
