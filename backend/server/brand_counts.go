package server

import (
	"database/sql"
	"time"

	"github.com/apex/log"
)

const brandCountsStateName = "brand_report_counts"

var brandCountsLastSeq int

// StartBrandReportCountsUpdater maintains materialized per-brand report counts.
// Readers (email-service, dashboards, etc.) can query brand_report_counts for O(1)
// lookup rather than running expensive aggregate scans.
func StartBrandReportCountsUpdater() {
	log.Info("Starting brand report counts updater...")

	go func() {
		// Wait a bit for DB to be ready
		time.Sleep(7 * time.Second)
		updateBrandReportCounts()

		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			updateBrandReportCounts()
		}
	}()
}

func updateBrandReportCounts() {
	start := time.Now()

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("Brand counts: failed to connect to DB: %v", err)
		return
	}

	// Get current max seq
	var maxSeq int
	if err := dbc.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM report_analysis").Scan(&maxSeq); err != nil {
		log.Errorf("Brand counts: failed to get max seq: %v", err)
		return
	}
	if maxSeq <= 0 {
		log.Infof("Brand counts: no report_analysis rows yet")
		return
	}

	// Load persisted state once (best-effort).
	if brandCountsLastSeq == 0 {
		var last int
		if err := dbc.QueryRow("SELECT last_seq FROM counters_state WHERE name = ?", brandCountsStateName).Scan(&last); err == nil && last > 0 {
			brandCountsLastSeq = last
		}
	}

	// First run (no state): do a full recompute (no COUNT DISTINCT; grouped by seq).
	if brandCountsLastSeq <= 0 {
		log.Infof("Brand counts: initializing full recompute (max_seq=%d)", maxSeq)
		if err := fullRecomputeBrandReportCounts(dbc); err != nil {
			log.Warnf("Brand counts: full recompute failed: %v", err)
			return
		}
		brandCountsLastSeq = maxSeq
		_, _ = dbc.Exec(`
			INSERT INTO counters_state (name, last_seq) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE last_seq = VALUES(last_seq)
		`, brandCountsStateName, brandCountsLastSeq)
		log.Infof("Brand counts: full recompute complete in %s (last_seq=%d)", time.Since(start), brandCountsLastSeq)
		return
	}

	if maxSeq <= brandCountsLastSeq {
		log.Infof("Brand counts: no new reports (max=%d, last=%d)", maxSeq, brandCountsLastSeq)
		return
	}

	prevLast := brandCountsLastSeq
	if err := incrementalUpdateBrandReportCounts(dbc, brandCountsLastSeq, maxSeq); err != nil {
		log.Warnf("Brand counts: incremental update failed: %v", err)
		return
	}

	brandCountsLastSeq = maxSeq
	_, _ = dbc.Exec(`
		INSERT INTO counters_state (name, last_seq) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE last_seq = VALUES(last_seq)
	`, brandCountsStateName, brandCountsLastSeq)

	log.Infof("Brand counts: incremental update complete in %s (counted seq %d to %d)",
		time.Since(start), prevLast, maxSeq)
}

func fullRecomputeBrandReportCounts(dbc Execer) error {
	// Writes absolute counts. Does not delete brands that disappeared, but that is rare
	// and can be handled by an occasional manual rebuild if needed.
	_, err := dbc.Exec(`
		INSERT INTO brand_report_counts (brand_name, language, total_valid, physical_valid, digital_valid)
		SELECT
			brand_name,
			'en' AS language,
			COUNT(*) AS total_valid,
			COALESCE(SUM(has_physical), 0) AS physical_valid,
			COALESCE(SUM(has_digital), 0) AS digital_valid
		FROM (
			SELECT
				ra.seq,
				ra.brand_name,
				MAX(CASE WHEN ra.classification = 'physical' THEN 1 ELSE 0 END) AS has_physical,
				MAX(CASE WHEN ra.classification = 'digital' THEN 1 ELSE 0 END) AS has_digital
			FROM report_analysis ra
			LEFT JOIN report_raw rr ON rr.report_seq = ra.seq
			WHERE ra.is_valid = TRUE
			  AND ra.language = 'en'
			  AND ra.brand_name IS NOT NULL
			  AND ra.brand_name <> ''
			  AND (rr.visibility IS NULL OR rr.visibility = 'public')
			GROUP BY ra.seq, ra.brand_name
		) grouped
		GROUP BY brand_name
		ON DUPLICATE KEY UPDATE
			total_valid = VALUES(total_valid),
			physical_valid = VALUES(physical_valid),
			digital_valid = VALUES(digital_valid),
			updated_at = CURRENT_TIMESTAMP
	`)
	return err
}

func incrementalUpdateBrandReportCounts(dbc Execer, lastSeq, maxSeq int) error {
	_, err := dbc.Exec(`
		INSERT INTO brand_report_counts (brand_name, language, total_valid, physical_valid, digital_valid)
		SELECT
			brand_name,
			'en' AS language,
			COUNT(*) AS total_valid,
			COALESCE(SUM(has_physical), 0) AS physical_valid,
			COALESCE(SUM(has_digital), 0) AS digital_valid
		FROM (
			SELECT
				ra.seq,
				ra.brand_name,
				MAX(CASE WHEN ra.classification = 'physical' THEN 1 ELSE 0 END) AS has_physical,
				MAX(CASE WHEN ra.classification = 'digital' THEN 1 ELSE 0 END) AS has_digital
			FROM report_analysis ra
			LEFT JOIN report_raw rr ON rr.report_seq = ra.seq
			WHERE ra.seq > ? AND ra.seq <= ?
			  AND ra.is_valid = TRUE
			  AND ra.language = 'en'
			  AND ra.brand_name IS NOT NULL
			  AND ra.brand_name <> ''
			  AND (rr.visibility IS NULL OR rr.visibility = 'public')
			GROUP BY ra.seq, ra.brand_name
		) grouped
		GROUP BY brand_name
		ON DUPLICATE KEY UPDATE
			total_valid = total_valid + VALUES(total_valid),
			physical_valid = physical_valid + VALUES(physical_valid),
			digital_valid = digital_valid + VALUES(digital_valid),
			updated_at = CURRENT_TIMESTAMP
	`, lastSeq, maxSeq)
	return err
}

// Execer is satisfied by *sql.DB and *sql.Tx.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}
