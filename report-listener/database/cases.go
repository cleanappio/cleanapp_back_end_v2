package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"report-listener/models"
)

func newOpaqueID(prefix string) (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func marshalNullableJSON(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		return v, nil
	case []byte:
		if len(v) == 0 {
			return nil, nil
		}
		return string(v), nil
	default:
		blob, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if len(blob) == 0 || string(blob) == "null" {
			return nil, nil
		}
		return string(blob), nil
	}
}

func nullableStringPtr(s *string) interface{} {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return *s
}

func (d *Database) CreateSavedCluster(ctx context.Context, cluster *models.SavedCluster) error {
	if cluster == nil {
		return fmt.Errorf("cluster is required")
	}
	if cluster.ClusterID == "" {
		clusterID, err := newOpaqueID("clst")
		if err != nil {
			return err
		}
		cluster.ClusterID = clusterID
	}

	geometryJSON, err := marshalNullableJSON(cluster.GeometryJSON)
	if err != nil {
		return fmt.Errorf("marshal cluster geometry: %w", err)
	}
	statsJSON, err := marshalNullableJSON(cluster.StatsJSON)
	if err != nil {
		return fmt.Errorf("marshal cluster stats: %w", err)
	}
	analysisJSON, err := marshalNullableJSON(cluster.AnalysisJSON)
	if err != nil {
		return fmt.Errorf("marshal cluster analysis: %w", err)
	}

	if cluster.SourceType == "" {
		cluster.SourceType = "geometry"
	}
	if cluster.Classification == "" {
		cluster.Classification = "physical"
	}

	_, err = d.db.ExecContext(ctx, `
		INSERT INTO saved_clusters (
			cluster_id, source_type, classification, geometry_json, seed_report_seq,
			report_count, summary, stats_json, analysis_json, created_by_user_id
		) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)
	`, cluster.ClusterID, cluster.SourceType, cluster.Classification, geometryJSON, cluster.SeedReportSeq,
		cluster.ReportCount, cluster.Summary, statsJSON, analysisJSON, cluster.CreatedByUserID)
	if err != nil {
		return fmt.Errorf("insert saved cluster: %w", err)
	}
	return nil
}

