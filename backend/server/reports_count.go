package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

const (
	reportCounterBatchSize       = 10000
	reportCounterQueryTimeout    = 5 * time.Second
	reportCounterBatchPause      = time.Second
	reportCounterRefreshInterval = 10 * time.Minute
)

// The existing covering index makes each page an ordered, index-only read of
// at most batchSize distinct seq values. Include invalid groups for progress;
// only has_valid contributes to the totals. All languages share one group.
const reportCounterPageSQL = `
	SELECT COALESCE(SUM(has_valid), 0),
	       COALESCE(SUM(has_physical), 0),
	       COALESCE(SUM(has_digital), 0), MAX(seq)
	FROM (
		SELECT seq,
		       MAX(CASE WHEN is_valid = TRUE THEN 1 ELSE 0 END) AS has_valid,
		       MAX(CASE WHEN is_valid = TRUE AND classification = 'physical' THEN 1 ELSE 0 END) AS has_physical,
		       MAX(CASE WHEN is_valid = TRUE AND classification = 'digital' THEN 1 ELSE 0 END) AS has_digital
		FROM report_analysis FORCE INDEX (idx_ra_seq_class_valid)
		WHERE seq > ? AND seq <= ?
		GROUP BY seq ORDER BY seq LIMIT ?
	) grouped`

type reportCountSnapshot struct {
	totalReports   int
	physicalCount  int
	digitalCount   int
	lastCountedSeq int // Completed pass upper bound, not an incremental cursor.
	lastUpdated    time.Time
}

type reportCountCache struct {
	mu sync.RWMutex
	reportCountSnapshot
	initialized bool
}

var countCache = &reportCountCache{}

// Requests only read the last complete snapshot; they never initiate DB work.
func GetValidReportsCount(c *gin.Context) {
	countCache.mu.RLock()
	defer countCache.mu.RUnlock()
	if !countCache.initialized {
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Report counts are not available yet"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total_reports":          countCache.totalReports,
		"total_physical_reports": countCache.physicalCount,
		"total_digital_reports":  countCache.digitalCount,
		"last_updated":           countCache.lastUpdated.Format(time.RFC3339),
	})
}

func StartCounterCacheUpdater() {
	log.Info("Starting report counter reconciliation...")
	go func() {
		time.Sleep(5 * time.Second)
		for {
			updateCounterCache()
			// Wait after completion, so a slow pass cannot queue overlapping work.
			time.Sleep(reportCounterRefreshInterval)
		}
	}()
}

func updateCounterCache() {
	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Counter cache: failed to connect to DB: %v", err)
		return
	}
	countCache.mu.RLock()
	initialized := countCache.initialized
	countCache.mu.RUnlock()
	if !initialized {
		tryLoadCounterCacheFromDB(dbc)
	}
	started := time.Now()
	if err := refreshCounterCache(context.Background(), dbc, countCache, reportCounterBatchSize, pauseCounterPage); err != nil {
		log.Errorf("Counter cache: reconciliation failed; retaining previous snapshot: %v", err)
		return
	}
	log.Infof("Counter cache: reconciliation complete in %v", time.Since(started))
}

