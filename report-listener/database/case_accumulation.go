package database

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"report-listener/geojsonx"
	"report-listener/models"
)

func populateClusterGeometryMetadata(cluster *models.SavedCluster) error {
	if cluster == nil {
		return nil
	}
	geometry := strings.TrimSpace(cluster.GeometryJSON)
	if geometry == "" {
		return nil
	}
	bounds, err := geojsonx.BoundsFromJSON(geometry)
	if err == nil && bounds != nil && bounds.Valid() {
		boundsJSON, err := bounds.ToJSONArray()
		if err != nil {
			return err
		}
		cluster.BBoxJSON = string(boundsJSON)
		lat, lng := bounds.Center()
		cluster.CentroidLat = &lat
		cluster.CentroidLng = &lng
	}
	if strings.TrimSpace(cluster.ClusterFingerprint) == "" {
		sum := sha1.Sum([]byte(cluster.Classification + "|" + geometry + "|" + strings.TrimSpace(cluster.Summary)))
		cluster.ClusterFingerprint = hex.EncodeToString(sum[:])
	}
	return nil
}

func (d *Database) ListOpenCasesForMatching(
	ctx context.Context,
	classification string,
	bounds *geojsonx.Bounds,
	limit int,
) ([]models.CaseMatchCandidate, error) {
	if classification == "" {
		classification = "physical"
	}
	if limit <= 0 {
		limit = 12
	}

	query := `
		SELECT
			c.case_id,
			c.slug,
			c.title,
			c.status,
			c.classification,
			COALESCE(c.summary, ''),
			COALESCE(CAST(c.geometry_json AS CHAR), ''),
			COALESCE(CAST(c.aggregate_geometry_json AS CHAR), ''),
			COALESCE(CAST(c.aggregate_bbox_json AS CHAR), ''),
			c.anchor_report_seq,
			c.anchor_lat,
			c.anchor_lng,
			c.cluster_count,
			c.linked_report_count,
			c.updated_at
		FROM cases c
		WHERE c.classification = ?
			AND c.status IN ('open', 'investigating')
			AND COALESCE(c.merged_into_case_id, '') = ''
	`
	args := []interface{}{classification}
	if bounds != nil && bounds.Valid() {
		padded := bounds.Expand(0.0018)
		query += `
			AND (
				(c.anchor_lat IS NOT NULL AND c.anchor_lng IS NOT NULL
					AND c.anchor_lat BETWEEN ? AND ?
					AND c.anchor_lng BETWEEN ? AND ?)
				OR c.anchor_lat IS NULL
				OR c.anchor_lng IS NULL
			)
		`
		args = append(args, padded.South, padded.North, padded.West, padded.East)
	}
	query += `
		ORDER BY c.updated_at DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list matchable cases: %w", err)
	}
	defer rows.Close()

	candidates := make([]models.CaseMatchCandidate, 0, limit)
	caseIDs := make([]string, 0, limit)
	for rows.Next() {
		var candidate models.CaseMatchCandidate
		if err := rows.Scan(
			&candidate.CaseID,
			&candidate.Slug,
			&candidate.Title,
			&candidate.Status,
			&candidate.Classification,
			&candidate.Summary,
			&candidate.GeometryJSON,
			&candidate.AggregateGeometryJSON,
			&candidate.AggregateBBoxJSON,
			&candidate.AnchorReportSeq,
			&candidate.AnchorLat,
			&candidate.AnchorLng,
			&candidate.ClusterCount,
			&candidate.LinkedReportCount,
			&candidate.UpdatedAt,
		); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
		caseIDs = append(caseIDs, candidate.CaseID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return candidates, nil
	}

	placeholders := make([]string, len(caseIDs))
	queryArgs := make([]interface{}, len(caseIDs))
	for i, caseID := range caseIDs {
		placeholders[i] = "?"
		queryArgs[i] = caseID
	}
	reportRows, err := d.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT case_id, GROUP_CONCAT(seq ORDER BY seq ASC)
		FROM case_reports
		WHERE case_id IN (%s)
		GROUP BY case_id
	`, strings.Join(placeholders, ",")), queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("load candidate case reports: %w", err)
	}
	defer reportRows.Close()

	reportSeqsByCase := make(map[string][]int, len(caseIDs))
	for reportRows.Next() {
		var caseID string
		var seqCSV sql.NullString
		if err := reportRows.Scan(&caseID, &seqCSV); err != nil {
			return nil, err
		}
		reportSeqsByCase[caseID] = parseSeqCSV(seqCSV.String)
	}
	if err := reportRows.Err(); err != nil {
		return nil, err
	}

	for i := range candidates {
		candidates[i].LinkedReportSeqs = reportSeqsByCase[candidates[i].CaseID]
	}
	return candidates, nil
}

