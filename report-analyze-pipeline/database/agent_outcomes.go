package database

import (
	"context"
	"database/sql"
	"fmt"

	"cleanapp-common/reportoutcome"
)

// These wrappers keep source authorization and outcome transitions in the
// shared store. A queued body or generated analysis never grants permission.
func (d *Database) AgentOutcomeInput(ctx context.Context, seq int) (reportoutcome.Input, bool, error) {
	return reportoutcome.NewStore(d.db).Input(ctx, seq)
}

func (d *Database) CompleteAgentOutcome(ctx context.Context, seq int, analysis reportoutcome.Analysis) (reportoutcome.Record, error) {
	return reportoutcome.NewStore(d.db).Complete(ctx, seq, analysis)
}

func (d *Database) AgentPublicationPending(ctx context.Context, seq int) (bool, error) {
	return reportoutcome.NewStore(d.db).PublicationPending(ctx, seq)
}

func (d *Database) MarkAgentPublished(ctx context.Context, seq int) error {
	return reportoutcome.NewStore(d.db).MarkPublished(ctx, seq)
}

func (d *Database) MarkAgentPublicationFailed(ctx context.Context, seq int, reason string) error {
	return reportoutcome.NewStore(d.db).MarkPublicationFailed(ctx, seq, reason)
}

func (d *Database) PendingAgentOutcomes(ctx context.Context, limit int) ([]int, error) {
	return reportoutcome.NewStore(d.db).Pending(ctx, limit)
}

// Recovery needs canonical report fields, not an image blob or queued body.
func (d *Database) GetAgentOutcomeReport(ctx context.Context, seq int) (*Report, error) {
	var report Report
	var description sql.NullString
	err := d.db.QueryRowContext(ctx, `SELECT r.seq,r.ts,r.id,r.team,r.latitude,r.longitude,
		COALESCE(r.x,0),COALESCE(r.y,0),COALESCE(r.action_id,''),r.description,
		COALESCE(NULLIF(dsr.source_url,''),NULLIF(rd.url,''),''),
		COALESCE(dsr.shared_text,''),COALESCE(dsr.source_app,'')
		FROM reports r LEFT JOIN digital_share_reports dsr ON dsr.report_seq=r.seq
		LEFT JOIN report_details rd ON rd.seq=r.seq WHERE r.seq=?`, seq).
		Scan(&report.Seq, &report.Timestamp, &report.ID, &report.Team, &report.Latitude,
			&report.Longitude, &report.X, &report.Y, &report.ActionID, &description,
			&report.SourceURL, &report.SharedText, &report.SourceApp)
	if err != nil {
		return nil, fmt.Errorf("load canonical agent outcome report: %w", err)
	}
	report.Description = description.String
	return &report, nil
}

// LoadAgentReportAnalyses reuses completed provider work on private broker
// replays and outcome recovery. The English row proves that the normal analysis
// was saved; absent evidence stays version 0, never an excuse for another model
// pass. Existing translations are reused too, with a fixed result bound.
func (d *Database) LoadAgentReportAnalyses(ctx context.Context, seq int) ([]*ReportAnalysis, error) {
	if seq <= 0 {
		return nil, fmt.Errorf("invalid agent report sequence")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT ra.seq,ra.source,
		COALESCE(ra.analysis_text,''),COALESCE(ra.title,''),COALESCE(ra.description,''),
		COALESCE(ra.brand_name,''),COALESCE(ra.brand_display_name,''),
		COALESCE(ra.litter_probability,0),COALESCE(ra.hazard_probability,0),
		COALESCE(ra.digital_bug_probability,0),COALESCE(ra.severity_level,0),
		COALESCE(ra.summary,''),ra.language,COALESCE(ra.is_valid,FALSE),
		COALESCE(ra.classification,''),COALESCE(ra.inferred_contact_emails,''),
		COALESCE(ra.legal_risk_estimate,''),COALESCE(ev.report_kind,'unknown'),
		COALESCE(ev.evidence_excerpt,''),COALESCE(ev.reported_impact,''),
		COALESCE(ev.classification_reason,''),COALESCE(ev.entity_relationship,'unknown'),
		COALESCE(ev.evidence_version,0)
		FROM report_analysis ra LEFT JOIN report_evidence ev ON ev.seq=ra.seq
		WHERE ra.seq=? ORDER BY (ra.language='en') DESC,ra.updated_at DESC LIMIT 16`, seq)
	if err != nil {
		return nil, fmt.Errorf("load stored agent analysis: %w", err)
	}
	defer rows.Close()
	var result []*ReportAnalysis
	seen := map[string]bool{}
	hasEnglish := false
	for rows.Next() {
		a := &ReportAnalysis{}
		if err := rows.Scan(&a.Seq, &a.Source, &a.AnalysisText, &a.Title, &a.Description,
			&a.BrandName, &a.BrandDisplayName, &a.LitterProbability, &a.HazardProbability,
			&a.DigitalBugProbability, &a.SeverityLevel, &a.Summary, &a.Language,
			&a.IsValid, &a.Classification, &a.InferredContactEmails, &a.LegalRiskEstimate,
			&a.ReportKind, &a.EvidenceExcerpt, &a.ReportedImpact, &a.ClassificationReason,
			&a.EntityRelationship, &a.EvidenceVersion); err != nil {
			return nil, fmt.Errorf("read stored agent analysis: %w", err)
		}
		if seen[a.Language] {
			continue
		}
		seen[a.Language] = true
		hasEnglish = hasEnglish || a.Language == "en"
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !hasEnglish {
		return nil, nil
	}
	return result, nil
}
