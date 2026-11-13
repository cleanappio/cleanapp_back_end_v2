package database

import (
	"context"
	"fmt"
)

// ReportExtraImages returns photos 2 to 4 of a report in the order the
// reporter added them. The caller must already have established that the
// report may be shown: this function applies no visibility rule of its own.
func (d *Database) ReportExtraImages(ctx context.Context, seq int) ([][]byte, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT image FROM report_extra_images WHERE seq = ? ORDER BY position LIMIT 3", seq)
	if err != nil {
		return nil, fmt.Errorf("query extra images: %w", err)
	}
	defer rows.Close()
	var images [][]byte
	for rows.Next() {
		var image []byte
		if err := rows.Scan(&image); err != nil {
			return nil, fmt.Errorf("scan extra image: %w", err)
		}
		images = append(images, image)
	}
	return images, rows.Err()
}
