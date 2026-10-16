package database

import (
	"context"
	"log"
	"time"

	"cleanapp-common/evidencesourceprojection"
)

func (d *Database) VerifyEvidenceSourceProjection(ctx context.Context) error {
	return evidencesourceprojection.Verify(ctx, d.db)
}

// RunEvidenceSourceProjectionWorker maintains derived source data only. One
// bounded batch per interval limits ingestion overhead. Dirty rows remain in the
// durable SQL queue on failure; public requests always recompute those rows.
// The in-memory cursor provides fairness past a repeatedly failing batch and
// resets on the next sweep. It is not a completion checkpoint.
func (d *Database) RunEvidenceSourceProjectionWorker(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	log.Print("Evidence source projection refresher started; bounded batches of 100 every 30 seconds")
	defer log.Print("Evidence source projection refresher stopped")
	after := 0
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ids, err := d.nextEvidenceSourceProjectionBatch(batchCtx, after)
		if err == nil && len(ids) > 0 {
			after = ids[len(ids)-1]
			_, err = RefreshEvidenceSourceProjection(batchCtx, d.db, ids)
		} else if err == nil {
			after = 0
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			// SQL/driver errors can contain source values or transport details.
			log.Print("Evidence source projection refresh failed; dirty entries retained")
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (d *Database) nextEvidenceSourceProjectionBatch(ctx context.Context, after int) ([]int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT /*+ MAX_EXECUTION_TIME(8000) */ p.seq
 FROM `+evidencesourceprojection.Table+` p FORCE INDEX(idx_dirty_seq)
 STRAIGHT_JOIN reports r ON r.seq=p.seq
 WHERE p.dirty=TRUE AND p.seq>? ORDER BY p.seq LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		ids = append(ids, seq)
	}
	return ids, rows.Err()
}
