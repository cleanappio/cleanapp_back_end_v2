package eiiheader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Backfill copies only a caller-selected cohort batch, with current source
// locks through commit. Concurrent EII updates/deletes then run their triggers
// after the copy, preventing stale overwrite or resurrection. UPSERT also
// reconciles a partial prior trigger migration. No source value is rewritten.
func Backfill(ctx context.Context, db *sql.DB, seqs []int) (affected int64, retErr error) {
	if len(seqs) == 0 {
		return 0, nil
	}
	if len(seqs) > 200 {
		return 0, fmt.Errorf("external source header backfill batch exceeds 200 reports")
	}
	conn, restore, err := projectionSession(ctx, db)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	defer func() { retErr = errors.Join(retErr, restore()) }()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	args := make([]any, len(seqs))
	for i, seq := range seqs {
		args[i] = seq
	}
	query := `INSERT INTO external_ingest_header_projection (seq,source,external_id,source_url,source_timestamp,created_at)
 SELECT seq,source,external_id,source_url,source_timestamp,created_at FROM external_ingest_index FORCE INDEX(idx_seq)
 WHERE seq IN (` + strings.TrimRight(strings.Repeat("?,", len(seqs)), ",") + `) FOR SHARE
 ON DUPLICATE KEY UPDATE source=VALUES(source),external_id=VALUES(external_id),source_url=VALUES(source_url),source_timestamp=VALUES(source_timestamp),created_at=VALUES(created_at)`
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("copy external source header batch: %w", err)
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
