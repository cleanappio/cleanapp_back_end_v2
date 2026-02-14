package database

import (
	"database/sql"
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
