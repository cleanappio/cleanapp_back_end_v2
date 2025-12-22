package server

import (
	"database/sql"
	"net/http"
	"sync"
	"time"

	"cleanapp/common"

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

	dbc, err := common.DBConnect()
	if err != nil {
		log.Errorf("Counter cache: failed to connect to DB: %v", err)
		return
	}
	defer dbc.Close()

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
		// First run: do full count (will be slow, ~30-60s, but only once)
		log.Info("Counter cache: initializing with full count (this may take a minute)...")
		fullUpdateCounterCache(dbc, maxSeq)
		log.Infof("Counter cache: initialization complete in %v", time.Since(startTime))
		return
	}

	// Incremental update: only count new reports since lastSeq
	if maxSeq <= lastSeq {
		log.Infof("Counter cache: no new reports (max=%d, last=%d)", maxSeq, lastSeq)
		return
	}

	incrementalUpdateCounterCache(dbc, lastSeq, maxSeq)
	log.Infof("Counter cache: incremental update complete in %v (counted seq %d to %d)", 
		time.Since(startTime), lastSeq, maxSeq)
}

// fullUpdateCounterCache does a full count of all valid reports
// This is slow (~30-60s on 1M+ rows) but only runs once on startup
func fullUpdateCounterCache(dbc *sql.DB, maxSeq int) {
	// Count total valid reports
	var total int
	err := dbc.QueryRow(`
		SELECT COUNT(DISTINCT seq) 
		FROM report_analysis 
		WHERE is_valid = TRUE
	`).Scan(&total)
	if err != nil {
		log.Errorf("Counter cache: failed to count total: %v", err)
		return
	}

	// Count physical valid reports
	var physical int
	err = dbc.QueryRow(`
		SELECT COUNT(DISTINCT seq) 
		FROM report_analysis 
		WHERE classification = 'physical' AND is_valid = TRUE
	`).Scan(&physical)
	if err != nil {
		log.Errorf("Counter cache: failed to count physical: %v", err)
		return
	}

	// Count digital valid reports
	var digital int
	err = dbc.QueryRow(`
		SELECT COUNT(DISTINCT seq) 
		FROM report_analysis 
		WHERE classification = 'digital' AND is_valid = TRUE
	`).Scan(&digital)
	if err != nil {
		log.Errorf("Counter cache: failed to count digital: %v", err)
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
	// Count new valid reports since lastSeq
	var newTotal int
	err := dbc.QueryRow(`
		SELECT COUNT(DISTINCT seq) 
		FROM report_analysis 
		WHERE seq > ? AND is_valid = TRUE
	`, lastSeq).Scan(&newTotal)
	if err != nil {
		log.Errorf("Counter cache: failed to count new total: %v", err)
		return
	}

	// Count new physical valid reports since lastSeq
	var newPhysical int
	err = dbc.QueryRow(`
		SELECT COUNT(DISTINCT seq) 
		FROM report_analysis 
		WHERE seq > ? AND classification = 'physical' AND is_valid = TRUE
	`, lastSeq).Scan(&newPhysical)
	if err != nil {
		log.Errorf("Counter cache: failed to count new physical: %v", err)
		return
	}

	// Count new digital valid reports since lastSeq
	var newDigital int
	err = dbc.QueryRow(`
		SELECT COUNT(DISTINCT seq) 
		FROM report_analysis 
		WHERE seq > ? AND classification = 'digital' AND is_valid = TRUE
	`, lastSeq).Scan(&newDigital)
	if err != nil {
		log.Errorf("Counter cache: failed to count new digital: %v", err)
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