func (d *Database) CreateCase(
	ctx context.Context,
	caseRecord *models.Case,
	reportSeqs []int,
	clusterIDs []string,
	targets []models.CaseEscalationTarget,
	auditPayload interface{},
) error {
	if caseRecord == nil {
		return fmt.Errorf("case is required")
	}
	if caseRecord.CaseID == "" {
		caseID, err := newOpaqueID("case")
		if err != nil {
			return err
		}
		caseRecord.CaseID = caseID
	}
	if caseRecord.Status == "" {
		caseRecord.Status = "open"
	}
	if caseRecord.Type == "" {
		caseRecord.Type = "incident"
	}
	if caseRecord.Classification == "" {
		caseRecord.Classification = "physical"
	}
	geometryJSON, err := marshalNullableJSON(caseRecord.GeometryJSON)
	if err != nil {
		return fmt.Errorf("marshal case geometry: %w", err)
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO cases (
			case_id, slug, title, type, status, classification, summary, uncertainty_notes,
			geometry_json, anchor_report_seq, anchor_lat, anchor_lng, building_id, parcel_id,
			severity_score, urgency_score, confidence_score, exposure_score, criticality_score, trend_score,
			first_seen_at, last_seen_at, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, caseRecord.CaseID, caseRecord.Slug, caseRecord.Title, caseRecord.Type, caseRecord.Status,
		caseRecord.Classification, caseRecord.Summary, caseRecord.UncertaintyNotes, geometryJSON,
		caseRecord.AnchorReportSeq, caseRecord.AnchorLat, caseRecord.AnchorLng, nullableStringPtr(caseRecord.BuildingID),
		nullableStringPtr(caseRecord.ParcelID), caseRecord.SeverityScore, caseRecord.UrgencyScore,
		caseRecord.ConfidenceScore, caseRecord.ExposureScore, caseRecord.CriticalityScore, caseRecord.TrendScore,
		caseRecord.FirstSeenAt, caseRecord.LastSeenAt, caseRecord.CreatedByUserID)
	if err != nil {
		return fmt.Errorf("insert case: %w", err)
	}

	for _, seq := range reportSeqs {
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO case_reports (case_id, seq, link_reason, confidence)
			VALUES (?, ?, 'initial_selection', 1.0)
		`, caseRecord.CaseID, seq); err != nil {
			return fmt.Errorf("insert case report link: %w", err)
		}
	}

	for _, clusterID := range clusterIDs {
		if strings.TrimSpace(clusterID) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO case_clusters (case_id, cluster_id) VALUES (?, ?)
		`, caseRecord.CaseID, clusterID); err != nil {
			return fmt.Errorf("insert case cluster link: %w", err)
		}
	}

	for _, target := range targets {
		if strings.TrimSpace(target.Email) == "" && strings.TrimSpace(target.Phone) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_escalation_targets (
				case_id, role_type, organization, display_name, email, phone,
				target_source, confidence_score, rationale
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, caseRecord.CaseID, emptyOrDefault(target.RoleType, "contact"), emptyOrNil(target.Organization),
			emptyOrNil(target.DisplayName), emptyOrNil(target.Email), emptyOrNil(target.Phone),
			emptyOrDefault(target.TargetSource, "suggested"), target.ConfidenceScore, emptyOrNil(target.Rationale)); err != nil {
			return fmt.Errorf("insert escalation target: %w", err)
		}
	}

	if err := insertCaseAuditEventTx(ctx, tx, caseRecord.CaseID, "case_created", caseRecord.CreatedByUserID, auditPayload); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (d *Database) AddReportsToCase(ctx context.Context, caseID string, seqs []int, linkReason string, confidence float64, actorUserID string) error {
	if strings.TrimSpace(caseID) == "" {
		return fmt.Errorf("case_id is required")
	}
	if len(seqs) == 0 {
		return nil
	}
	if linkReason == "" {
		linkReason = "manual"
	}
	if confidence <= 0 {
		confidence = 1.0
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, seq := range seqs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_reports (case_id, seq, link_reason, confidence)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE link_reason = VALUES(link_reason), confidence = VALUES(confidence)
		`, caseID, seq, linkReason, confidence); err != nil {
			return fmt.Errorf("add report to case: %w", err)
		}
	}

	payload := map[string]interface{}{"report_seqs": seqs, "link_reason": linkReason, "confidence": confidence}
	if err := insertCaseAuditEventTx(ctx, tx, caseID, "reports_added", actorUserID, payload); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *Database) UpdateCaseStatus(ctx context.Context, caseID, status, summary, actorUserID string, payload interface{}) error {
	if caseID == "" || status == "" {
		return fmt.Errorf("case_id and status are required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if summary != "" {
		_, err = tx.ExecContext(ctx, `UPDATE cases SET status = ?, summary = ?, updated_at = CURRENT_TIMESTAMP WHERE case_id = ?`, status, summary, caseID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE cases SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE case_id = ?`, status, caseID)
	}
	if err != nil {
		return fmt.Errorf("update case status: %w", err)
	}

	if err := insertCaseAuditEventTx(ctx, tx, caseID, "status_changed", actorUserID, payload); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *Database) GetCaseDetail(ctx context.Context, caseID string) (*models.CaseDetail, error) {
	var detail models.CaseDetail
	var (
		geometryJSON sql.NullString
		buildingID   sql.NullString
		parcelID     sql.NullString
		firstSeenAt  sql.NullTime
		lastSeenAt   sql.NullTime
	)

	err := d.db.QueryRowContext(ctx, `
		SELECT
			case_id, slug, title, type, status, classification, summary, uncertainty_notes,
			COALESCE(CAST(geometry_json AS CHAR), ''), anchor_report_seq, anchor_lat, anchor_lng,
			building_id, parcel_id, severity_score, urgency_score, confidence_score,
			exposure_score, criticality_score, trend_score, first_seen_at, last_seen_at,
			created_by_user_id, created_at, updated_at
		FROM cases
		WHERE case_id = ?
	`, caseID).Scan(
		&detail.Case.CaseID, &detail.Case.Slug, &detail.Case.Title, &detail.Case.Type, &detail.Case.Status,
		&detail.Case.Classification, &detail.Case.Summary, &detail.Case.UncertaintyNotes, &geometryJSON,
		&detail.Case.AnchorReportSeq, &detail.Case.AnchorLat, &detail.Case.AnchorLng, &buildingID, &parcelID,
		&detail.Case.SeverityScore, &detail.Case.UrgencyScore, &detail.Case.ConfidenceScore,
		&detail.Case.ExposureScore, &detail.Case.CriticalityScore, &detail.Case.TrendScore,
		&firstSeenAt, &lastSeenAt, &detail.Case.CreatedByUserID, &detail.Case.CreatedAt, &detail.Case.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("case %s not found", caseID)
		}
		return nil, fmt.Errorf("load case: %w", err)
	}
	detail.Case.GeometryJSON = geometryJSON.String
	if buildingID.Valid {
		detail.Case.BuildingID = &buildingID.String
	}
	if parcelID.Valid {
		detail.Case.ParcelID = &parcelID.String
	}
	if firstSeenAt.Valid {
		detail.Case.FirstSeenAt = &firstSeenAt.Time
	}
	if lastSeenAt.Valid {
		detail.Case.LastSeenAt = &lastSeenAt.Time
	}

	if detail.LinkedReports, err = d.listCaseReports(ctx, caseID); err != nil {
		return nil, err
	}
	if detail.Clusters, err = d.listCaseClusters(ctx, caseID); err != nil {
		return nil, err
	}
	if detail.EscalationTargets, err = d.listCaseEscalationTargets(ctx, caseID); err != nil {
		return nil, err
	}
	if detail.EscalationActions, err = d.listCaseEscalationActions(ctx, caseID); err != nil {
		return nil, err
	}
	if detail.EmailDeliveries, err = d.listCaseEmailDeliveries(ctx, caseID); err != nil {
		return nil, err
	}
	if detail.ResolutionSignals, err = d.listCaseResolutionSignals(ctx, caseID); err != nil {
		return nil, err
	}
	if detail.AuditEvents, err = d.listCaseAuditEvents(ctx, caseID); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (d *Database) GetCasesByReportSeq(ctx context.Context, seq int) ([]models.ReportCaseSummary, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			c.case_id,
			c.slug,
			c.title,
			c.status,
			c.classification,
			COALESCE(c.summary, ''),
			c.severity_score,
			c.urgency_score,
			c.updated_at,
			COALESCE(t.target_count, 0) AS escalation_target_count,
			COALESCE(del.delivery_count, 0) AS delivery_count
		FROM case_reports cr
		JOIN cases c ON c.case_id = cr.case_id
		LEFT JOIN (
			SELECT case_id, COUNT(*) AS target_count
			FROM case_escalation_targets
			GROUP BY case_id
		) t ON t.case_id = c.case_id
		LEFT JOIN (
			SELECT case_id, COUNT(*) AS delivery_count
			FROM case_email_deliveries
			WHERE delivery_status = 'sent'
			GROUP BY case_id
		) del ON del.case_id = c.case_id
		WHERE cr.seq = ?
		ORDER BY c.updated_at DESC, c.created_at DESC
	`, seq)
	if err != nil {
		return nil, fmt.Errorf("list cases by report seq: %w", err)
	}
	defer rows.Close()

	items := make([]models.ReportCaseSummary, 0)
	for rows.Next() {
		var item models.ReportCaseSummary
		if err := rows.Scan(
			&item.CaseID,
			&item.Slug,
			&item.Title,
			&item.Status,
			&item.Classification,
			&item.Summary,
			&item.SeverityScore,
			&item.UrgencyScore,
			&item.UpdatedAt,
			&item.EscalationTargetCount,
			&item.DeliveryCount,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) SuggestEscalationTargetsByGeometry(ctx context.Context, geometryJSON string, reportSeqs []int, limit int) ([]models.CaseEscalationTarget, error) {
	if limit <= 0 {
		limit = 8
	}
	results := make([]models.CaseEscalationTarget, 0, limit)
	seen := make(map[string]struct{})

	if strings.TrimSpace(geometryJSON) != "" {
		rows, err := d.db.QueryContext(ctx, `
			SELECT DISTINCT ce.email, a.name
			FROM area_index ai
			JOIN contact_emails ce ON ce.area_id = ai.area_id AND ce.consent_report = TRUE
			JOIN areas a ON a.id = ai.area_id
			WHERE ST_Intersects(ai.geom, ST_SRID(ST_GeomFromGeoJSON(?), 4326))
			ORDER BY a.name ASC, ce.email ASC
			LIMIT ?
		`, geometryJSON, limit)
		if err != nil {
			return nil, fmt.Errorf("query area escalation targets: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var email, org string
			if err := rows.Scan(&email, &org); err != nil {
				return nil, err
			}
			key := strings.ToLower(strings.TrimSpace(email))
			if key == "" {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, models.CaseEscalationTarget{
				RoleType:        "contact",
				Organization:    org,
				DisplayName:     org,
				Email:           email,
				TargetSource:    "area_contact",
				ConfidenceScore: 0.9,
				Rationale:       "Area contact mapped from selected geometry.",
			})
			if len(results) >= limit {
				return results, nil
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if len(reportSeqs) == 0 || len(results) >= limit {
		return results, nil
	}
	placeholders := make([]string, len(reportSeqs))
	args := make([]interface{}, len(reportSeqs))
	for i, seq := range reportSeqs {
		placeholders[i] = "?"
		args[i] = seq
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(inferred_contact_emails, '')
		FROM report_analysis
		WHERE seq IN (%s)
	`, strings.Join(placeholders, ","))
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query inferred contacts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		for _, email := range splitPossibleEmails(raw) {
			key := strings.ToLower(email)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, models.CaseEscalationTarget{
				RoleType:        "contact",
				Email:           email,
				TargetSource:    "inferred_contact",
				ConfidenceScore: 0.6,
				Rationale:       "Inferred contact found in report analysis.",
			})
			if len(results) >= limit {
				return results, nil
			}
		}
	}
	return results, rows.Err()
}

