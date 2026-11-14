package reportoutcome

import (
	"context"
	"database/sql"
	"errors"
)

// Check every bounded public analysis translation and evidence field; private
// analysis_text and inferred recipient addresses are deliberately excluded.
func publicAnalysisPrivacy(ctx context.Context, q queryer, seq int) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT COALESCE(title,''),COALESCE(description,''),COALESCE(summary,''),
 COALESCE(brand_name,''),COALESCE(brand_display_name,''),COALESCE(OCTET_LENGTH(analysis_image),0)
 FROM report_analysis WHERE seq=? ORDER BY language,source LIMIT 101`, seq)
	if err != nil {
		return "", err
	}
	count := 0
	hold := ""
	for rows.Next() {
		count++
		var title, description, summary, brand, display string
		var media int64
		if err = rows.Scan(&title, &description, &summary, &brand, &display, &media); err != nil {
			rows.Close()
			return "", err
		}
		if count > 100 {
			hold = "public_projection_too_large"
			break
		}
		if reason := PrivacyHold(nil, media, false, false, title, description, summary, brand, display); reason != "" {
			hold = reason
			break
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()
	if hold != "" {
		return hold, nil
	}
	var excerpt, impact, reason string
	err = q.QueryRowContext(ctx, `SELECT COALESCE(evidence_excerpt,''),COALESCE(reported_impact,''),COALESCE(classification_reason,'')
 FROM report_evidence WHERE seq=?`, seq).Scan(&excerpt, &impact, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return PrivacyHold(nil, 0, false, false, excerpt, impact, reason), nil
}
