package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"report-listener/models"
)

// Each pass reads indexed, bounded metadata in the same repeatable-read snapshot.
// Full analysis/evidence text is fetched only after exact source/family grouping.
// In particular, neither source JSON nor report images participate in a sort.
type evidenceAnalysisCore struct {
	Record             evidenceRecord
	Created            sql.NullTime
	CreatedLiteral     *string
	LanguageNull       bool
	Requested, English sql.NullInt64
	LanguageWeight     []byte
}

func evidenceIDsJSON(ids []int) string { data, _ := json.Marshal(ids); return string(data) }

const evidenceIDsTable = `JSON_TABLE(?, '$[*]' COLUMNS(seq INT PATH '$')) ids`

func preferredEvidenceAnalysis(a, b evidenceAnalysisCore) bool {
	for _, pair := range [][2]sql.NullInt64{{a.Requested, b.Requested}, {a.English, b.English}} {
		if pair[0].Valid != pair[1].Valid {
			return pair[0].Valid
		}
		if pair[0].Int64 != pair[1].Int64 {
			return pair[0].Int64 > pair[1].Int64
		}
	}
	if cmp := bytes.Compare(a.LanguageWeight, b.LanguageWeight); cmp != 0 {
		return cmp < 0
	}
	if a.Created.Valid != b.Created.Valid {
		return a.Created.Valid
	}
	return a.Created.Time.After(b.Created.Time)
}

