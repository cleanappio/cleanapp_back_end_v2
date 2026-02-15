package server

import (
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

// reportCountCache holds cached report counts with thread-safe access
type reportCountCache struct {
	mu             sync.RWMutex
	totalReports   int
	physicalCount  int
	digitalCount   int
	lastCountedSeq int
	lastUpdated    time.Time
	initialized    bool
}

var countCache = &reportCountCache{}

// GetValidReportsCount returns cached report counts instantly (<1ms)
// The background job updates the cache every 10 minutes
func GetValidReportsCount(c *gin.Context) {
	countCache.mu.RLock()
	defer countCache.mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"total_reports":          countCache.totalReports,
		"total_physical_reports": countCache.physicalCount,
		"total_digital_reports":  countCache.digitalCount,
		"last_updated":           countCache.lastUpdated.Format(time.RFC3339),
	})
}

// StartCounterCacheUpdater starts a background goroutine that updates report counts
// It uses incremental counting to avoid full table scans
func StartCounterCacheUpdater() {
	log.Info("Starting counter cache updater...")

	// Do initial update (may be slow on first run)
	go func() {
		// Wait a bit for DB to be ready
		time.Sleep(5 * time.Second)
		updateCounterCache()

		// Then update every 10 minutes
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			updateCounterCache()
		}
	}()
}

// updateCounterCache incrementally updates the cached counts
// On first run: does a full count (slow but only once)
// On subsequent runs: only counts new reports since lastCountedSeq (fast)
func updateCounterCache() {
	startTime := time.Now()

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Counter cache: failed to connect to DB: %v", err)
		return
	}

	// Get current max seq
	var maxSeq int
	err = dbc.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM report_analysis").Scan(&maxSeq)
	if err != nil {
		log.Errorf("Counter cache: failed to get max seq: %v", err)
		return
	}

	countCache.mu.RLock()
	initialized := countCache.initialized
	lastSeq := countCache.lastCountedSeq
	countCache.mu.RUnlock()

	if !initialized {
		// Prefer loading from the materialized counters table to avoid repeated full scans
		// on every service restart. If the table isn't present yet, fall back to a full scan.
		if loaded := tryLoadCounterCacheFromDB(dbc); loaded {
			countCache.mu.RLock()
			lastSeq = countCache.lastCountedSeq
			countCache.mu.RUnlock()
			log.Infof("Counter cache: loaded from DB (last_seq=%d)", lastSeq)
		} else {
			// First run: do full count (will be slow, ~30-60s, but only once)
			log.Info("Counter cache: initializing with full count (this may take a minute)...")
			fullUpdateCounterCache(dbc, maxSeq)
			persistCounterCacheToDB(dbc)
			log.Infof("Counter cache: initialization complete in %v", time.Since(startTime))
			return
		}
	}

	// Incremental update: only count new reports since lastSeq
	if maxSeq <= lastSeq {
		log.Infof("Counter cache: no new reports (max=%d, last=%d)", maxSeq, lastSeq)
		return
	}

	incrementalUpdateCounterCache(dbc, lastSeq, maxSeq)
	persistCounterCacheToDB(dbc)
	log.Infof("Counter cache: incremental update complete in %v (counted seq %d to %d)",
		time.Since(startTime), lastSeq, maxSeq)
}

// fullUpdateCounterCache does a full count of all valid reports
// This is slow (~30-60s on 1M+ rows) but only runs once on startup
func fullUpdateCounterCache(dbc *sql.DB, maxSeq int) {
	// Single grouped scan instead of three COUNT(DISTINCT ...) full scans.
	// Semantics preserved: totals are distinct by seq; physical/digital count
	// a seq if it has at least one valid row with that classification.
	var total, physical, digital int
	err := dbc.QueryRow(`
		SELECT
			COUNT(*) AS total_reports,
			COALESCE(SUM(has_physical), 0) AS physical_reports,
			COALESCE(SUM(has_digital), 0) AS digital_reports
		FROM (
			SELECT
				seq,
				MAX(CASE WHEN classification = 'physical' THEN 1 ELSE 0 END) AS has_physical,
				MAX(CASE WHEN classification = 'digital' THEN 1 ELSE 0 END) AS has_digital
			FROM report_analysis
			WHERE is_valid = TRUE
			GROUP BY seq
		) grouped
	`).Scan(&total, &physical, &digital)
	if err != nil {
		log.Errorf("Counter cache: failed to count full totals: %v", err)
		return
	}

	// Update cache
	countCache.mu.Lock()
	countCache.totalReports = total
	countCache.physicalCount = physical
	countCache.digitalCount = digital
	countCache.lastCountedSeq = maxSeq
	countCache.lastUpdated = time.Now()
	countCache.initialized = true
	countCache.mu.Unlock()

	log.Infof("Counter cache: full count complete - total=%d, physical=%d, digital=%d", total, physical, digital)
}

