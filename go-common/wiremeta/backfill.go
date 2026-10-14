package wiremeta

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Backfill copies a caller-selected cohort batch under current source locks
// through commit. Concurrent parent writes then apply after the copy, avoiding
// stale overwrites/deletion resurrection. UPSERT repairs partial migrations.
func Backfill(ctx context.Context, db *sql.DB, seqs []int) (affected int64, retErr error) {
	if len(seqs) == 0 {
		return 0, nil
	}
	if len(seqs) > 200 {
		return 0, fmt.Errorf("Wire metadata backfill batch exceeds 200 reports")
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
	columns := projectionColumns()
	updates := []string{}
	for _, name := range columns {
		updates = append(updates, name+"=VALUES("+name+")")
	}
	query := "INSERT INTO wire_evidence_projection (" + strings.Join(columns, ",") + ") SELECT " + strings.Join(sourceExpressions("ws"), ",") + " FROM wire_submissions_raw ws WHERE ws.report_seq IN (" + strings.TrimRight(strings.Repeat("?,", len(seqs)), ",") + ") FOR SHARE ON DUPLICATE KEY UPDATE " + strings.Join(updates, ",")
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("copy Wire metadata batch: %w", err)
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
