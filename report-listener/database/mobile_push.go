package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"report-listener/models"
)

func hashPushToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func normalizePushString(value string) string {
	return strings.TrimSpace(value)
}

func (d *Database) UpsertMobilePushDevice(ctx context.Context, device models.MobilePushDevice) error {
	device.InstallID = normalizePushString(device.InstallID)
	device.Platform = strings.ToLower(normalizePushString(device.Platform))
	device.Provider = strings.ToLower(normalizePushString(device.Provider))
	device.PushToken = normalizePushString(device.PushToken)
	device.AppVersion = normalizePushString(device.AppVersion)

	if device.InstallID == "" {
		return fmt.Errorf("install_id is required")
	}
	if device.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if device.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	if device.NotificationsEnabled && device.PushToken == "" {
		return fmt.Errorf("push_token is required when notifications are enabled")
	}

	tokenHash := hashPushToken(device.PushToken)
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO mobile_push_devices (
			install_id,
			platform,
			provider,
			push_token,
			push_token_hash,
			app_version,
			notifications_enabled,
			status,
			last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'active', NOW())
		ON DUPLICATE KEY UPDATE
			platform = VALUES(platform),
			push_token = VALUES(push_token),
			push_token_hash = VALUES(push_token_hash),
			app_version = VALUES(app_version),
			notifications_enabled = VALUES(notifications_enabled),
			status = IF(VALUES(notifications_enabled), 'active', 'inactive'),
			last_seen_at = NOW(),
			updated_at = NOW()
	`, device.InstallID, device.Platform, device.Provider, device.PushToken, tokenHash, device.AppVersion, device.NotificationsEnabled)
	if err != nil {
		return fmt.Errorf("failed to upsert mobile push device: %w", err)
	}
	return nil
}

func (d *Database) DeactivateMobilePushDevice(ctx context.Context, installID, provider string) error {
	installID = normalizePushString(installID)
	provider = strings.ToLower(normalizePushString(provider))
	if installID == "" || provider == "" {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE mobile_push_devices
		SET notifications_enabled = FALSE,
		    status = 'inactive',
		    updated_at = NOW()
		WHERE install_id = ? AND provider = ?
	`, installID, provider)
	if err != nil {
		return fmt.Errorf("failed to deactivate mobile push device: %w", err)
	}
	return nil
}

func (d *Database) DeactivateMobilePushDeviceByID(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE mobile_push_devices
		SET notifications_enabled = FALSE,
		    status = 'inactive',
		    updated_at = NOW()
		WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("failed to deactivate mobile push device by id: %w", err)
	}
	return nil
}

func (d *Database) LinkReportToPushInstall(ctx context.Context, reportSeq int, installID string) error {
	installID = normalizePushString(installID)
	if reportSeq <= 0 || installID == "" {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO report_push_subscriptions (
			report_seq,
			install_id,
			notification_kind
		) VALUES (?, ?, 'delivery')
		ON DUPLICATE KEY UPDATE
			updated_at = NOW()
	`, reportSeq, installID)
	if err != nil {
		return fmt.Errorf("failed to link report to push install: %w", err)
	}
	return nil
}

func (d *Database) GetReportPushDevices(ctx context.Context, reportSeq int) ([]models.MobilePushDevice, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			mpd.id,
			mpd.install_id,
			mpd.platform,
			mpd.provider,
			mpd.push_token,
			mpd.push_token_hash,
			mpd.app_version,
			mpd.notifications_enabled,
			mpd.status,
			mpd.last_seen_at
		FROM report_push_subscriptions rps
		JOIN mobile_push_devices mpd
		  ON mpd.install_id = rps.install_id
		WHERE rps.report_seq = ?
		  AND rps.notification_kind = 'delivery'
		  AND mpd.notifications_enabled = TRUE
		  AND mpd.status = 'active'
		ORDER BY mpd.updated_at DESC
	`, reportSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query report push devices: %w", err)
	}
	defer rows.Close()

	devices := make([]models.MobilePushDevice, 0)
	for rows.Next() {
		var device models.MobilePushDevice
		if err := rows.Scan(
			&device.ID,
			&device.InstallID,
			&device.Platform,
			&device.Provider,
			&device.PushToken,
			&device.PushTokenHash,
			&device.AppVersion,
			&device.NotificationsEnabled,
			&device.Status,
			&device.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan report push device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating report push devices: %w", err)
	}
	return devices, nil
}

func (d *Database) HasMobilePushDeliveryEvent(ctx context.Context, reportSeq int, installID, deliveryStatus string) (bool, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mobile_push_delivery_events
		WHERE report_seq = ? AND install_id = ? AND delivery_status = ?
	`, reportSeq, normalizePushString(installID), normalizePushString(deliveryStatus)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check mobile push delivery event: %w", err)
	}
	return count > 0, nil
}

func (d *Database) RecordMobilePushDeliveryEvent(ctx context.Context, event models.ReportPushDeliveryEvent) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO mobile_push_delivery_events (
			report_seq,
			install_id,
			delivery_status,
			provider,
			response_code,
			response_body
		) VALUES (?, ?, ?, ?, ?, ?)
	`, event.ReportSeq, normalizePushString(event.InstallID), normalizePushString(event.DeliveryStatus), normalizePushString(event.Provider), nullableInt(event.ResponseCode), nullableString(event.ResponseBody))
	if err != nil {
		return fmt.Errorf("failed to record mobile push delivery event: %w", err)
	}
	return nil
}