// incrementalUpdateCounterCache only counts new reports since lastSeq
// This is much faster (<5s) since it only processes new rows
func incrementalUpdateCounterCache(dbc *sql.DB, lastSeq, maxSeq int) {
	// Single grouped incremental scan instead of three distinct scans.
	var newTotal, newPhysical, newDigital int
	err := dbc.QueryRow(`
		SELECT
			COUNT(*) AS total_reports,
			COALESCE(SUM(has_physical), 0) AS physical_reports,
			COALESCE(SUM(has_digital), 0) AS digital_reports
		FROM (
			SELECT
				seq,
				MAX(CASE WHEN classification = 'physical' THEN 1 ELSE 0 END) AS has_physical,
				MAX(CASE WHEN classification = 'digital' THEN 1 ELSE 0 END) AS has_digital
			FROM report_analysis
			WHERE seq > ? AND is_valid = TRUE
			GROUP BY seq
		) grouped
	`, lastSeq).Scan(&newTotal, &newPhysical, &newDigital)
	if err != nil {
		log.Errorf("Counter cache: failed to count incremental totals: %v", err)
		return
	}

	// Update cache by adding new counts
	countCache.mu.Lock()
	countCache.totalReports += newTotal
	countCache.physicalCount += newPhysical
	countCache.digitalCount += newDigital
	countCache.lastCountedSeq = maxSeq
	countCache.lastUpdated = time.Now()
	countCache.mu.Unlock()

	log.Infof("Counter cache: added new reports - +%d total, +%d physical, +%d digital", newTotal, newPhysical, newDigital)
}

// tryLoadCounterCacheFromDB attempts to initialize the in-memory cache from the
// materialized counters table. This avoids repeated full table scans on restarts.
// Returns true if loaded successfully, false if unavailable (e.g. table missing).
func tryLoadCounterCacheFromDB(dbc *sql.DB) bool {
	var total, physical, digital, lastSeq int
	err := dbc.QueryRow(`
		SELECT total_valid, physical_valid, digital_valid, last_counted_seq
		FROM report_counts_total
		WHERE id = 1
	`).Scan(&total, &physical, &digital, &lastSeq)
	if err != nil {
		// Most commonly: "Error 1146: Table ... doesn't exist" before patch is applied.
		return false
	}
	if lastSeq <= 0 {
		return false
	}

	countCache.mu.Lock()
	countCache.totalReports = total
	countCache.physicalCount = physical
	countCache.digitalCount = digital
	countCache.lastCountedSeq = lastSeq
	countCache.lastUpdated = time.Now()
	countCache.initialized = true
	countCache.mu.Unlock()
	return true
}

func persistCounterCacheToDB(dbc *sql.DB) {
	countCache.mu.RLock()
	total := countCache.totalReports
	physical := countCache.physicalCount
	digital := countCache.digitalCount
	lastSeq := countCache.lastCountedSeq
	countCache.mu.RUnlock()

	// Best-effort: if table doesn't exist yet, ignore silently.
	_, _ = dbc.Exec(`
		INSERT INTO report_counts_total (id, total_valid, physical_valid, digital_valid, last_counted_seq)
		VALUES (1, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			total_valid = VALUES(total_valid),
			physical_valid = VALUES(physical_valid),
			digital_valid = VALUES(digital_valid),
			last_counted_seq = VALUES(last_counted_seq)
	`, total, physical, digital, lastSeq)
}
