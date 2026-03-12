package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"report-listener/models"
)

func (d *Database) ListReportEscalationTargets(ctx context.Context, reportSeq int) ([]models.CaseEscalationTarget, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			id, report_seq, role_type, COALESCE(decision_scope, ''), COALESCE(organization, ''), COALESCE(display_name, ''),
			COALESCE(channel, ''), COALESCE(email, ''), COALESCE(phone, ''), COALESCE(website, ''),
			COALESCE(contact_url, ''), COALESCE(social_platform, ''), COALESCE(social_handle, ''),
			COALESCE(source_url, ''), COALESCE(evidence_text, ''), COALESCE(verification_level, ''),
			COALESCE(attribution_class, ''), COALESCE(target_source, ''), confidence_score, actionability_score,
			notify_tier, COALESCE(send_eligibility, ''), COALESCE(rationale, ''), COALESCE(reason_selected, ''),
			created_at
		FROM report_escalation_targets
		WHERE report_seq = ?
		ORDER BY notify_tier ASC, actionability_score DESC, confidence_score DESC, created_at ASC
	`, reportSeq)
	if err != nil {
		return nil, fmt.Errorf("list report escalation targets: %w", err)
	}
	defer rows.Close()

	items := make([]models.CaseEscalationTarget, 0)
	for rows.Next() {
		var (
			item      models.CaseEscalationTarget
			storedSeq int
		)
		if err := rows.Scan(
			&item.ID,
			&storedSeq,
			&item.RoleType,
			&item.DecisionScope,
			&item.Organization,
			&item.DisplayName,
			&item.Channel,
			&item.Email,
			&item.Phone,
			&item.Website,
			&item.ContactURL,
			&item.SocialPlatform,
			&item.SocialHandle,
			&item.SourceURL,
			&item.EvidenceText,
			&item.Verification,
			&item.AttributionClass,
			&item.TargetSource,
			&item.ConfidenceScore,
			&item.ActionabilityScore,
			&item.NotifyTier,
			&item.SendEligibility,
			&item.Rationale,
			&item.ReasonSelected,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = storedSeq
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) ReplaceReportEscalationTargets(ctx context.Context, reportSeq int, targets []models.CaseEscalationTarget) ([]models.CaseEscalationTarget, error) {
	if reportSeq <= 0 {
		return nil, fmt.Errorf("report_seq is required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM report_escalation_targets WHERE report_seq = ?`, reportSeq); err != nil {
		return nil, fmt.Errorf("clear report escalation targets: %w", err)
	}

	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		key := strings.ToLower(strings.TrimSpace(target.Email))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(target.Phone))
		}
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(target.DisplayName)) + "|" + strings.ToLower(strings.TrimSpace(target.Organization))
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO report_escalation_targets (
				report_seq, role_type, decision_scope, organization, display_name,
				channel, email, phone, website, contact_url, social_platform, social_handle,
				source_url, evidence_text, verification_level, attribution_class, target_source,
				confidence_score, actionability_score, notify_tier, send_eligibility, rationale, reason_selected
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			reportSeq,
			emptyOrDefault(target.RoleType, "contact"),
			emptyOrDefault(target.DecisionScope, "other"),
			emptyOrNil(target.Organization),
			emptyOrNil(target.DisplayName),
			emptyOrDefault(target.Channel, caseEscalationTargetFallbackChannelForDB(target)),
			emptyOrNil(target.Email),
			emptyOrNil(target.Phone),
			emptyOrNil(target.Website),
			emptyOrNil(target.ContactURL),
			emptyOrNil(target.SocialPlatform),
			emptyOrNil(target.SocialHandle),
			emptyOrNil(target.SourceURL),
			emptyOrNil(target.EvidenceText),
			emptyOrNil(target.Verification),
			emptyOrDefault(target.AttributionClass, "heuristic"),
			emptyOrDefault(target.TargetSource, "suggested"),
			target.ConfidenceScore,
			target.ActionabilityScore,
			target.NotifyTier,
			emptyOrDefault(target.SendEligibility, "review"),
			emptyOrNil(target.Rationale),
			emptyOrNil(target.ReasonSelected),
		); err != nil {
			return nil, fmt.Errorf("insert report escalation target: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.ListReportEscalationTargets(ctx, reportSeq)
}

func caseEscalationTargetFallbackChannelForDB(target models.CaseEscalationTarget) string {
	switch {
	case strings.TrimSpace(target.Channel) != "":
		return strings.TrimSpace(target.Channel)
	case strings.TrimSpace(target.Email) != "":
		return "email"
	case strings.TrimSpace(target.Phone) != "":
		return "phone"
	case strings.TrimSpace(target.SocialHandle) != "":
		return "social"
	case strings.TrimSpace(target.Website) != "" || strings.TrimSpace(target.ContactURL) != "":
		return "website"
	default:
		return "email"
	}
}

func (d *Database) ReportEscalationTargetsFresh(ctx context.Context, reportSeq int, ttlSeconds int) (bool, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	var lastUpdated sql.NullTime
	if err := d.db.QueryRowContext(ctx, `
		SELECT MAX(updated_at)
		FROM report_escalation_targets
		WHERE report_seq = ?
	`, reportSeq).Scan(&lastUpdated); err != nil {
		return false, fmt.Errorf("load report escalation target freshness: %w", err)
	}
	if !lastUpdated.Valid {
		return false, nil
	}
	return time.Since(lastUpdated.Time) < time.Duration(ttlSeconds)*time.Second, nil
}
