package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"report-listener/models"
)

func (d *Database) listCaseContactObservations(ctx context.Context, caseID string) ([]models.CaseContactObservation, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			id, case_id, role_type, decision_scope, COALESCE(organization_name, ''),
			COALESCE(person_name, ''), COALESCE(channel_type, ''), COALESCE(channel_value, ''),
			COALESCE(email, ''), COALESCE(phone, ''), COALESCE(website, ''), COALESCE(contact_url, ''),
			COALESCE(social_platform, ''), COALESCE(social_handle, ''), COALESCE(source_url, ''),
			COALESCE(evidence_text, ''), COALESCE(verification_level, ''), COALESCE(attribution_class, ''),
			confidence_score, COALESCE(target_source, ''), discovered_at
		FROM case_contact_observations
		WHERE case_id = ?
		ORDER BY confidence_score DESC, discovered_at ASC, id ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list case contact observations: %w", err)
	}
	defer rows.Close()

	items := make([]models.CaseContactObservation, 0)
	for rows.Next() {
		var item models.CaseContactObservation
		if err := rows.Scan(
			&item.ID, &item.CaseID, &item.RoleType, &item.DecisionScope,
			&item.OrganizationName, &item.PersonName, &item.ChannelType, &item.ChannelValue,
			&item.Email, &item.Phone, &item.Website, &item.ContactURL,
			&item.SocialPlatform, &item.SocialHandle, &item.SourceURL,
			&item.EvidenceText, &item.Verification, &item.AttributionClass,
			&item.ConfidenceScore, &item.TargetSource, &item.DiscoveredAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) ReplaceCaseContactObservations(ctx context.Context, caseID string, observations []models.CaseContactObservation) ([]models.CaseContactObservation, error) {
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("case_id is required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM case_contact_observations WHERE case_id = ?`, caseID); err != nil {
		return nil, fmt.Errorf("clear case contact observations: %w", err)
	}

	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		if !hasCaseContactObservationMethod(observation) {
			continue
		}
		key := caseContactObservationDedupKey(observation)
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		channelValue := strings.TrimSpace(observation.ChannelValue)
		if channelValue == "" {
			channelValue = caseContactObservationChannelValue(observation)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_contact_observations (
				case_id, role_type, decision_scope, organization_name, person_name,
				channel_type, channel_value, email, phone, website, contact_url,
				social_platform, social_handle, source_url, evidence_text, verification_level,
				attribution_class, confidence_score, target_source
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			caseID,
			emptyOrDefault(observation.RoleType, "contact"),
			emptyOrDefault(observation.DecisionScope, "other"),
			emptyOrNil(observation.OrganizationName),
			emptyOrNil(observation.PersonName),
			emptyOrDefault(observation.ChannelType, "email"),
			emptyOrNil(channelValue),
			emptyOrNil(observation.Email),
			emptyOrNil(observation.Phone),
			emptyOrNil(observation.Website),
			emptyOrNil(observation.ContactURL),
			emptyOrNil(observation.SocialPlatform),
			emptyOrNil(observation.SocialHandle),
			emptyOrNil(observation.SourceURL),
			emptyOrNil(observation.EvidenceText),
			emptyOrNil(observation.Verification),
			emptyOrDefault(observation.AttributionClass, "heuristic"),
			observation.ConfidenceScore,
			emptyOrDefault(observation.TargetSource, "suggested"),
		); err != nil {
			return nil, fmt.Errorf("insert case contact observation: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.listCaseContactObservations(ctx, caseID)
}

func (d *Database) getActiveCaseNotifyPlan(ctx context.Context, caseID string) (*models.CaseNotifyPlan, error) {
	var plan models.CaseNotifyPlan
	err := d.db.QueryRowContext(ctx, `
		SELECT id, case_id, plan_version, hazard_mode, status, COALESCE(summary, ''), created_at, updated_at
		FROM case_notify_plans
		WHERE case_id = ?
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, updated_at DESC, id DESC
		LIMIT 1
	`, caseID).Scan(
		&plan.ID, &plan.CaseID, &plan.PlanVersion, &plan.HazardMode, &plan.Status, &plan.Summary, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load case notify plan: %w", err)
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, plan_id, target_id, observation_id, wave_number, priority_rank, role_type,
			decision_scope, actionability_score, send_eligibility, COALESCE(reason_selected, ''),
			selected, created_at
		FROM case_notify_plan_items
		WHERE plan_id = ?
		ORDER BY wave_number ASC, priority_rank ASC, actionability_score DESC, id ASC
	`, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("load case notify plan items: %w", err)
	}
	defer rows.Close()

	items := make([]models.CaseNotifyPlanItem, 0)
	for rows.Next() {
		var item models.CaseNotifyPlanItem
		var targetID sql.NullInt64
		var observationID sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.PlanID, &targetID, &observationID, &item.WaveNumber,
			&item.PriorityRank, &item.RoleType, &item.DecisionScope, &item.ActionabilityScore,
			&item.SendEligibility, &item.ReasonSelected, &item.Selected, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if targetID.Valid {
			item.TargetID = &targetID.Int64
		}
		if observationID.Valid {
			item.ObservationID = &observationID.Int64
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	plan.Items = items
	return &plan, nil
}

func (d *Database) ReplaceCaseNotifyPlan(ctx context.Context, caseID string, plan *models.CaseNotifyPlan) (*models.CaseNotifyPlan, error) {
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("case_id is required")
	}
	if plan == nil {
		return d.getActiveCaseNotifyPlan(ctx, caseID)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		planID  int64
		version int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, plan_version
		FROM case_notify_plans
		WHERE case_id = ? AND status = 'active'
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, caseID).Scan(&planID, &version)
	switch {
	case err == sql.ErrNoRows:
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(plan_version), 0) + 1
			FROM case_notify_plans
			WHERE case_id = ?
		`, caseID).Scan(&version); err != nil {
			return nil, fmt.Errorf("load next case notify plan version: %w", err)
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO case_notify_plans (case_id, plan_version, hazard_mode, status, summary)
			VALUES (?, ?, ?, 'active', ?)
		`, caseID, version, emptyOrDefault(plan.HazardMode, "standard"), emptyOrNil(plan.Summary))
		if err != nil {
			return nil, fmt.Errorf("insert case notify plan: %w", err)
		}
		planID, err = result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("get case notify plan id: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("load active case notify plan: %w", err)
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_notify_plans
			SET hazard_mode = ?, summary = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, emptyOrDefault(plan.HazardMode, "standard"), emptyOrNil(plan.Summary), planID); err != nil {
			return nil, fmt.Errorf("update case notify plan: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_notify_plans
			SET status = 'superseded'
			WHERE case_id = ? AND status = 'active' AND id <> ?
		`, caseID, planID); err != nil {
			return nil, fmt.Errorf("supersede stale case notify plans: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM case_notify_plan_items WHERE plan_id = ?`, planID); err != nil {
		return nil, fmt.Errorf("clear case notify plan items: %w", err)
	}

	for _, item := range plan.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_notify_plan_items (
				plan_id, target_id, observation_id, wave_number, priority_rank, role_type,
				decision_scope, actionability_score, send_eligibility, reason_selected, selected
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			planID, item.TargetID, item.ObservationID, item.WaveNumber, item.PriorityRank,
			emptyOrDefault(item.RoleType, "contact"), emptyOrDefault(item.DecisionScope, "other"),
			item.ActionabilityScore, emptyOrDefault(item.SendEligibility, "review"),
			emptyOrNil(item.ReasonSelected), item.Selected,
		); err != nil {
			return nil, fmt.Errorf("insert case notify plan item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.getActiveCaseNotifyPlan(ctx, caseID)
}

func hasCaseContactObservationMethod(observation models.CaseContactObservation) bool {
	return strings.TrimSpace(observation.Email) != "" ||
		strings.TrimSpace(observation.Phone) != "" ||
		strings.TrimSpace(observation.Website) != "" ||
		strings.TrimSpace(observation.ContactURL) != "" ||
		strings.TrimSpace(observation.SocialHandle) != "" ||
		strings.TrimSpace(observation.ChannelValue) != ""
}

func caseContactObservationChannelValue(observation models.CaseContactObservation) string {
	switch strings.ToLower(strings.TrimSpace(observation.ChannelType)) {
	case "email":
		return strings.TrimSpace(observation.Email)
	case "phone":
		return strings.TrimSpace(observation.Phone)
	case "social":
		if handle := strings.TrimSpace(observation.SocialHandle); handle != "" {
			return handle
		}
	case "website":
		if website := strings.TrimSpace(observation.Website); website != "" {
			return website
		}
		if contactURL := strings.TrimSpace(observation.ContactURL); contactURL != "" {
			return contactURL
		}
	}
	if value := strings.TrimSpace(observation.ChannelValue); value != "" {
		return value
	}
	return ""
}

func caseContactObservationDedupKey(observation models.CaseContactObservation) string {
	channelType := strings.ToLower(strings.TrimSpace(observation.ChannelType))
	channelValue := strings.ToLower(strings.TrimSpace(caseContactObservationChannelValue(observation)))
	if channelType != "" && channelValue != "" {
		return channelType + ":" + channelValue
	}
	org := strings.ToLower(strings.TrimSpace(observation.OrganizationName))
	person := strings.ToLower(strings.TrimSpace(observation.PersonName))
	if org != "" || person != "" {
		return "name:" + org + ":" + person
	}
	return ""
}