// Re-read every seq on each pass. Analysis can arrive or change at any older
// report seq; MAX(seq) therefore cannot establish that the counts are current.
// The upper bound only makes one pass finite while new reports are arriving.
func reconcileReportCounts(ctx context.Context, dbc *sql.DB, batchSize int, pause func(context.Context) error) (reportCountSnapshot, error) {
	if batchSize < 1 || batchSize > reportCounterBatchSize {
		return reportCountSnapshot{}, fmt.Errorf("invalid report counter batch size")
	}
	queryCtx, cancel := context.WithTimeout(ctx, reportCounterQueryTimeout)
	var minSeq, maxSeq int
	err := dbc.QueryRowContext(queryCtx, `SELECT COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0)
		FROM report_analysis FORCE INDEX (idx_ra_seq_class_valid)`).Scan(&minSeq, &maxSeq)
	cancel()
	if err != nil {
		return reportCountSnapshot{}, fmt.Errorf("report counter bounds: %w", err)
	}
	snapshot := reportCountSnapshot{lastCountedSeq: maxSeq}
	cursor := minSeq - 1 // seq is a MySQL INT; Go int is 64-bit on this service.
	for cursor < maxSeq {
		var total, physical, digital int
		var lastSeq sql.NullInt64
		queryCtx, cancel = context.WithTimeout(ctx, reportCounterQueryTimeout)
		err = dbc.QueryRowContext(queryCtx, reportCounterPageSQL, cursor, maxSeq, batchSize).
			Scan(&total, &physical, &digital, &lastSeq)
		cancel()
		if err != nil {
			return reportCountSnapshot{}, fmt.Errorf("report counter page: %w", err)
		}
		if !lastSeq.Valid {
			break // Remaining rows may have been deleted since reading the bound.
		}
		if lastSeq.Int64 <= int64(cursor) || lastSeq.Int64 > int64(maxSeq) {
			return reportCountSnapshot{}, fmt.Errorf("report counter page did not advance within its bound")
		}
		snapshot.totalReports += total
		snapshot.physicalCount += physical
		snapshot.digitalCount += digital
		cursor = int(lastSeq.Int64)
		if cursor < maxSeq && pause != nil {
			if err := pause(ctx); err != nil {
				return reportCountSnapshot{}, err
			}
		}
	}
	snapshot.lastUpdated = time.Now().UTC()
	return snapshot, nil
}

func pauseCounterPage(ctx context.Context) error {
	timer := time.NewTimer(reportCounterBatchPause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func refreshCounterCache(ctx context.Context, dbc *sql.DB, cache *reportCountCache, batchSize int, pause func(context.Context) error) error {
	snapshot, err := reconcileReportCounts(ctx, dbc, batchSize, pause)
	if err != nil {
		return err
	}
	// Keep durable readers and the HTTP cache on the same complete snapshot.
	// A failed write must not turn a partial or non-durable pass into freshness.
	if err := persistCounterSnapshot(ctx, dbc, snapshot); err != nil {
		return err
	}
	cache.mu.Lock()
	cache.reportCountSnapshot = snapshot
	cache.initialized = true
	cache.mu.Unlock()
	return nil
}

func tryLoadCounterCacheFromDB(dbc *sql.DB) bool {
	ctx, cancel := context.WithTimeout(context.Background(), reportCounterQueryTimeout)
	defer cancel()
	var snapshot reportCountSnapshot
	var updatedUnix int64
	err := dbc.QueryRowContext(ctx, `
		SELECT total_valid, physical_valid, digital_valid, last_counted_seq,
		       COALESCE(UNIX_TIMESTAMP(updated_at), 0)
		FROM report_counts_total WHERE id = 1
	`).Scan(&snapshot.totalReports, &snapshot.physicalCount, &snapshot.digitalCount, &snapshot.lastCountedSeq, &updatedUnix)
	if err != nil || snapshot.lastCountedSeq <= 0 || updatedUnix <= 0 {
		return false
	}
	// Loading an old snapshot is not a fresh reconciliation.
	snapshot.lastUpdated = time.Unix(updatedUnix, 0).UTC()
	countCache.mu.Lock()
	countCache.reportCountSnapshot = snapshot
	countCache.initialized = true
	countCache.mu.Unlock()
	return true
}

func persistCounterSnapshot(ctx context.Context, dbc *sql.DB, snapshot reportCountSnapshot) error {
	ctx, cancel := context.WithTimeout(ctx, reportCounterQueryTimeout)
	defer cancel()
	_, err := dbc.ExecContext(ctx, `
		INSERT INTO report_counts_total (id, total_valid, physical_valid, digital_valid, last_counted_seq, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE total_valid = VALUES(total_valid),
			physical_valid = VALUES(physical_valid), digital_valid = VALUES(digital_valid),
			last_counted_seq = VALUES(last_counted_seq), updated_at = VALUES(updated_at)
	`, snapshot.totalReports, snapshot.physicalCount, snapshot.digitalCount, snapshot.lastCountedSeq, snapshot.lastUpdated)
	if err != nil {
		return fmt.Errorf("persist report counter snapshot: %w", err)
	}
	return nil
}