func (d *Database) AttachClusterToCase(
	ctx context.Context,
	caseID string,
	reportSeqs []int,
	clusterLinks []models.CaseClusterLink,
	targets []models.CaseEscalationTarget,
	actorUserID string,
	auditPayload interface{},
) error {
	if strings.TrimSpace(caseID) == "" {
		return fmt.Errorf("case_id is required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, seq := range reportSeqs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_reports (case_id, seq, link_reason, confidence)
			VALUES (?, ?, 'cluster_match', 0.95)
			ON DUPLICATE KEY UPDATE
				link_reason = VALUES(link_reason),
				confidence = GREATEST(confidence, VALUES(confidence))
		`, caseID, seq); err != nil {
			return fmt.Errorf("attach report to case: %w", err)
		}
	}

	for _, link := range clusterLinks {
		if strings.TrimSpace(link.ClusterID) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_clusters (case_id, cluster_id, match_score, match_reason)
			VALUES (?, ?, ?, NULLIF(?, ''))
			ON DUPLICATE KEY UPDATE
				match_score = GREATEST(match_score, VALUES(match_score)),
				match_reason = COALESCE(NULLIF(VALUES(match_reason), ''), match_reason)
		`, caseID, link.ClusterID, link.MatchScore, link.MatchReason); err != nil {
			return fmt.Errorf("attach cluster to case: %w", err)
		}
	}

	existingKeys, err := loadExistingCaseTargetKeysTx(ctx, tx, caseID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if !hasCaseEscalationContactMethod(target) {
			continue
		}
		key := caseEscalationTargetDedupKey(target)
		if key != "" {
			if _, exists := existingKeys[key]; exists {
				continue
			}
			existingKeys[key] = struct{}{}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO case_escalation_targets (
				case_id, role_type, organization, display_name, channel, email, phone,
				website, contact_url, social_platform, social_handle,
				target_source, confidence_score, rationale
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, caseID, emptyOrDefault(target.RoleType, "contact"), emptyOrNil(target.Organization),
			emptyOrNil(target.DisplayName), emptyOrDefault(caseEscalationTargetChannel(target), "email"),
			emptyOrNil(target.Email), emptyOrNil(target.Phone), emptyOrNil(target.Website),
			emptyOrNil(target.ContactURL), emptyOrNil(target.SocialPlatform), emptyOrNil(target.SocialHandle),
			emptyOrDefault(target.TargetSource, "suggested"), target.ConfidenceScore, emptyOrNil(target.Rationale)); err != nil {
			return fmt.Errorf("attach escalation target to case: %w", err)
		}
	}

	if err := d.recomputeCaseAggregateTx(ctx, tx, caseID); err != nil {
		return err
	}
	if err := insertCaseAuditEventTx(ctx, tx, caseID, "cluster_attached", actorUserID, auditPayload); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) MergeCases(ctx context.Context, targetCaseID string, sourceCaseIDs []string, actorUserID string) error {
	targetCaseID = strings.TrimSpace(targetCaseID)
	if targetCaseID == "" {
		return fmt.Errorf("target_case_id is required")
	}
	normalizedSources := make([]string, 0, len(sourceCaseIDs))
	seen := make(map[string]struct{}, len(sourceCaseIDs))
	for _, sourceID := range sourceCaseIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" || sourceID == targetCaseID {
			continue
		}
		if _, exists := seen[sourceID]; exists {
			continue
		}
		seen[sourceID] = struct{}{}
		normalizedSources = append(normalizedSources, sourceID)
	}
	if len(normalizedSources) == 0 {
		return fmt.Errorf("source_case_ids is required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, sourceCaseID := range normalizedSources {
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO case_reports (case_id, seq, link_reason, confidence, attached_at)
			SELECT ?, seq, CONCAT('merged_from:', ?), confidence, attached_at
			FROM case_reports
			WHERE case_id = ?
		`, targetCaseID, sourceCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("merge case reports from %s: %w", sourceCaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO case_clusters (case_id, cluster_id, match_score, match_reason, linked_at)
			SELECT ?, cluster_id, match_score, match_reason, linked_at
			FROM case_clusters
			WHERE case_id = ?
		`, targetCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("merge case clusters from %s: %w", sourceCaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_escalation_targets SET case_id = ? WHERE case_id = ?
		`, targetCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("merge escalation targets from %s: %w", sourceCaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_escalation_actions SET case_id = ? WHERE case_id = ?
		`, targetCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("merge escalation actions from %s: %w", sourceCaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_email_deliveries SET case_id = ? WHERE case_id = ?
		`, targetCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("merge email deliveries from %s: %w", sourceCaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE case_resolution_signals SET case_id = ? WHERE case_id = ?
		`, targetCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("merge resolution signals from %s: %w", sourceCaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE cases
			SET status = 'merged', merged_into_case_id = ?, cluster_count = 0, linked_report_count = 0, last_cluster_at = NULL
			WHERE case_id = ?
		`, targetCaseID, sourceCaseID); err != nil {
			return fmt.Errorf("mark merged source case %s: %w", sourceCaseID, err)
		}
		if err := insertCaseAuditEventTx(ctx, tx, sourceCaseID, "case_merged_into", actorUserID, map[string]interface{}{"target_case_id": targetCaseID}); err != nil {
			return err
		}
	}

	if err := d.recomputeCaseAggregateTx(ctx, tx, targetCaseID); err != nil {
		return err
	}
	if err := insertCaseAuditEventTx(ctx, tx, targetCaseID, "case_merged", actorUserID, map[string]interface{}{"source_case_ids": normalizedSources}); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *Database) recomputeCaseAggregateTx(ctx context.Context, tx *sql.Tx, caseID string) error {
	clusterRows, err := tx.QueryContext(ctx, `
		SELECT
			COALESCE(CAST(sc.geometry_json AS CHAR), ''),
			COALESCE(CAST(sc.bbox_json AS CHAR), ''),
			cc.linked_at
		FROM case_clusters cc
		JOIN saved_clusters sc ON sc.cluster_id = cc.cluster_id
		WHERE cc.case_id = ?
		ORDER BY cc.linked_at ASC
	`, caseID)
	if err != nil {
		return fmt.Errorf("load case clusters for aggregate refresh: %w", err)
	}
	defer clusterRows.Close()

	clusterGeometries := make([]string, 0)
	clusterCount := 0
	var lastClusterAt *time.Time
	for clusterRows.Next() {
		var geometryJSON string
		var bboxJSON string
		var linkedAt time.Time
		if err := clusterRows.Scan(&geometryJSON, &bboxJSON, &linkedAt); err != nil {
			return err
		}
		clusterCount++
		if strings.TrimSpace(geometryJSON) != "" {
			clusterGeometries = append(clusterGeometries, geometryJSON)
		}
		if lastClusterAt == nil || linkedAt.After(*lastClusterAt) {
			copyTime := linkedAt
			lastClusterAt = &copyTime
		}
	}
	if err := clusterRows.Err(); err != nil {
		return err
	}

	var originalGeometry sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(CAST(geometry_json AS CHAR), '') FROM cases WHERE case_id = ?`, caseID).Scan(&originalGeometry); err != nil {
		return fmt.Errorf("load case geometry for aggregate refresh: %w", err)
	}
	if len(clusterGeometries) == 0 && strings.TrimSpace(originalGeometry.String) != "" {
		clusterGeometries = append(clusterGeometries, originalGeometry.String)
	}
	aggregateGeometryJSON, aggregateBBoxJSON, err := geojsonx.BuildAggregateGeometryJSON(clusterGeometries)
	if err != nil {
		return fmt.Errorf("build case aggregate geometry: %w", err)
	}

	reportRows, err := tx.QueryContext(ctx, `
		SELECT
			r.seq,
			r.latitude,
			r.longitude,
			r.ts,
			COALESCE(MAX(CASE WHEN ra.language = 'en' THEN ra.severity_level END), MAX(ra.severity_level), 0) AS severity
		FROM case_reports cr
		JOIN reports r ON r.seq = cr.seq
		LEFT JOIN report_analysis ra ON ra.seq = r.seq
		WHERE cr.case_id = ?
		GROUP BY r.seq, r.latitude, r.longitude, r.ts
	`, caseID)
	if err != nil {
		return fmt.Errorf("load case reports for aggregate refresh: %w", err)
	}
	defer reportRows.Close()

	type caseReportAggregate struct {
		Seq      int
		Lat      float64
		Lng      float64
		TS       time.Time
		Severity float64
	}
	reports := make([]caseReportAggregate, 0)
	var firstSeenAt *time.Time
	var lastSeenAt *time.Time
	var anchor *caseReportAggregate
	var totalSeverity float64
	for reportRows.Next() {
		var item caseReportAggregate
		if err := reportRows.Scan(&item.Seq, &item.Lat, &item.Lng, &item.TS, &item.Severity); err != nil {
			return err
		}
		reports = append(reports, item)
		totalSeverity += item.Severity
		if firstSeenAt == nil || item.TS.Before(*firstSeenAt) {
			copyTime := item.TS
			firstSeenAt = &copyTime
		}
		if lastSeenAt == nil || item.TS.After(*lastSeenAt) {
			copyTime := item.TS
			lastSeenAt = &copyTime
		}
		if anchor == nil || item.Severity > anchor.Severity || (item.Severity == anchor.Severity && item.TS.After(anchor.TS)) {
			copyItem := item
			anchor = &copyItem
		}
	}
	if err := reportRows.Err(); err != nil {
		return err
	}

	linkedReportCount := len(reports)
	maxSeverity := 0.0
	avgSeverity := 0.0
	if linkedReportCount > 0 {
		maxSeverity = reports[0].Severity
		for _, report := range reports[1:] {
			if report.Severity > maxSeverity {
				maxSeverity = report.Severity
			}
		}
		avgSeverity = totalSeverity / float64(linkedReportCount)
	}
	urgencyScore := math.Min(1, math.Max(maxSeverity, avgSeverity+(float64(clusterCount-1)*0.05)))
	trendScore := math.Min(1, float64(clusterCount)/5)

	var anchorSeq interface{}
	var anchorLat interface{}
	var anchorLng interface{}
	if anchor != nil {
		anchorSeq = anchor.Seq
		anchorLat = anchor.Lat
		anchorLng = anchor.Lng
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE cases
		SET
			aggregate_geometry_json = NULLIF(?, ''),
			aggregate_bbox_json = NULLIF(?, ''),
			cluster_count = ?,
			linked_report_count = ?,
			last_cluster_at = ?,
			anchor_report_seq = ?,
			anchor_lat = ?,
			anchor_lng = ?,
			first_seen_at = ?,
			last_seen_at = ?,
			severity_score = ?,
			urgency_score = ?,
			criticality_score = ?,
			trend_score = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE case_id = ?
	`, aggregateGeometryJSON, aggregateBBoxJSON, clusterCount, linkedReportCount, lastClusterAt,
		anchorSeq, anchorLat, anchorLng, firstSeenAt, lastSeenAt, maxSeverity, urgencyScore, maxSeverity, trendScore, caseID)
	if err != nil {
		return fmt.Errorf("update case aggregate fields: %w", err)
	}
	return nil
}

func loadExistingCaseTargetKeysTx(ctx context.Context, tx *sql.Tx, caseID string) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT COALESCE(channel, ''), COALESCE(email, ''), COALESCE(phone, ''), COALESCE(website, ''),
			COALESCE(contact_url, ''), COALESCE(social_platform, ''), COALESCE(social_handle, ''),
			COALESCE(organization, ''), COALESCE(display_name, '')
		FROM case_escalation_targets
		WHERE case_id = ?
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("load existing case targets: %w", err)
	}
	defer rows.Close()

	keys := make(map[string]struct{})
	for rows.Next() {
		var target models.CaseEscalationTarget
		if err := rows.Scan(
			&target.Channel,
			&target.Email,
			&target.Phone,
			&target.Website,
			&target.ContactURL,
			&target.SocialPlatform,
			&target.SocialHandle,
			&target.Organization,
			&target.DisplayName,
		); err != nil {
			return nil, err
		}
		if key := caseEscalationTargetDedupKey(target); key != "" {
			keys[key] = struct{}{}
		}
	}
	return keys, rows.Err()
}

func caseEscalationTargetDedupKey(target models.CaseEscalationTarget) string {
	channel := strings.ToLower(strings.TrimSpace(caseEscalationTargetChannel(target)))
	switch channel {
	case "email":
		if email := strings.ToLower(strings.TrimSpace(target.Email)); email != "" {
			return channel + ":" + email
		}
	case "phone":
		if phone := strings.ToLower(strings.TrimSpace(target.Phone)); phone != "" {
			return channel + ":" + phone
		}
	case "social":
		handle := strings.ToLower(strings.TrimSpace(target.SocialHandle))
		platform := strings.ToLower(strings.TrimSpace(target.SocialPlatform))
		if handle != "" {
			return channel + ":" + platform + ":" + handle
		}
	case "website":
		if website := strings.ToLower(strings.TrimSpace(target.Website)); website != "" {
			return channel + ":" + website
		}
		if contactURL := strings.ToLower(strings.TrimSpace(target.ContactURL)); contactURL != "" {
			return channel + ":" + contactURL
		}
	}
	org := strings.ToLower(strings.TrimSpace(target.Organization))
	display := strings.ToLower(strings.TrimSpace(target.DisplayName))
	if org != "" || display != "" {
		return "name:" + org + ":" + display
	}
	return ""
}

func parseSeqCSV(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		var seq int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &seq); err == nil && seq > 0 {
			out = append(out, seq)
		}
	}
	sort.Ints(out)
	return out
}