func (d *Database) listCaseReports(ctx context.Context, caseID string) ([]models.CaseReportLink, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			cr.case_id,
			cr.seq,
			COALESCE(r.public_id, '') AS public_id,
			cr.link_reason,
			cr.confidence,
			cr.attached_at,
			COALESCE(MAX(CASE WHEN ra.language = 'en' THEN ra.title END), MAX(ra.title), '') AS title,
			COALESCE(MAX(CASE WHEN ra.language = 'en' THEN ra.summary END), MAX(ra.summary), '') AS summary,
			COALESCE(MAX(CASE WHEN ra.language = 'en' THEN ra.classification END), MAX(ra.classification), 'physical') AS classification,
			COALESCE(MAX(CASE WHEN ra.language = 'en' THEN ra.severity_level END), MAX(ra.severity_level), 0) AS severity_level,
			r.latitude,
			r.longitude,
			r.ts,
			(SELECT MAX(created_at) FROM sent_reports_emails sre WHERE sre.seq = r.seq) AS last_email_sent_at,
			(SELECT COUNT(*) FROM report_email_deliveries red WHERE red.seq = r.seq AND red.delivery_status = 'sent') AS recipient_count
		FROM case_reports cr
		JOIN reports r ON r.seq = cr.seq
		LEFT JOIN report_analysis ra ON ra.seq = r.seq
		WHERE cr.case_id = ?
			GROUP BY cr.case_id, cr.seq, r.public_id, cr.link_reason, cr.confidence, cr.attached_at, r.latitude, r.longitude, r.ts
		ORDER BY cr.attached_at ASC, cr.seq ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list case reports: %w", err)
	}
	defer rows.Close()

	var items []models.CaseReportLink
	for rows.Next() {
		var item models.CaseReportLink
		var lastSent sql.NullTime
		if err := rows.Scan(
			&item.CaseID, &item.Seq, &item.PublicID, &item.LinkReason, &item.Confidence, &item.AttachedAt,
			&item.Title, &item.Summary, &item.Classification, &item.SeverityLevel,
			&item.Latitude, &item.Longitude, &item.ReportTimestamp, &lastSent, &item.RecipientCount,
		); err != nil {
			return nil, err
		}
		if lastSent.Valid {
			item.LastEmailSentAt = &lastSent.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) listCaseClusters(ctx context.Context, caseID string) ([]models.SavedCluster, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT sc.cluster_id, sc.source_type, sc.classification, COALESCE(CAST(sc.geometry_json AS CHAR), ''),
			sc.seed_report_seq, sc.report_count, COALESCE(sc.summary, ''), COALESCE(CAST(sc.stats_json AS CHAR), ''),
			COALESCE(CAST(sc.analysis_json AS CHAR), ''), sc.created_by_user_id, sc.created_at, sc.updated_at
		FROM case_clusters cc
		JOIN saved_clusters sc ON sc.cluster_id = cc.cluster_id
		WHERE cc.case_id = ?
		ORDER BY cc.linked_at ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list case clusters: %w", err)
	}
	defer rows.Close()

	var items []models.SavedCluster
	for rows.Next() {
		var item models.SavedCluster
		if err := rows.Scan(
			&item.ClusterID, &item.SourceType, &item.Classification, &item.GeometryJSON, &item.SeedReportSeq,
			&item.ReportCount, &item.Summary, &item.StatsJSON, &item.AnalysisJSON,
			&item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) listCaseEscalationTargets(ctx context.Context, caseID string) ([]models.CaseEscalationTarget, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, case_id, role_type, COALESCE(organization, ''), COALESCE(display_name, ''),
			COALESCE(email, ''), COALESCE(phone, ''), target_source, confidence_score,
			COALESCE(rationale, ''), created_at
		FROM case_escalation_targets
		WHERE case_id = ?
		ORDER BY confidence_score DESC, created_at ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list escalation targets: %w", err)
	}
	defer rows.Close()

	var items []models.CaseEscalationTarget
	for rows.Next() {
		var item models.CaseEscalationTarget
		if err := rows.Scan(
			&item.ID, &item.CaseID, &item.RoleType, &item.Organization, &item.DisplayName,
			&item.Email, &item.Phone, &item.TargetSource, &item.ConfidenceScore, &item.Rationale, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) listCaseEscalationActions(ctx context.Context, caseID string) ([]models.CaseEscalationAction, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, case_id, target_id, channel, status, COALESCE(subject, ''), COALESCE(body, ''),
			COALESCE(CAST(attachments_json AS CHAR), ''), COALESCE(sent_by_user_id, ''),
			COALESCE(provider_message_id, ''), sent_at, created_at
		FROM case_escalation_actions
		WHERE case_id = ?
		ORDER BY created_at ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list escalation actions: %w", err)
	}
	defer rows.Close()

	var items []models.CaseEscalationAction
	for rows.Next() {
		var item models.CaseEscalationAction
		var targetID sql.NullInt64
		var sentAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.CaseID, &targetID, &item.Channel, &item.Status, &item.Subject, &item.Body,
			&item.AttachmentsJSON, &item.SentByUserID, &item.ProviderMessageID, &sentAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if targetID.Valid {
			item.TargetID = &targetID.Int64
		}
		if sentAt.Valid {
			item.SentAt = &sentAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) listCaseEmailDeliveries(ctx context.Context, caseID string) ([]models.CaseEmailDelivery, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, case_id, action_id, target_id, recipient_email, delivery_status,
			delivery_source, provider, COALESCE(provider_message_id, ''), sent_at,
			COALESCE(error_message, ''), created_at
		FROM case_email_deliveries
		WHERE case_id = ?
		ORDER BY created_at ASC, id ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list case email deliveries: %w", err)
	}
	defer rows.Close()

	var items []models.CaseEmailDelivery
	for rows.Next() {
		var item models.CaseEmailDelivery
		var actionID sql.NullInt64
		var targetID sql.NullInt64
		var sentAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.CaseID, &actionID, &targetID, &item.RecipientEmail, &item.DeliveryStatus,
			&item.DeliverySource, &item.Provider, &item.ProviderMessageID, &sentAt, &item.ErrorMessage,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if actionID.Valid {
			item.ActionID = &actionID.Int64
		}
		if targetID.Valid {
			item.TargetID = &targetID.Int64
		}
		if sentAt.Valid {
			item.SentAt = &sentAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) CreateCaseEscalationActions(ctx context.Context, caseID string, targetIDs []int64, subject, body, actorUserID string) ([]models.CaseEscalationTarget, []models.CaseEscalationAction, error) {
	targets, err := d.listCaseEscalationTargets(ctx, caseID)
	if err != nil {
		return nil, nil, err
	}
	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("no escalation targets for case %s", caseID)
	}

	selected := make([]models.CaseEscalationTarget, 0, len(targets))
	allowAll := len(targetIDs) == 0
	allowed := make(map[int64]struct{}, len(targetIDs))
	for _, id := range targetIDs {
		allowed[id] = struct{}{}
	}
	for _, target := range targets {
		if strings.TrimSpace(target.Email) == "" {
			continue
		}
		if !allowAll {
			if _, ok := allowed[target.ID]; !ok {
				continue
			}
		}
		selected = append(selected, target)
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("no email-capable escalation targets selected")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	actions := make([]models.CaseEscalationAction, 0, len(selected))
	for _, target := range selected {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO case_escalation_actions (
				case_id, target_id, channel, status, subject, body, sent_by_user_id
			) VALUES (?, ?, 'email', 'pending', ?, ?, ?)
		`, caseID, target.ID, subject, body, emptyOrNil(actorUserID))
		if err != nil {
			return nil, nil, fmt.Errorf("insert case escalation action: %w", err)
		}
		actionID, err := result.LastInsertId()
		if err != nil {
			return nil, nil, fmt.Errorf("get case escalation action id: %w", err)
		}
		actions = append(actions, models.CaseEscalationAction{
			ID:           actionID,
			CaseID:       caseID,
			TargetID:     &target.ID,
			Channel:      "email",
			Status:       "pending",
			Subject:      subject,
			Body:         body,
			SentByUserID: actorUserID,
		})
	}

	payload := map[string]interface{}{
		"target_ids":   targetIDs,
		"target_count": len(selected),
		"subject":      subject,
	}
	if err := insertCaseAuditEventTx(ctx, tx, caseID, "case_escalation_requested", actorUserID, payload); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return selected, actions, nil
}

func (d *Database) RecordCaseEscalationDeliveries(ctx context.Context, caseID string, actions []models.CaseEscalationAction, deliveries []models.CaseEmailDelivery, actorUserID string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, delivery := range deliveries {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO case_email_deliveries (
				case_id, action_id, target_id, recipient_email, delivery_status,
				delivery_source, provider, provider_message_id, sent_at, error_message
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, caseID, delivery.ActionID, delivery.TargetID, delivery.RecipientEmail, delivery.DeliveryStatus,
			emptyOrDefault(delivery.DeliverySource, "case_target"), emptyOrDefault(delivery.Provider, "sendgrid"),
			emptyOrNil(delivery.ProviderMessageID), delivery.SentAt, emptyOrNil(delivery.ErrorMessage))
		if err != nil {
			return fmt.Errorf("insert case email delivery: %w", err)
		}
	}

	for _, action := range actions {
		status := "failed"
		var providerMessageID interface{}
		var sentAt interface{}
		for _, delivery := range deliveries {
			if delivery.ActionID != nil && action.ID == *delivery.ActionID {
				if delivery.DeliveryStatus == "sent" {
					status = "sent"
					providerMessageID = emptyOrNil(delivery.ProviderMessageID)
					sentAt = delivery.SentAt
					break
				}
				if status != "sent" {
					status = delivery.DeliveryStatus
				}
			}
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE case_escalation_actions
			SET status = ?, provider_message_id = ?, sent_at = ?, created_at = created_at
			WHERE id = ? AND case_id = ?
		`, status, providerMessageID, sentAt, action.ID, caseID)
		if err != nil {
			return fmt.Errorf("update case escalation action: %w", err)
		}
	}

	payload := map[string]interface{}{
		"action_count":   len(actions),
		"delivery_count": len(deliveries),
	}
	if err := insertCaseAuditEventTx(ctx, tx, caseID, "case_escalation_recorded", actorUserID, payload); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *Database) listCaseResolutionSignals(ctx context.Context, caseID string) ([]models.CaseResolutionSignal, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, case_id, source_type, summary, linked_report_seq, COALESCE(CAST(metadata_json AS CHAR), ''), created_at
		FROM case_resolution_signals
		WHERE case_id = ?
		ORDER BY created_at ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list resolution signals: %w", err)
	}
	defer rows.Close()

	var items []models.CaseResolutionSignal
	for rows.Next() {
		var item models.CaseResolutionSignal
		if err := rows.Scan(
			&item.ID, &item.CaseID, &item.SourceType, &item.Summary, &item.LinkedReportSeq, &item.MetadataJSON, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *Database) listCaseAuditEvents(ctx context.Context, caseID string) ([]models.CaseAuditEvent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, case_id, event_type, COALESCE(actor_user_id, ''), COALESCE(CAST(payload_json AS CHAR), ''), created_at
		FROM case_audit_events
		WHERE case_id = ?
		ORDER BY created_at ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	var items []models.CaseAuditEvent
	for rows.Next() {
		var item models.CaseAuditEvent
		if err := rows.Scan(&item.ID, &item.CaseID, &item.EventType, &item.ActorUserID, &item.PayloadJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func insertCaseAuditEventTx(ctx context.Context, tx *sql.Tx, caseID, eventType, actorUserID string, payload interface{}) error {
	payloadJSON, err := marshalNullableJSON(payload)
	if err != nil {
		return fmt.Errorf("marshal case audit payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO case_audit_events (case_id, event_type, actor_user_id, payload_json)
		VALUES (?, ?, ?, NULLIF(?, ''))
	`, caseID, eventType, emptyOrNil(actorUserID), payloadJSON)
	if err != nil {
		return fmt.Errorf("insert case audit event: %w", err)
	}
	return nil
}

func emptyOrNil(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func emptyOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func splitPossibleEmails(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, ";", ",")
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" || !strings.Contains(email, "@") {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}
