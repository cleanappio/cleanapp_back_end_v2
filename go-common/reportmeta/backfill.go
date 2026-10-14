package reportmeta

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Backfill reconciles only a caller-selected bounded batch. FOR SHARE makes
// INSERT...SELECT a locking current read: concurrent UPDATE/DELETE waits until
// this copy commits, then its trigger updates/removes the copied row. A plain
// snapshot SELECT followed by an INSERT could resurrect a deleted report.
// UPSERT repairs rows written while a previous migration had only some triggers
// installed; it cannot overwrite a newer committed source with a stale snapshot.
func Backfill(ctx context.Context, db *sql.DB, seqs []int) (int64, error) {
	if len(seqs) == 0 {
		return 0, nil
	}
	if len(seqs) > 200 {
		return 0, fmt.Errorf("metadata backfill batch exceeds 200 reports")
	}
	args := make([]any, len(seqs))
	for i, seq := range seqs {
		args[i] = seq
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	query := `INSERT INTO report_metadata_projection (seq,id,public_id,ts,latitude,longitude,has_original_media)
		SELECT seq,id,public_id,ts,latitude,longitude,COALESCE(LENGTH(image),0)>0 FROM reports
		WHERE seq IN (` + strings.TrimRight(strings.Repeat("?,", len(seqs)), ",") + `) FOR SHARE
		ON DUPLICATE KEY UPDATE id=VALUES(id),public_id=VALUES(public_id),ts=VALUES(ts),
		latitude=VALUES(latitude),longitude=VALUES(longitude),has_original_media=VALUES(has_original_media)`
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("copy report metadata batch: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