func (d *Database) ListReportDeliveryRecipients(ctx context.Context, reportSeq int) ([]models.ReportDeliveryRecipient, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT recipient_email, delivery_source, delivery_status, sent_at
		FROM report_email_deliveries
		WHERE seq = ? AND delivery_status = 'sent'
		ORDER BY sent_at DESC, id DESC
	`, reportSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query report email deliveries: %w", err)
	}
	defer rows.Close()

	recipients := make([]models.ReportDeliveryRecipient, 0)
	emailOrder := make([]string, 0)
	for rows.Next() {
		var (
			item      models.ReportDeliveryRecipient
			sentAtRaw sql.NullTime
		)
		if err := rows.Scan(&item.Email, &item.DeliverySource, &item.DeliveryStatus, &sentAtRaw); err != nil {
			return nil, fmt.Errorf("failed to scan report email delivery: %w", err)
		}
		item.Email = strings.TrimSpace(item.Email)
		if sentAtRaw.Valid {
			sentAt := sentAtRaw.Time
			item.SentAt = &sentAt
		}
		if item.Email != "" {
			emailOrder = append(emailOrder, strings.ToLower(item.Email))
		}
		recipients = append(recipients, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating report email deliveries: %w", err)
	}
	if len(recipients) == 0 {
		return recipients, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(emailOrder)), ",")
	args := make([]interface{}, 0, len(emailOrder)+1)
	args = append(args, reportSeq)
	for _, email := range emailOrder {
		args = append(args, email)
	}
	targetRows, err := d.db.QueryContext(ctx, `
		SELECT
			LOWER(TRIM(email)) AS normalized_email,
			COALESCE(display_name, ''),
			COALESCE(organization, '')
		FROM report_escalation_targets
		WHERE report_seq = ?
		  AND LOWER(TRIM(email)) IN (`+placeholders+`)
		ORDER BY actionability_score DESC, confidence_score DESC, id ASC
	`, args...)
	if err != nil {
		return recipients, nil
	}
	defer targetRows.Close()

	targetByEmail := make(map[string]models.ReportDeliveryRecipient, len(emailOrder))
	for targetRows.Next() {
		var (
			normalizedEmail string
			displayName     string
			organization    string
		)
		if err := targetRows.Scan(&normalizedEmail, &displayName, &organization); err != nil {
			return nil, fmt.Errorf("failed to scan report escalation target recipient metadata: %w", err)
		}
		normalizedEmail = strings.TrimSpace(strings.ToLower(normalizedEmail))
		if normalizedEmail == "" {
			continue
		}
		if _, exists := targetByEmail[normalizedEmail]; exists {
			continue
		}
		targetByEmail[normalizedEmail] = models.ReportDeliveryRecipient{
			DisplayName:  strings.TrimSpace(displayName),
			Organization: strings.TrimSpace(organization),
		}
	}
	if err := targetRows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating report escalation target recipient metadata: %w", err)
	}

	for i := range recipients {
		normalizedEmail := strings.ToLower(strings.TrimSpace(recipients[i].Email))
		if normalizedEmail == "" {
			continue
		}
		if metadata, ok := targetByEmail[normalizedEmail]; ok {
			recipients[i].DisplayName = metadata.DisplayName
			recipients[i].Organization = metadata.Organization
		}
		if recipients[i].DisplayName == "" && recipients[i].Organization == "" {
			recipients[i].Organization = inferRecipientOrganizationFromEmail(recipients[i].Email)
		}
	}

	slices.SortStableFunc(recipients, func(left, right models.ReportDeliveryRecipient) int {
		if left.SentAt == nil && right.SentAt == nil {
			return strings.Compare(left.Email, right.Email)
		}
		if left.SentAt == nil {
			return 1
		}
		if right.SentAt == nil {
			return -1
		}
		if left.SentAt.Equal(*right.SentAt) {
			return strings.Compare(left.Email, right.Email)
		}
		if left.SentAt.After(*right.SentAt) {
			return -1
		}
		return 1
	})

	return recipients, nil
}

func inferRecipientOrganizationFromEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	domain := email[at+1:]
	parts := strings.Split(domain, ".")
	if len(parts) == 0 {
		return domain
	}
	label := parts[0]
	label = strings.ReplaceAll(label, "-", " ")
	label = strings.ReplaceAll(label, "_", " ")
	return strings.TrimSpace(label)
}

func nullableString(value string) sql.NullString {
	value = normalizePushString(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableInt(value int) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(value), Valid: true}
}
