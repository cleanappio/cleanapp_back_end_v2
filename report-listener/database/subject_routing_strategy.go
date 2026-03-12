package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"report-listener/models"
)

func (d *Database) UpsertSubjectRoutingProfile(ctx context.Context, profile models.SubjectRoutingProfile) (*models.SubjectRoutingProfile, error) {
	if strings.TrimSpace(profile.SubjectKind) == "" || strings.TrimSpace(profile.SubjectRef) == "" {
		return nil, fmt.Errorf("subject routing profile requires subject_kind and subject_ref")
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO subject_routing_profiles (
			subject_kind, subject_ref, classification, defect_class, defect_mode, asset_class,
			jurisdiction_key, exposure_mode, severity_band, urgency_band, context_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))
		ON DUPLICATE KEY UPDATE
			classification = VALUES(classification),
			defect_class = VALUES(defect_class),
			defect_mode = VALUES(defect_mode),
			asset_class = VALUES(asset_class),
			jurisdiction_key = VALUES(jurisdiction_key),
			exposure_mode = VALUES(exposure_mode),
			severity_band = VALUES(severity_band),
			urgency_band = VALUES(urgency_band),
			context_json = VALUES(context_json),
			refreshed_at = CURRENT_TIMESTAMP
	`,
		profile.SubjectKind,
		profile.SubjectRef,
		emptyOrDefault(profile.Classification, "physical"),
		emptyOrDefault(profile.DefectClass, "general_defect"),
		emptyOrDefault(profile.DefectMode, "standard"),
		emptyOrDefault(profile.AssetClass, "general_site"),
		emptyOrDefault(profile.JurisdictionKey, ""),
		emptyOrDefault(profile.ExposureMode, "localized"),
		emptyOrDefault(profile.SeverityBand, "medium"),
		emptyOrDefault(profile.UrgencyBand, "medium"),
		emptyOrDefault(profile.ContextJSON, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert subject routing profile: %w", err)
	}
	return d.GetSubjectRoutingProfile(ctx, profile.SubjectKind, profile.SubjectRef)
}

func (d *Database) GetSubjectRoutingProfile(ctx context.Context, subjectKind, subjectRef string) (*models.SubjectRoutingProfile, error) {
	if strings.TrimSpace(subjectKind) == "" || strings.TrimSpace(subjectRef) == "" {
		return nil, nil
	}
	var profile models.SubjectRoutingProfile
	err := d.db.QueryRowContext(ctx, `
		SELECT id, subject_kind, subject_ref, classification, defect_class, defect_mode, asset_class,
			COALESCE(jurisdiction_key, ''), COALESCE(exposure_mode, ''), COALESCE(severity_band, ''),
			COALESCE(urgency_band, ''), COALESCE(CAST(context_json AS CHAR), ''), refreshed_at
		FROM subject_routing_profiles
		WHERE subject_kind = ? AND subject_ref = ?
	`, subjectKind, subjectRef).Scan(
		&profile.ID,
		&profile.SubjectKind,
		&profile.SubjectRef,
		&profile.Classification,
		&profile.DefectClass,
		&profile.DefectMode,
		&profile.AssetClass,
		&profile.JurisdictionKey,
		&profile.ExposureMode,
		&profile.SeverityBand,
		&profile.UrgencyBand,
		&profile.ContextJSON,
		&profile.RefreshedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get subject routing profile: %w", err)
	}
	return &profile, nil
}

func (d *Database) ReplaceNotifyExecutionTasks(ctx context.Context, subjectKind, subjectRef string, tasks []models.NotifyExecutionTask) ([]models.NotifyExecutionTask, error) {
	if strings.TrimSpace(subjectKind) == "" || strings.TrimSpace(subjectRef) == "" {
		return nil, fmt.Errorf("notify execution tasks require subject_kind and subject_ref")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM notify_execution_tasks WHERE subject_kind = ? AND subject_ref = ? AND task_status = 'pending'`, subjectKind, subjectRef); err != nil {
		return nil, fmt.Errorf("clear notify execution tasks: %w", err)
	}
	for _, task := range tasks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notify_execution_tasks (
				subject_kind, subject_ref, target_id, wave_number, role_type, channel_type,
				execution_mode, task_status, summary, payload_json, assigned_user_id, due_at, completed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
		`,
			subjectKind,
			subjectRef,
			task.TargetID,
			task.WaveNumber,
			emptyOrDefault(task.RoleType, "contact"),
			emptyOrDefault(task.ChannelType, "email"),
			emptyOrDefault(task.ExecutionMode, "review"),
			emptyOrDefault(task.TaskStatus, "pending"),
			emptyOrDefault(task.Summary, ""),
			emptyOrDefault(task.PayloadJSON, ""),
			emptyOrDefault(task.AssignedUserID, ""),
			task.DueAt,
			task.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("insert notify execution task: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.ListNotifyExecutionTasks(ctx, subjectKind, subjectRef)
}

func (d *Database) ListNotifyExecutionTasks(ctx context.Context, subjectKind, subjectRef string) ([]models.NotifyExecutionTask, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, subject_kind, subject_ref, target_id, wave_number, role_type, channel_type,
			execution_mode, task_status, COALESCE(summary, ''), COALESCE(CAST(payload_json AS CHAR), ''),
			COALESCE(assigned_user_id, ''), due_at, completed_at, created_at, updated_at
		FROM notify_execution_tasks
		WHERE subject_kind = ? AND subject_ref = ?
		ORDER BY CASE task_status WHEN 'pending' THEN 0 WHEN 'in_progress' THEN 1 ELSE 2 END, wave_number ASC, id ASC
	`, subjectKind, subjectRef)
	if err != nil {
		return nil, fmt.Errorf("list notify execution tasks: %w", err)
	}
	defer rows.Close()
	items := make([]models.NotifyExecutionTask, 0)
	for rows.Next() {
		var item models.NotifyExecutionTask
		var targetID sql.NullInt64
		var dueAt sql.NullTime
		var completedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.SubjectKind,
			&item.SubjectRef,
			&targetID,
			&item.WaveNumber,
			&item.RoleType,
			&item.ChannelType,
			&item.ExecutionMode,
			&item.TaskStatus,
			&item.Summary,
			&item.PayloadJSON,
			&item.AssignedUserID,
			&dueAt,
			&completedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if targetID.Valid {
			item.TargetID = &targetID.Int64
		}
		if dueAt.Valid {
			item.DueAt = &dueAt.Time
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) GetNotifyExecutionTask(ctx context.Context, subjectKind, subjectRef string, taskID int64) (*models.NotifyExecutionTask, error) {
	if strings.TrimSpace(subjectKind) == "" || strings.TrimSpace(subjectRef) == "" || taskID <= 0 {
		return nil, nil
	}
	row := d.db.QueryRowContext(ctx, `
		SELECT id, subject_kind, subject_ref, target_id, wave_number, role_type, channel_type,
			execution_mode, task_status, COALESCE(summary, ''), COALESCE(CAST(payload_json AS CHAR), ''),
			COALESCE(assigned_user_id, ''), due_at, completed_at, created_at, updated_at
		FROM notify_execution_tasks
		WHERE id = ? AND subject_kind = ? AND subject_ref = ?
	`, taskID, subjectKind, subjectRef)
	var item models.NotifyExecutionTask
	var targetID sql.NullInt64
	var dueAt sql.NullTime
	var completedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.SubjectKind,
		&item.SubjectRef,
		&targetID,
		&item.WaveNumber,
		&item.RoleType,
		&item.ChannelType,
		&item.ExecutionMode,
		&item.TaskStatus,
		&item.Summary,
		&item.PayloadJSON,
		&item.AssignedUserID,
		&dueAt,
		&completedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get notify execution task: %w", err)
	}
	if targetID.Valid {
		item.TargetID = &targetID.Int64
	}
	if dueAt.Valid {
		item.DueAt = &dueAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return &item, nil
}

func (d *Database) UpdateNotifyExecutionTask(ctx context.Context, subjectKind, subjectRef string, taskID int64, taskStatus, assignedUserID string, completedAt *time.Time) (*models.NotifyExecutionTask, error) {
	if strings.TrimSpace(subjectKind) == "" || strings.TrimSpace(subjectRef) == "" || taskID <= 0 {
		return nil, fmt.Errorf("notify execution task update requires subject and task id")
	}
	if _, err := d.db.ExecContext(ctx, `
		UPDATE notify_execution_tasks
		SET task_status = ?, assigned_user_id = NULLIF(?, ''), completed_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND subject_kind = ? AND subject_ref = ?
	`,
		emptyOrDefault(taskStatus, "completed"),
		emptyOrDefault(assignedUserID, ""),
		completedAt,
		taskID,
		subjectKind,
		subjectRef,
	); err != nil {
		return nil, fmt.Errorf("update notify execution task: %w", err)
	}
	return d.GetNotifyExecutionTask(ctx, subjectKind, subjectRef, taskID)
}

func (d *Database) RecordNotifyOutcome(ctx context.Context, outcome models.NotifyOutcome) error {
	if strings.TrimSpace(outcome.SubjectKind) == "" || strings.TrimSpace(outcome.SubjectRef) == "" {
		return fmt.Errorf("notify outcome requires subject_kind and subject_ref")
	}
	if strings.TrimSpace(outcome.OutcomeType) == "" {
		return fmt.Errorf("notify outcome requires outcome_type")
	}
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO notify_outcomes (
			subject_kind, subject_ref, target_id, endpoint_key, outcome_type, source_type, source_ref, evidence_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))
	`,
		outcome.SubjectKind,
		outcome.SubjectRef,
		outcome.TargetID,
		emptyOrDefault(outcome.EndpointKey, ""),
		outcome.OutcomeType,
		emptyOrDefault(outcome.SourceType, ""),
		emptyOrDefault(outcome.SourceRef, ""),
		emptyOrDefault(outcome.EvidenceJSON, ""),
	); err != nil {
		return fmt.Errorf("insert notify outcome: %w", err)
	}
	return nil
}

func (d *Database) ListNotifyOutcomes(ctx context.Context, subjectKind, subjectRef string) ([]models.NotifyOutcome, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, subject_kind, subject_ref, target_id, COALESCE(endpoint_key, ''), outcome_type,
			COALESCE(source_type, ''), COALESCE(source_ref, ''), COALESCE(CAST(evidence_json AS CHAR), ''), recorded_at
		FROM notify_outcomes
		WHERE subject_kind = ? AND subject_ref = ?
		ORDER BY recorded_at DESC, id DESC
	`, subjectKind, subjectRef)
	if err != nil {
		return nil, fmt.Errorf("list notify outcomes: %w", err)
	}
	defer rows.Close()
	items := make([]models.NotifyOutcome, 0)
	for rows.Next() {
		var item models.NotifyOutcome
		var targetID sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.SubjectKind,
			&item.SubjectRef,
			&targetID,
			&item.EndpointKey,
			&item.OutcomeType,
			&item.SourceType,
			&item.SourceRef,
			&item.EvidenceJSON,
			&item.RecordedAt,
		); err != nil {
			return nil, err
		}
		if targetID.Valid {
			item.TargetID = &targetID.Int64
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) UpsertContactEndpointMemory(ctx context.Context, memory models.ContactEndpointMemory) (*models.ContactEndpointMemory, error) {
	if strings.TrimSpace(memory.EndpointKey) == "" || strings.TrimSpace(memory.ChannelValue) == "" {
		return nil, fmt.Errorf("endpoint memory requires endpoint_key and channel_value")
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO contact_endpoint_memory (
			endpoint_key, organization_key, channel_type, channel_value, last_result,
			success_count, bounce_count, ack_count, fix_count, misroute_count, no_response_count,
			last_contacted_at, cooldown_until, preferred_for_role_type, preferred_for_asset_class
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			organization_key = VALUES(organization_key),
			channel_type = VALUES(channel_type),
			channel_value = VALUES(channel_value),
			last_result = VALUES(last_result),
			success_count = GREATEST(success_count, VALUES(success_count)),
			bounce_count = GREATEST(bounce_count, VALUES(bounce_count)),
			ack_count = GREATEST(ack_count, VALUES(ack_count)),
			fix_count = GREATEST(fix_count, VALUES(fix_count)),
			misroute_count = GREATEST(misroute_count, VALUES(misroute_count)),
			no_response_count = GREATEST(no_response_count, VALUES(no_response_count)),
			last_contacted_at = VALUES(last_contacted_at),
			cooldown_until = VALUES(cooldown_until),
			preferred_for_role_type = VALUES(preferred_for_role_type),
			preferred_for_asset_class = VALUES(preferred_for_asset_class),
			updated_at = CURRENT_TIMESTAMP
	`,
		memory.EndpointKey,
		emptyOrDefault(memory.OrganizationKey, ""),
		emptyOrDefault(memory.ChannelType, "email"),
		memory.ChannelValue,
		emptyOrDefault(memory.LastResult, ""),
		memory.SuccessCount,
		memory.BounceCount,
		memory.AckCount,
		memory.FixCount,
		memory.MisrouteCount,
		memory.NoResponseCount,
		memory.LastContactedAt,
		memory.CooldownUntil,
		emptyOrDefault(memory.PreferredForRoleType, ""),
		emptyOrDefault(memory.PreferredForAssetClass, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert contact endpoint memory: %w", err)
	}
	return d.GetContactEndpointMemory(ctx, memory.EndpointKey)
}

func (d *Database) GetContactEndpointMemory(ctx context.Context, endpointKey string) (*models.ContactEndpointMemory, error) {
	if strings.TrimSpace(endpointKey) == "" {
		return nil, nil
	}
	var item models.ContactEndpointMemory
	var lastContactedAt sql.NullTime
	var cooldownUntil sql.NullTime
	err := d.db.QueryRowContext(ctx, `
		SELECT id, endpoint_key, COALESCE(organization_key, ''), channel_type, channel_value, COALESCE(last_result, ''),
			success_count, bounce_count, ack_count, fix_count, misroute_count, no_response_count,
			last_contacted_at, cooldown_until, COALESCE(preferred_for_role_type, ''), COALESCE(preferred_for_asset_class, ''),
			updated_at
		FROM contact_endpoint_memory
		WHERE endpoint_key = ?
	`, endpointKey).Scan(
		&item.ID,
		&item.EndpointKey,
		&item.OrganizationKey,
		&item.ChannelType,
		&item.ChannelValue,
		&item.LastResult,
		&item.SuccessCount,
		&item.BounceCount,
		&item.AckCount,
		&item.FixCount,
		&item.MisrouteCount,
		&item.NoResponseCount,
		&lastContactedAt,
		&cooldownUntil,
		&item.PreferredForRoleType,
		&item.PreferredForAssetClass,
		&item.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get contact endpoint memory: %w", err)
	}
	if lastContactedAt.Valid {
		item.LastContactedAt = &lastContactedAt.Time
	}
	if cooldownUntil.Valid {
		item.CooldownUntil = &cooldownUntil.Time
	}
	return &item, nil
}

func (d *Database) ListAuthorityDirectoryRules(ctx context.Context, defectClass, assetClass string) ([]models.AuthorityDirectoryRule, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, jurisdiction_key, asset_class, defect_class, role_type,
			COALESCE(CAST(query_templates_json AS CHAR), ''), COALESCE(CAST(official_domains_json AS CHAR), ''),
			priority, created_at, updated_at
		FROM authority_directory_rules
		WHERE (defect_class = ? OR defect_class = 'general_defect')
		  AND (asset_class = ? OR asset_class = 'general_site')
		ORDER BY CASE jurisdiction_key WHEN '*' THEN 1 ELSE 0 END, priority ASC, id ASC
	`, defectClass, assetClass)
	if err != nil {
		return nil, fmt.Errorf("list authority directory rules: %w", err)
	}
	defer rows.Close()
	items := make([]models.AuthorityDirectoryRule, 0)
	for rows.Next() {
		var item models.AuthorityDirectoryRule
		if err := rows.Scan(
			&item.ID,
			&item.JurisdictionKey,
			&item.AssetClass,
			&item.DefectClass,
			&item.RoleType,
			&item.QueryTemplatesJSON,
			&item.OfficialDomainsJSON,
			&item.Priority,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func defaultCooldownForOutcome(outcomeType string) *time.Time {
	if outcomeType == "bounced" || outcomeType == "misrouted" {
		ts := time.Now().UTC().Add(7 * 24 * time.Hour)
		return &ts
	}
	return nil
}
