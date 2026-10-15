package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// HumanWebIntake is persisted by the canonical ingress adapter. Neither model
// output nor a machine's claimed operator_type can authorize this policy.
type HumanWebIntake struct {
	Text        string
	HasLocation bool
}

func (d *Database) HumanWebIntake(ctx context.Context, seq int) (HumanWebIntake, bool, error) {
	var fetcher string
	var submissionID sql.NullString
	var actor, channel sql.NullString
	var reportJSON, extensionsJSON []byte
	err := d.db.QueryRowContext(ctx, `SELECT rr.fetcher_id, w.submission_id, w.actor_kind, w.channel
		FROM report_raw rr LEFT JOIN wire_submissions_raw w
		ON w.report_seq=rr.report_seq AND w.fetcher_id=rr.fetcher_id
		WHERE rr.report_seq=? ORDER BY w.created_at DESC LIMIT 1`, seq).
		Scan(&fetcher, &submissionID, &actor, &channel)
	if err == sql.ErrNoRows {
		return HumanWebIntake{}, false, nil
	}
	if err != nil {
		return HumanWebIntake{}, false, err
	}
	if fetcher != "human-web" {
		return HumanWebIntake{}, false, nil
	}
	// The queue can be consumed before the receipt transaction commits. Retry
	// rather than analyze a truncated legacy projection in that small window.
	if !actor.Valid || !channel.Valid {
		return HumanWebIntake{}, false, fmt.Errorf("web intake metadata is not persisted yet")
	}
	if actor.String != "human" || channel.String != "web" {
		return HumanWebIntake{}, false, nil
	}
	// Sort narrow metadata only, then hydrate the selected original. Sorting
	// report JSON can exceed MySQL's sort buffer for large retained images.
	if err := d.db.QueryRowContext(ctx, `SELECT report_json, extensions_json FROM wire_submissions_raw WHERE submission_id=? AND report_seq=?`, submissionID.String, seq).Scan(&reportJSON, &extensionsJSON); err != nil {
		return HumanWebIntake{}, false, err
	}
	var extensions struct {
		PendingAnalysis bool `json:"web_intake_pending_analysis"`
	}
	if err := json.Unmarshal(extensionsJSON, &extensions); err != nil {
		return HumanWebIntake{}, false, err
	}
	if !extensions.PendingAnalysis {
		return HumanWebIntake{}, false, nil
	}
	var report struct {
		ProblemType string           `json:"problem_type"`
		Description string           `json:"description"`
		Location    *json.RawMessage `json:"location"`
	}
	if err := json.Unmarshal(reportJSON, &report); err != nil {
		return HumanWebIntake{}, false, err
	}
	if report.ProblemType != "citizen_submission" {
		return HumanWebIntake{}, false, nil
	}
	return HumanWebIntake{Text: report.Description, HasLocation: report.Location != nil}, true, nil
}

// Promotion changes visibility only after the existing analyzer establishes a
// valid firsthand problem. Intake remains unverified; this is not independent
// corroboration or a guarantee of resolution.
func (d *Database) PublishAnalyzedHumanWebIntake(ctx context.Context, seq int) error {
	_, err := d.db.ExecContext(ctx, `UPDATE report_raw rr JOIN wire_submissions_raw w
		ON w.report_seq=rr.report_seq AND w.fetcher_id=rr.fetcher_id
		SET rr.visibility='public', rr.trust_level='unverified',
		rr.promoted_to_public_at=COALESCE(rr.promoted_to_public_at,NOW())
		WHERE rr.report_seq=? AND rr.visibility='shadow' AND rr.fetcher_id='human-web'
		AND w.actor_kind='human' AND w.channel='web'
		AND JSON_EXTRACT(w.extensions_json,'$.web_intake_pending_analysis')=TRUE
		AND JSON_UNQUOTE(JSON_EXTRACT(w.report_json,'$.problem_type'))='citizen_submission'`, seq)
	return err
}