func loadEvidenceAnalysisCore(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (map[int]evidenceAnalysisCore, []int, error) {
	marks := make([]string, len(f.Brands))
	args := []interface{}{f.Language}
	for i, brand := range f.Brands {
		marks[i] = "?"
		args = append(args, brand)
	}
	query := `SELECT seq,language,created_at,brand_name,classification,CAST(severity_level AS DOUBLE),(language=?),(language='en'),WEIGHT_STRING(language AS CHAR(2)),DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s.%f') FROM report_analysis FORCE INDEX (` + evidenceBrandCoreIndex + `) WHERE brand_name IN (` + strings.Join(marks, ",") + `) AND is_valid=TRUE AND classification='digital' AND seq IS NOT NULL`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("read evidence analysis core: %w", err)
	}
	defer rows.Close()
	selected := map[int]evidenceAnalysisCore{}
	for rows.Next() {
		var a evidenceAnalysisCore
		var lang, brand, classification sql.NullString
		var severity sql.NullFloat64
		var createdLiteral sql.NullString
		if err := rows.Scan(&a.Record.Seq, &lang, &a.Created, &brand, &classification, &severity, &a.Requested, &a.English, &a.LanguageWeight, &createdLiteral); err != nil {
			return nil, nil, err
		}
		a.Record.Language = lang.String
		a.LanguageNull = !lang.Valid
		a.CreatedLiteral = nullableEvidenceString(createdLiteral)
		a.Record.Brand = brand.String
		a.Record.Classification = classification.String
		a.Record.Severity = severity.Float64
		a.Record.SeverityNull = !severity.Valid
		if prior, ok := selected[a.Record.Seq]; !ok || preferredEvidenceAnalysis(a, prior) {
			selected[a.Record.Seq] = a
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	ids := make([]int, 0, len(selected))
	for seq := range selected {
		ids = append(ids, seq)
	}
	sort.Ints(ids)
	return selected, ids, nil
}

func nullableEvidenceString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func loadEvidenceMetadataCore(ctx context.Context, tx *sql.Tx, ids []int) (map[int]evidenceRecord, map[int]evidenceSourceNormalizationRow, []int, error) {
	query := `SELECT ids.seq,
 CASE WHEN m.seq IS NOT NULL THEN m.id ELSE r.id END,
 CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE r.public_id END,
 DATE_FORMAT(CASE WHEN m.seq IS NOT NULL THEN m.ts ELSE r.ts END,'%Y-%m-%d %H:%i:%s.%f'),
 CASE WHEN m.seq IS NOT NULL THEN m.latitude ELSE r.latitude END,
 CASE WHEN m.seq IS NOT NULL THEN m.longitude ELSE r.longitude END,
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.has_original_media ELSE LENGTH(r.image)>0 END,FALSE),
 rr.source_type,rr.source_id,COALESCE(rr.fetcher_id,''),
 CASE WHEN ev.evidence_version>0 AND ev.report_kind IN ('firsthand','coverage') THEN ev.report_kind ELSE 'unknown' END,
 COALESCE(ev.entity_relationship,'unknown'),COALESCE(ev.evidence_version,0),
 COALESCE(ev.evidence_excerpt<>'',FALSE),COALESCE(ev.reported_impact<>'',FALSE)
 FROM ` + evidenceIDsTable + `
 LEFT JOIN report_metadata_projection m ON m.seq=ids.seq
 LEFT JOIN reports r ON r.seq=CASE WHEN m.seq IS NULL THEN ids.seq ELSE NULL END
 LEFT JOIN report_raw rr ON rr.report_seq=ids.seq LEFT JOIN report_evidence ev ON ev.seq=ids.seq
 WHERE CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE r.public_id END<>'' AND ` + PublicVisibilityWhereSQL + `
 AND (SELECT COUNT(*) FROM report_status rs WHERE rs.seq=ids.seq AND rs.status IS NOT NULL AND rs.status<>'active')=0
 AND (SELECT COUNT(*) FROM reports_owners ro WHERE ro.seq=ids.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)=0`
	rows, err := tx.QueryContext(ctx, query, evidenceIDsJSON(ids))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read public evidence metadata: %w", err)
	}
	defer rows.Close()
	records := map[int]evidenceRecord{}
	sources := map[int]evidenceSourceNormalizationRow{}
	visible := []int{}
	for rows.Next() {
		var r evidenceRecord
		var source evidenceSourceNormalizationRow
		var reportID, ts, rawType, rawID sql.NullString
		var quote, impact bool
		if err := rows.Scan(&r.Seq, &reportID, &r.PublicID, &ts, &r.Latitude, &r.Longitude, &r.HasOriginalMedia, &rawType, &rawID, &source.FetcherID, &r.Kind, &r.Relationship, &r.Version, &quote, &impact); err != nil {
			return nil, nil, nil, err
		}
		if quote {
			r.Excerpt = "1"
		}
		if impact {
			r.Impact = "1"
		}
		source.Seq = r.Seq
		source.ReportID = reportID.String
		source.ReportTS = nullableEvidenceString(ts)
		source.RawSourceType = nullableEvidenceString(rawType)
		source.RawSourceID = nullableEvidenceString(rawID)
		records[r.Seq] = r
		sources[r.Seq] = source
		visible = append(visible, r.Seq)
	}
	return records, sources, visible, rows.Err()
}

func loadEvidenceIndexSources(ctx context.Context, tx *sql.Tx, ids []int, sources map[int]evidenceSourceNormalizationRow) error {
	// idx_seq carries the primary source/ID key. Read that covered identity first,
	// using SQL's actual collation; each report's first identity remains its
	// original source winner. Global primary-key order batches subsequent reads.
	rows, err := tx.QueryContext(ctx, `SELECT e.seq,e.source,e.external_id FROM `+evidenceIDsTable+` JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=ids.seq ORDER BY e.source,e.external_id`, evidenceIDsJSON(ids))
	if err != nil {
		return fmt.Errorf("read covered evidence source identities: %w", err)
	}
	type sourceIdentity struct {
		Seq    int    `json:"seq"`
		Source string `json:"source"`
		ID     string `json:"id"`
	}
	chosen := []sourceIdentity{}
	seen := map[int]bool{}
	for rows.Next() {
		var identity sourceIdentity
		if err := rows.Scan(&identity.Seq, &identity.Source, &identity.ID); err != nil {
			rows.Close()
			return err
		}
		if !seen[identity.Seq] {
			seen[identity.Seq] = true
			chosen = append(chosen, identity)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		return nil
	}
	payload, _ := json.Marshal(chosen)
	rows, err = tx.QueryContext(ctx, `SELECT ids.seq,ids.source,ids.id,
 CASE WHEN h.seq IS NOT NULL THEN h.source_url ELSE e.source_url END,
 DATE_FORMAT(CASE WHEN h.seq IS NOT NULL THEN h.source_timestamp ELSE e.source_timestamp END,'%Y-%m-%d %H:%i:%s.%f'),
 DATE_FORMAT(CASE WHEN h.seq IS NOT NULL THEN h.created_at ELSE e.created_at END,'%Y-%m-%d %H:%i:%s.%f')
 FROM JSON_TABLE(?, '$[*]' COLUMNS(seq INT PATH '$.seq',source VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.source',id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.id')) ids
 LEFT JOIN external_ingest_header_projection h ON h.seq=ids.seq AND h.source=ids.source AND h.external_id=ids.id
 LEFT JOIN external_ingest_index e FORCE INDEX(PRIMARY) ON e.source=CASE WHEN h.seq IS NULL THEN ids.source ELSE NULL END AND e.external_id=ids.id AND e.seq=ids.seq`, string(payload))
	if err != nil {
		return fmt.Errorf("hydrate ordered evidence source metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var platform, id, url, published, created sql.NullString
		if err := rows.Scan(&seq, &platform, &id, &url, &published, &created); err != nil {
			return err
		}
		s := sources[seq]
		s.EiiSource = nullableEvidenceString(platform)
		s.EiiID = nullableEvidenceString(id)
		s.EiiURL = nullableEvidenceString(url)
		s.EiiPublished = nullableEvidenceString(published)
		s.EiiCreated = nullableEvidenceString(created)
		sources[seq] = s
	}
	return rows.Err()
}

type evidenceWireHeader struct {
	ID  string
	Seq int
}

func loadEvidenceWireSources(ctx context.Context, tx *sql.Tx, ids []int, sources map[int]evidenceSourceNormalizationRow) error {
	// Sort only IDs/timestamps. Historical payloads can exceed MySQL's sort buffer.
	rows, err := tx.QueryContext(ctx, `SELECT ws.submission_id,ws.report_seq FROM `+evidenceIDsTable+` JOIN wire_submissions_raw ws ON ws.report_seq=ids.seq ORDER BY ws.report_seq,ws.submitted_at DESC`, evidenceIDsJSON(ids))
	if err != nil {
		return fmt.Errorf("read evidence Wire IDs: %w", err)
	}
	headers := []evidenceWireHeader{}
	seen := map[int]bool{}
	for rows.Next() {
		var h evidenceWireHeader
		if err := rows.Scan(&h.ID, &h.Seq); err != nil {
			rows.Close()
			return err
		}
		if !seen[h.Seq] {
			seen[h.Seq] = true
			headers = append(headers, h)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(headers) == 0 {
		return nil
	}
	wireIDs := make([]string, len(headers))
	for i, h := range headers {
		wireIDs[i] = h.ID
	}
	encoded, _ := json.Marshal(wireIDs)
	paths := []string{"$.digital_context.platform", "$.digital_context.legacy_source", "$.digital_context.metadata.source_url", "$.digital_context.source_url", "$.digital_context.url", "$.evidence_bundle[0].uri", "$.digital_context.metadata.source_published_at", "$.digital_context.metadata.author_did", "$.digital_context.metadata.author_username", "$.digital_context.metadata.author_handle", "$.digital_context.author_handle", "$.privacy.contains_pii", "$.privacy.requires_redaction"}
	columns := []string{"ws.report_seq", "DATE_FORMAT(ws.observed_at,'%Y-%m-%d %H:%i:%s.%f')", "DATE_FORMAT(ws.submitted_at,'%Y-%m-%d %H:%i:%s.%f')", "COALESCE(ws.actor_kind,'')", "COALESCE(ws.channel,'')"}
	for i, path := range paths {
		node := "JSON_EXTRACT(ws.report_json,'" + path + "')"
		if i >= 2 && i <= 5 {
			// NonHTTP URLs already have no public/canonical identity. Preserve
			// missing and literal null nodes, whose fallback behavior differs;
			// keep other nonHTTP values as blockers without transferring images.
			node = "CASE WHEN " + node + " IS NULL THEN NULL WHEN JSON_UNQUOTE(" + node + ")='null' OR LOWER(LEFT(TRIM(JSON_UNQUOTE(" + node + ")),8)) REGEXP '^https?://' THEN " + node + " ELSE JSON_QUOTE('__non_http_source_uri__') END"
		}
		columns = append(columns, node)
	}
	columns = append(columns, "JSON_EXTRACT(ws.provenance_json,'$.generation_method')", "JSON_EXTRACT(ws.extensions_json,'$.observation_time_supplied')")
	rows, err = tx.QueryContext(ctx, `SELECT `+strings.Join(columns, ",")+` FROM JSON_TABLE(?, '$[*]' COLUMNS(id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$')) ids JOIN wire_submissions_raw ws ON ws.submission_id=ids.id`, string(encoded))
	if err != nil {
		return fmt.Errorf("read selected evidence Wire metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var observed, submitted sql.NullString
		var actor, channel string
		nodes := make([][]byte, len(paths)+2)
		dest := []interface{}{&seq, &observed, &submitted, &actor, &channel}
		for i := range nodes {
			dest = append(dest, &nodes[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		s := sources[seq]
		s.WireObserved = nullableEvidenceString(observed)
		s.WireSubmitted = nullableEvidenceString(submitted)
		s.WireActor = actor
		s.WireChannel = channel
		platform := map[string]json.RawMessage{}
		metadata := map[string]json.RawMessage{}
		privacy := map[string]json.RawMessage{}
		put := func(m map[string]json.RawMessage, key string, node []byte) {
			if node != nil {
				m[key] = json.RawMessage(node)
			}
		}
		put(platform, "platform", nodes[0])
		put(platform, "legacy_source", nodes[1])
		put(metadata, "source_url", nodes[2])
		put(platform, "source_url", nodes[3])
		put(platform, "url", nodes[4])
		put(metadata, "source_published_at", nodes[6])
		put(metadata, "author_did", nodes[7])
		put(metadata, "author_username", nodes[8])
		put(metadata, "author_handle", nodes[9])
		put(platform, "author_handle", nodes[10])
		put(privacy, "contains_pii", nodes[11])
		put(privacy, "requires_redaction", nodes[12])
		metaJSON, _ := json.Marshal(metadata)
		platform["metadata"] = metaJSON
		dcJSON, _ := json.Marshal(platform)
		privacyJSON, _ := json.Marshal(privacy)
		report := map[string]json.RawMessage{"digital_context": dcJSON, "privacy": privacyJSON}
		if nodes[5] != nil {
			bundle, _ := json.Marshal([]map[string]json.RawMessage{{"uri": nodes[5]}})
			report["evidence_bundle"] = bundle
		}
		s.WireReport, _ = json.Marshal(report)
		provenance := map[string]json.RawMessage{}
		extensions := map[string]json.RawMessage{}
		put(provenance, "generation_method", nodes[len(paths)])
		put(extensions, "observation_time_supplied", nodes[len(paths)+1])
		s.WireProvenance, _ = json.Marshal(provenance)
		s.WireExtensions, _ = json.Marshal(extensions)
		sources[seq] = s
	}
	return rows.Err()
}

func loadEvidenceCaseReferences(ctx context.Context, tx *sql.Tx, ids []int) (map[int]evidenceCaseReference, error) {
	rows, err := tx.QueryContext(ctx, `SELECT cr.seq,MIN(cr.case_id) AS case_id FROM `+evidenceIDsTable+` JOIN case_reports cr ON cr.seq=ids.seq GROUP BY cr.seq ORDER BY case_id`, evidenceIDsJSON(ids))
	if err != nil {
		return nil, fmt.Errorf("read evidence case references: %w", err)
	}
	defer rows.Close()
	refs := map[int]evidenceCaseReference{}
	order := -1
	previous := ""
	for rows.Next() {
		var seq int
		var id string
		if err := rows.Scan(&seq, &id); err != nil {
			return nil, err
		}
		if order < 0 || id != previous {
			order++
			previous = id
		}
		refs[seq] = evidenceCaseReference{ID: id, Order: order}
	}
	return refs, rows.Err()
}

func hydrateEvidenceDisplay(ctx context.Context, tx *sql.Tx, f EvidenceFilter, chosen map[int]evidenceAnalysisCore, grouped *evidenceGroupedPage) error {
	ids := []int{}
	seen := map[int]bool{}
	for _, r := range grouped.Records {
		if !seen[r.Seq] {
			ids = append(ids, r.Seq)
			seen[r.Seq] = true
		}
		for _, sample := range grouped.SupportingSamples[r.Seq] {
			if !seen[sample.Seq] {
				ids = append(ids, sample.Seq)
				seen[sample.Seq] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	// Historical analyses have no unique row key. Restrict hydration to the
	// selected core's exact nullable metadata, so tied versions cannot produce a
	// hybrid title/summary from a different brand or severity analysis.
	type displaySelection struct {
		Seq                   int     `json:"seq"`
		Language              *string `json:"language"`
		Created               *string `json:"created"`
		Brand, Classification string
		Severity              *float64 `json:"severity"`
	}
	selections := make([]displaySelection, 0, len(ids))
	for _, seq := range ids {
		core := chosen[seq]
		selection := displaySelection{Seq: seq, Created: core.CreatedLiteral, Brand: core.Record.Brand, Classification: core.Record.Classification}
		if !core.LanguageNull {
			language := core.Record.Language
			selection.Language = &language
		}
		if !core.Record.SeverityNull {
			severity := core.Record.Severity
			selection.Severity = &severity
		}
		selections = append(selections, selection)
	}
	payload, _ := json.Marshal(selections)
	rows, err := tx.QueryContext(ctx, `WITH display AS (SELECT ra.seq,ra.title,ra.summary,ra.brand_display_name,ROW_NUMBER() OVER(PARTITION BY ra.seq ORDER BY (ra.language=?) DESC,(ra.language='en') DESC,ra.language ASC,ra.created_at DESC) AS rank_no
 FROM JSON_TABLE(?, '$[*]' COLUMNS(seq INT PATH '$.seq',language VARCHAR(2) PATH '$.language',created VARCHAR(26) PATH '$.created',brand VARCHAR(255) PATH '$.Brand',classification VARCHAR(32) PATH '$.Classification',severity DOUBLE PATH '$.severity')) selected
 JOIN report_analysis ra ON ra.seq=selected.seq
 WHERE ra.is_valid=TRUE AND BINARY ra.language <=> BINARY selected.language AND BINARY DATE_FORMAT(ra.created_at,'%Y-%m-%d %H:%i:%s.%f') <=> BINARY selected.created AND BINARY ra.brand_name <=> BINARY selected.brand AND BINARY ra.classification <=> BINARY selected.classification AND CAST(ra.severity_level AS DOUBLE) <=> selected.severity)
 SELECT seq,COALESCE(title,''),COALESCE(summary,''),COALESCE(brand_display_name,'') FROM display WHERE rank_no=1`, f.Language, string(payload))
	if err != nil {
		return fmt.Errorf("hydrate evidence analysis: %w", err)
	}
	display := map[int]evidenceRecord{}
	for rows.Next() {
		var r evidenceRecord
		if err := rows.Scan(&r.Seq, &r.Title, &r.Summary, &r.BrandDisplay); err != nil {
			rows.Close()
			return err
		}
		display[r.Seq] = r
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	pageIDs := make([]int, len(grouped.Records))
	for i, r := range grouped.Records {
		pageIDs[i] = r.Seq
	}
	rows, err = tx.QueryContext(ctx, `SELECT ev.seq,COALESCE(ev.evidence_excerpt,''),COALESCE(ev.reported_impact,''),COALESCE(ev.classification_reason,'') FROM `+evidenceIDsTable+` JOIN report_evidence ev ON ev.seq=ids.seq`, evidenceIDsJSON(pageIDs))
	if err != nil {
		return fmt.Errorf("hydrate original evidence fields: %w", err)
	}
	evidenceText := map[int]evidenceRecord{}
	for rows.Next() {
		var r evidenceRecord
		if err := rows.Scan(&r.Seq, &r.Excerpt, &r.Impact, &r.Reason); err != nil {
			rows.Close()
			return err
		}
		evidenceText[r.Seq] = r
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i := range grouped.Records {
		r := &grouped.Records[i]
		d := display[r.Seq]
		r.Title = d.Title
		r.Summary = d.Summary
		r.BrandDisplay = d.BrandDisplay
		text := evidenceText[r.Seq]
		r.Excerpt = text.Excerpt
		r.Impact = text.Impact
		r.Reason = text.Reason
		samples := []models.PublicSupportingSource{}
		for _, sample := range grouped.SupportingSamples[r.Seq] {
			samples = append(samples, models.PublicSupportingSource{Title: display[sample.Seq].Title, SourceURL: evidenceTextPointer(sample.SourceURL), ReportKind: sample.Kind, SourcePublishedAt: sample.PublishedAt})
		}
		r.SupportingSourcesJSON, _ = json.Marshal(samples)
	}
	return nil
}

func evidenceTextPointer(s string) *string { return &s }

func loadEvidenceBrandPageBulk(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (EvidenceBrandPage, error) {
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}}
	analysis, ids, err := loadEvidenceAnalysisCore(ctx, tx, f)
	if err != nil {
		return page, err
	}
	if len(ids) == 0 {
		return page, nil
	}
	records, sources, visible, err := loadEvidenceMetadataCore(ctx, tx, ids)
	if err != nil {
		return page, err
	}
	if len(visible) == 0 {
		return page, nil
	}
	if err = loadEvidenceIndexSources(ctx, tx, visible, sources); err != nil {
		return page, err
	}
	if err = loadEvidenceWireSources(ctx, tx, visible, sources); err != nil {
		return page, err
	}
	inputs := make([]evidenceSourceNormalizationRow, 0, len(visible))
	for _, seq := range visible {
		inputs = append(inputs, sources[seq])
	}
	normalized, err := normalizeEvidenceSources(ctx, tx, inputs)
	if err != nil {
		return page, fmt.Errorf("normalize evidence sources: %w", err)
	}
	all := make([]evidenceRecord, 0, len(visible))
	for _, seq := range visible {
		r := normalized[seq]
		base := records[seq]
		a := analysis[seq].Record
		r.Seq = seq
		r.PublicID = base.PublicID
		r.Latitude = base.Latitude
		r.Longitude = base.Longitude
		r.HasOriginalMedia = base.HasOriginalMedia
		r.Kind = base.Kind
		r.Relationship = base.Relationship
		r.Version = base.Version
		r.Excerpt = base.Excerpt
		r.Impact = base.Impact
		r.Language = a.Language
		r.Brand = a.Brand
		r.Classification = a.Classification
		r.Severity = a.Severity
		r.SeverityNull = a.SeverityNull
		all = append(all, r)
	}
	cases, err := loadEvidenceCaseReferences(ctx, tx, visible)
	if err != nil {
		return page, err
	}
	grouped := groupEvidenceRecords(all, cases, f)
	if err := hydrateEvidenceDisplay(ctx, tx, f, analysis, &grouped); err != nil {
		return page, err
	}
	mediaSeqs := []int{}
	for _, r := range grouped.Records {
		if r.HasOriginalMedia && !r.PrivacyRestricted {
			mediaSeqs = append(mediaSeqs, r.Seq)
		}
	}
	valid, err := loadEvidenceImageValidity(ctx, tx, mediaSeqs)
	if err != nil {
		return page, err
	}
	for _, r := range grouped.Records {
		// SQL NULL ranks last. Apply the existing public epoch fallback only
		// after grouping so it cannot fabricate a source-recency signal.
		if r.IngestedAtNull {
			r.IngestedAt = time.Unix(0, 0).UTC()
		}
		ev := r.publicEvidence()
		ev.HasOriginalMedia = ev.HasOriginalMedia && valid[r.Seq]
		rec := r.publicRecurrence()
		page.Reports = append(page.Reports, models.ReportWithMinimalAnalysis{Report: models.Report{Seq: r.Seq, PublicID: r.PublicID, Timestamp: r.IngestedAt, Latitude: r.Latitude, Longitude: r.Longitude, SourceTimestamp: ev.SourcePublishedAt, SourceURL: ev.SourceURL}, Analysis: []models.MinimalAnalysis{{Title: r.Title, Summary: r.Summary, Classification: r.Classification, SeverityLevel: r.Severity, Language: r.Language, BrandName: r.Brand, BrandDisplayName: r.BrandDisplay}}, Evidence: &ev, Recurrence: &rec})
	}
	page.Total = grouped.Total
	page.High = grouped.High
	page.Medium = grouped.Medium
	page.KindCounts = grouped.KindCounts
	page.HasMore = grouped.HasMore
	return page, nil
}

func (d *Database) GetEvidenceBrandPage(ctx context.Context, f EvidenceFilter) (EvidenceBrandPage, error) {
	f, err := NormalizeEvidenceFilter(f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	queryCtx, cancel := withPublicDiscoveryTimeout(ctx)
	defer cancel()
	tx, err := d.db.BeginTx(queryCtx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	defer tx.Rollback()
	page, err := loadEvidenceBrandPageBulk(queryCtx, tx, f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return EvidenceBrandPage{}, err
	}
	return page, nil
}
