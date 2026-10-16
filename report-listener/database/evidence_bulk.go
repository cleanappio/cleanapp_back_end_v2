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

	"cleanapp-common/wiremeta"
	"report-listener/models"
)

// Each pass reads indexed, bounded metadata in the same repeatable-read snapshot.
// Full analysis/evidence text is fetched only after exact source/family grouping.
// In particular, neither source JSON nor report images participate in a sort.
type evidenceAnalysisCore struct {
	Record             evidenceAnalysisRecord
	Created            sql.NullTime
	CreatedLiteral     *string
	LanguageNull       bool
	Requested, English sql.NullInt64
	LanguageWeight     []byte
}

// Keep full-cohort maps separate from the much larger public/display record.
// Metadata values fit Go's inline map-value limit instead of allocating one
// mostly empty display record per report before pagination.
type evidenceAnalysisRecord struct {
	Seq                             int
	Language, Brand, Classification string
	Severity                        float64
	SeverityNull                    bool
}

type evidenceMetadataRecord struct {
	Seq                 int
	PublicID            string
	Latitude, Longitude float64
	HasOriginalMedia    bool
	Kind, Relationship  string
	Version             int
	Excerpt, Impact     string
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

const evidenceMetadataColumns = ` CASE WHEN m.seq IS NOT NULL THEN m.id ELSE r.id END,
 CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE r.public_id END,
 DATE_FORMAT(CASE WHEN m.seq IS NOT NULL THEN m.ts ELSE r.ts END,'%Y-%m-%d %H:%i:%s.%f'),
 CASE WHEN m.seq IS NOT NULL THEN m.latitude ELSE r.latitude END,
 CASE WHEN m.seq IS NOT NULL THEN m.longitude ELSE r.longitude END,
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.has_original_media ELSE LENGTH(r.image)>0 END,FALSE),
 rr.source_type,rr.source_id,COALESCE(rr.fetcher_id,''),
 CASE WHEN ev.evidence_version>0 AND ev.report_kind IN ('firsthand','coverage') THEN ev.report_kind ELSE 'unknown' END,
 COALESCE(ev.entity_relationship,'unknown'),COALESCE(ev.evidence_version,0),
 COALESCE(ev.evidence_excerpt<>'',FALSE),COALESCE(ev.reported_impact<>'',FALSE)`

const evidenceMetadataJoins = `
 LEFT JOIN report_metadata_projection m ON m.seq=ids.seq
 LEFT JOIN reports r ON r.seq=CASE WHEN m.seq IS NULL THEN ids.seq ELSE NULL END
 LEFT JOIN report_raw rr ON rr.report_seq=ids.seq LEFT JOIN report_evidence ev ON ev.seq=ids.seq
 LEFT JOIN report_status rs ON rs.seq=ids.seq AND rs.status IS NOT NULL AND rs.status<>'active'
 LEFT JOIN reports_owners ro ON ro.seq=ids.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE`

const evidenceMetadataVisible = `CASE WHEN m.seq IS NOT NULL THEN m.public_id<>'' ELSE r.public_id<>'' END AND ` + PublicVisibilityWhereSQL + `
 AND rs.seq IS NULL AND ro.seq IS NULL`

func loadEvidenceMetadataCore(ctx context.Context, tx *sql.Tx, ids []int) (map[int]evidenceMetadataRecord, map[int]evidenceSourceNormalizationRow, []int, error) {
	query := `SELECT ids.seq,` + evidenceMetadataColumns + ` FROM ` + evidenceIDsTable + evidenceMetadataJoins + ` WHERE ` + evidenceMetadataVisible
	rows, err := tx.QueryContext(ctx, query, evidenceIDsJSON(ids))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read public evidence metadata: %w", err)
	}
	defer rows.Close()
	records := make(map[int]evidenceMetadataRecord, len(ids))
	sources := make(map[int]evidenceSourceNormalizationRow, len(ids))
	visible := make([]int, 0, len(ids))
	for rows.Next() {
		var r evidenceMetadataRecord
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

// Drive publication joins from the indexed brand range rather than streaming
// all analysis rows to Go, encoding their IDs, then streaming metadata again.
// Publication depends only on seq, so it filters every translation identically;
// the existing Go ranking still chooses the exact preferred analysis per seq.
func loadEvidenceAnalysisMetadataCore(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (map[int]evidenceAnalysisCore, map[int]evidenceMetadataRecord, map[int]evidenceSourceNormalizationRow, []int, error) {
	marks := make([]string, len(f.Brands))
	args := []any{f.Language}
	for i, brand := range f.Brands {
		marks[i] = "?"
		args = append(args, brand)
	}
	query := `SELECT ids.seq,ids.language,ids.created_at,ids.brand_name,ids.classification,CAST(ids.severity_level AS DOUBLE),(ids.language=?),(ids.language='en'),WEIGHT_STRING(ids.language AS CHAR(2)),DATE_FORMAT(ids.created_at,'%Y-%m-%d %H:%i:%s.%f'),` + evidenceMetadataColumns + `
 FROM report_analysis ids FORCE INDEX (` + evidenceBrandCoreIndex + `)` + evidenceMetadataJoins + `
 WHERE ids.brand_name IN (` + strings.Join(marks, ",") + `) AND ids.is_valid=TRUE AND ids.classification='digital' AND ids.seq IS NOT NULL AND ` + evidenceMetadataVisible
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read public evidence analysis and metadata: %w", err)
	}
	defer rows.Close()
	selected := map[int]evidenceAnalysisCore{}
	records := map[int]evidenceMetadataRecord{}
	sources := map[int]evidenceSourceNormalizationRow{}
	// Reuse scan buffers; values stored in maps are copied, and nullable string
	// pointers own their string value rather than pointing into these buffers.
	var a evidenceAnalysisCore
	var r evidenceMetadataRecord
	var source evidenceSourceNormalizationRow
	var lang, brand, classification, createdLiteral sql.NullString
	var severity sql.NullFloat64
	var reportID, ts, rawType, rawID sql.NullString
	var quote, impact bool
	for rows.Next() {
		a, r, source = evidenceAnalysisCore{}, evidenceMetadataRecord{}, evidenceSourceNormalizationRow{}
		if err := rows.Scan(&a.Record.Seq, &lang, &a.Created, &brand, &classification, &severity, &a.Requested, &a.English, &a.LanguageWeight, &createdLiteral,
			&reportID, &r.PublicID, &ts, &r.Latitude, &r.Longitude, &r.HasOriginalMedia, &rawType, &rawID, &source.FetcherID, &r.Kind, &r.Relationship, &r.Version, &quote, &impact); err != nil {
			return nil, nil, nil, nil, err
		}
		a.Record.Language, a.LanguageNull = lang.String, !lang.Valid
		a.CreatedLiteral = nullableEvidenceString(createdLiteral)
		a.Record.Brand, a.Record.Classification = brand.String, classification.String
		a.Record.Severity, a.Record.SeverityNull = severity.Float64, !severity.Valid
		seq := a.Record.Seq
		if prior, ok := selected[seq]; ok && !preferredEvidenceAnalysis(a, prior) {
			continue
		}
		selected[seq] = a
		r.Seq = seq
		if quote {
			r.Excerpt = "1"
		}
		if impact {
			r.Impact = "1"
		}
		source.Seq, source.ReportID = seq, reportID.String
		source.ReportTS = nullableEvidenceString(ts)
		source.RawSourceType, source.RawSourceID = nullableEvidenceString(rawType), nullableEvidenceString(rawID)
		records[seq], sources[seq] = r, source
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	visible := make([]int, 0, len(selected))
	for seq := range selected {
		visible = append(visible, seq)
	}
	sort.Ints(visible)
	return selected, records, sources, visible, nil
}

const evidenceIndexSourcesSQL = `SELECT e.seq,e.source,e.external_id,
 CASE WHEN h.seq IS NOT NULL THEN h.source_url ELSE fallback.source_url END,
 DATE_FORMAT(CASE WHEN h.seq IS NOT NULL THEN h.source_timestamp ELSE fallback.source_timestamp END,'%Y-%m-%d %H:%i:%s.%f'),
 DATE_FORMAT(CASE WHEN h.seq IS NOT NULL THEN h.created_at ELSE fallback.created_at END,'%Y-%m-%d %H:%i:%s.%f'),
 (h.seq IS NOT NULL OR fallback.seq IS NOT NULL)
 FROM ` + evidenceIDsTable + ` JOIN external_ingest_index e FORCE INDEX(idx_seq) ON e.seq=ids.seq
 LEFT JOIN external_ingest_header_projection h ON h.seq=e.seq AND h.source=e.source AND h.external_id=e.external_id
 LEFT JOIN external_ingest_index fallback FORCE INDEX(PRIMARY) ON fallback.source=CASE WHEN h.seq IS NULL THEN e.source ELSE NULL END AND fallback.external_id=e.external_id AND fallback.seq=e.seq`

func loadEvidenceIndexSources(ctx context.Context, tx *sql.Tx, ids []int, sources map[int]evidenceSourceNormalizationRow) error {
	// Keep e covered: selecting its header columns directly would fetch every
	// parent row before knowing whether the compact projection matches. The
	// separate fallback alias reads a parent only when that projection is absent.
	// Hydrate singleton winners in this pass, avoiding another full-cohort JSON
	// encode/JSON_TABLE round trip. SQL collation still selects ambiguous winners.
	counts := make(map[int]int, len(ids))
	read := func(query string, batch []int, ambiguous bool) error {
		rows, err := tx.QueryContext(ctx, query, evidenceIDsJSON(batch))
		if err != nil {
			return fmt.Errorf("read evidence source headers: %w", err)
		}
		defer rows.Close()
		var seen map[int]bool
		if ambiguous {
			seen = make(map[int]bool, len(batch))
		}
		for rows.Next() {
			var seq int
			var platform, id, url, published, created sql.NullString
			var metadataPresent bool
			if err := rows.Scan(&seq, &platform, &id, &url, &published, &created, &metadataPresent); err != nil {
				return err
			}
			if !metadataPresent || !platform.Valid || !id.Valid {
				return fmt.Errorf("selected evidence source metadata is incomplete")
			}
			if ambiguous {
				if counts[seq] < 2 {
					return fmt.Errorf("ambiguous evidence source has an invalid report identity")
				}
				if seen[seq] {
					continue
				}
				seen[seq] = true
			} else {
				counts[seq]++
				if counts[seq] > 1 {
					continue
				}
			}
			s := sources[seq]
			s.EiiSource, s.EiiID, s.EiiURL = nullableEvidenceString(platform), nullableEvidenceString(id), nullableEvidenceString(url)
			s.EiiPublished, s.EiiCreated = nullableEvidenceString(published), nullableEvidenceString(created)
			sources[seq] = s
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if ambiguous && len(seen) != len(batch) {
			return fmt.Errorf("ambiguous evidence source metadata is incomplete")
		}
		return nil
	}
	if err := read(evidenceIndexSourcesSQL, ids, false); err != nil {
		return err
	}
	duplicates := []int{}
	for seq, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, seq)
		}
	}
	if len(duplicates) == 0 {
		return nil
	}
	sort.Ints(duplicates)
	return read(evidenceIndexSourcesSQL+` ORDER BY e.source,e.external_id`, duplicates, true)
}

type evidenceWireHeader struct {
	ID  string `json:"id"`
	Seq int    `json:"seq"`
}

func loadEvidenceWireSources(ctx context.Context, tx *sql.Tx, ids []int, sources map[int]evidenceSourceNormalizationRow) error {
	// The report-sequence secondary index includes the submission primary key.
	// Keep timestamp/parent reads for duplicate groups only, and let SQL preserve
	// the submission ID's actual collation order for subsequent primary reads.
	rows, err := tx.QueryContext(ctx, `SELECT ws.submission_id,ws.report_seq FROM `+evidenceIDsTable+` STRAIGHT_JOIN wire_submissions_raw ws ON ws.report_seq=ids.seq ORDER BY ws.submission_id`, evidenceIDsJSON(ids))
	if err != nil {
		return fmt.Errorf("read evidence Wire IDs: %w", err)
	}
	headers := []evidenceWireHeader{}
	counts := map[int]int{}
	for rows.Next() {
		var h evidenceWireHeader
		if err := rows.Scan(&h.ID, &h.Seq); err != nil {
			rows.Close()
			return err
		}
		counts[h.Seq]++
		headers = append(headers, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(headers) == 0 {
		return nil
	}
	duplicates := []int{}
	for seq, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, seq)
		}
	}
	selected := map[int]string{}
	if len(duplicates) > 0 {
		sort.Ints(duplicates)
		// Retain the previous latest/tied selection exactly for the reports
		// whose timestamps are necessary to choose a submission.
		rows, err = tx.QueryContext(ctx, `SELECT ws.submission_id,ws.report_seq FROM `+evidenceIDsTable+` JOIN wire_submissions_raw ws ON ws.report_seq=ids.seq ORDER BY ws.report_seq,ws.submitted_at DESC`, evidenceIDsJSON(duplicates))
		if err != nil {
			return fmt.Errorf("read duplicate evidence Wire IDs: %w", err)
		}
		for rows.Next() {
			var h evidenceWireHeader
			if err := rows.Scan(&h.ID, &h.Seq); err != nil {
				rows.Close()
				return err
			}
			if _, exists := selected[h.Seq]; !exists {
				selected[h.Seq] = h.ID
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	winners := headers[:0]
	for _, h := range headers {
		if counts[h.Seq] == 1 || selected[h.Seq] == h.ID {
			winners = append(winners, h)
		}
	}
	headers = winners
	encoded, _ := json.Marshal(headers)
	// Covered IDs above remain authoritative. A projection with a stale report
	// link (a parent FK SET NULL does not fire Wire triggers) cannot match it.
	// SQL NULL nodes in a matching projection are authoritative missing nodes;
	// COALESCE would incorrectly resurrect values from the original payload.
	choose := func(projected, original string) string {
		return "CASE WHEN h.submission_id IS NOT NULL THEN " + projected + " ELSE " + original + " END"
	}
	columns := []string{"ids.seq",
		"DATE_FORMAT(" + choose("h.observed_at", "ws.observed_at") + ",'%Y-%m-%d %H:%i:%s.%f')",
		"DATE_FORMAT(" + choose("h.submitted_at", "ws.submitted_at") + ",'%Y-%m-%d %H:%i:%s.%f')",
		"COALESCE(" + choose("h.actor_kind", "ws.actor_kind") + ",'')",
		"COALESCE(" + choose("h.channel", "ws.channel") + ",'')"}
	nodeColumns, originalNodes := wiremeta.NodeColumns(), wiremeta.NodeExpressions("ws")
	for i, name := range nodeColumns {
		columns = append(columns, choose("h."+name, originalNodes[i]))
	}
	rows, err = tx.QueryContext(ctx, `SELECT `+strings.Join(columns, ",")+` FROM JSON_TABLE(?, '$[*]' COLUMNS(id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.id',seq INT PATH '$.seq')) ids
 LEFT JOIN wire_evidence_projection h ON h.submission_id=ids.id AND h.report_seq=ids.seq
 LEFT JOIN wire_submissions_raw ws ON ws.submission_id=CASE WHEN h.submission_id IS NULL THEN ids.id ELSE NULL END AND ws.report_seq=ids.seq
 WHERE h.submission_id IS NOT NULL OR ws.submission_id IS NOT NULL`, string(encoded))
	if err != nil {
		return fmt.Errorf("read selected evidence Wire metadata: %w", err)
	}
	defer rows.Close()
	loaded := map[int]bool{}
	for rows.Next() {
		var seq int
		var observed, submitted sql.NullString
		var actor, channel string
		nodes := make([][]byte, len(nodeColumns))
		dest := []interface{}{&seq, &observed, &submitted, &actor, &channel}
		for i := range nodes {
			dest = append(dest, &nodes[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		if loaded[seq] || counts[seq] == 0 {
			return fmt.Errorf("selected evidence Wire metadata has an invalid report identity")
		}
		loaded[seq] = true
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
		put(provenance, "generation_method", nodes[13])
		put(extensions, "observation_time_supplied", nodes[14])
		s.WireProvenance, _ = json.Marshal(provenance)
		s.WireExtensions, _ = json.Marshal(extensions)
		sources[seq] = s
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(loaded) != len(headers) {
		return fmt.Errorf("selected evidence Wire metadata is incomplete")
	}
	return nil
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
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}, Overview: evidenceOverview(nil, f.asOf)}
	analysis, records, sources, visible, err := loadEvidenceAnalysisMetadataCore(ctx, tx, f)
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
	page.Overview = evidenceOverview(all, f.asOf)
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
