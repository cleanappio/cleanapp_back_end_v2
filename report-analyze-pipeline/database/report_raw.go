package database

import (
	"database/sql"
	"log"
	"strings"
)

// ReportVisibility returns the visibility for a report if present, otherwise "public".
// If the table is missing (older DBs), this defaults to "public" to preserve legacy behavior.
func (d *Database) ReportVisibility(seq int) (string, error) {
	var vis sql.NullString
	err := d.db.QueryRow(`SELECT visibility FROM report_raw WHERE report_seq = ?`, seq).Scan(&vis)
	if err != nil {
		if err == sql.ErrNoRows {
			return "public", nil
		}
		// If report_raw doesn't exist yet, treat as legacy public.
		// MySQL error text is stable enough for this purpose.
		if strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
			return "public", nil
		}
		return "", err
	}
	if !vis.Valid || strings.TrimSpace(vis.String) == "" {
		return "public", nil
	}
	return strings.ToLower(strings.TrimSpace(vis.String)), nil
}

// MarkAnalysedPublished best-effort records that this report was published to report.analysed.
// This is used to make promotion-triggered publish idempotent and observable.
func (d *Database) MarkAnalysedPublished(seq int) {
	_, err := d.db.Exec(`UPDATE report_raw SET analysed_published_at = NOW() WHERE report_seq = ?`, seq)
	if err == nil {
		return
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "unknown column") || strings.Contains(low, "doesn't exist") {
		return
	}
	log.Printf("warn: failed to mark analysed_published_at seq=%d: %v", seq, err)
}
