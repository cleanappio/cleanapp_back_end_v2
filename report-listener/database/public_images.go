package database

import (
	"context"
	"database/sql"
	"fmt"
)

func (d *Database) GetImageByPublicID(ctx context.Context, publicID string) ([]byte, error) {
	query := `
		SELECT r.image
		FROM reports r
		WHERE r.public_id = ?
		AND ` + publicReportCurrentVisibilitySQL

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
