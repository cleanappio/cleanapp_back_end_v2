package database

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"report-listener/models"
	"report-listener/publicid"
)

const PublicBrandPreviewLimit = 6

var previewLanguage = regexp.MustCompile(`^[a-z]{2}$`)

func NormalizeBrandPreviewFilter(f EvidenceFilter) (EvidenceFilter, error) {
	if f.Limit <= 0 {
		return f, fmt.Errorf("n must be a positive integer")
	}
	if f.Limit > PublicBrandPreviewLimit {
		f.Limit = PublicBrandPreviewLimit
	}
	f.Language = strings.ToLower(strings.TrimSpace(f.Language))
	if f.Language == "" {
		f.Language = "en"
	}
	if !previewLanguage.MatchString(f.Language) || strings.IndexFunc(f.BrandName, unicode.IsControl) >= 0 {
		return f, fmt.Errorf("invalid brand_name or lang")
	}
	f.Offset = 0
	f.Product = ""
	return NormalizeEvidenceFilter(f)
}

// Each exact alias has its own index-ordered LIMIT before the small union is
// deduplicated. There is no full-brand grouping, count, offset or recent-global
// sample that can hide a less common brand.
func brandPreviewCandidatesQuery(f EvidenceFilter, limit int) (string, []interface{}) {
	parts := make([]string, 0, len(f.Brands))
	args := []interface{}{}
	for index, brand := range f.Brands {
		kindWhere := ""
		args = append(args, brand)
		if f.Kind == "firsthand" || f.Kind == "coverage" {
			kindWhere = " AND EXISTS (SELECT 1 FROM report_evidence ev WHERE ev.seq=ra.seq AND ev.evidence_version>0 AND ev.report_kind=?)"
			args = append(args, f.Kind)
		} else if f.Kind == "unknown" {
			kindWhere = " AND NOT EXISTS (SELECT 1 FROM report_evidence ev WHERE ev.seq=ra.seq AND ev.evidence_version>0 AND ev.report_kind IN ('firsthand','coverage'))"
		}
		parts = append(parts, fmt.Sprintf(`SELECT seq FROM (
 SELECT DISTINCT STRAIGHT_JOIN ra.seq FROM report_analysis ra FORCE INDEX (%s)
 INNER JOIN reports r ON r.seq=ra.seq
 WHERE ra.brand_name=? AND ra.is_valid=TRUE AND ra.classification='digital'
 AND r.public_id REGEXP '^rpt_[A-Za-z0-9_-]{22}$' AND %s%s
 ORDER BY ra.seq DESC LIMIT ?
) brand_%d`, evidenceBrandCoreIndex, publicReportCurrentVisibilitySQL, kindWhere, index))
		args = append(args, limit)
	}
	query := "SELECT DISTINCT preview_candidates.seq FROM (" + strings.Join(parts, " UNION ALL ") + ") preview_candidates ORDER BY preview_candidates.seq DESC LIMIT ?"
	return query, append(args, limit)
}

func loadBrandPreviewIDs(ctx context.Context, tx *sql.Tx, f EvidenceFilter, limit int) ([]int, error) {
	query, args := brandPreviewCandidatesQuery(f, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		if seq <= 0 || len(ids) >= limit {
			return nil, fmt.Errorf("invalid bounded preview identity")
		}
		ids = append(ids, seq)
	}
	return ids, rows.Err()
}

// Only scalar metadata and original excerpt text are selected for <=6 reports.
// Read complete text before redaction, then bound the public projection: SQL
// truncation could hide a sensitive value that crosses the output boundary.
// No image, coordinate, contact or raw JSON is loaded by this projection.
const brandPreviewMetadataQuery = `SELECT ids.seq,r.id,r.public_id,DATE_FORMAT(r.ts,'%Y-%m-%d %H:%i:%s.%f'),
 rr.source_type,rr.source_id,COALESCE(rr.fetcher_id,''),
 CASE WHEN ev.evidence_version>0 AND ev.report_kind IN ('firsthand','coverage') THEN ev.report_kind ELSE 'unknown' END,
 COALESCE(ev.evidence_version,0),COALESCE(ev.evidence_excerpt,'')
 FROM ` + evidenceIDsTable + ` JOIN reports r ON r.seq=ids.seq
 LEFT JOIN report_raw rr ON rr.report_seq=ids.seq LEFT JOIN report_evidence ev ON ev.seq=ids.seq
 WHERE r.public_id<>'' AND ` + publicReportCurrentVisibilitySQL

func loadBrandPreviewMetadata(ctx context.Context, tx *sql.Tx, ids []int) (map[int]evidenceRecord, map[int]evidenceSourceNormalizationRow, []int, error) {
	rows, err := tx.QueryContext(ctx, brandPreviewMetadataQuery, evidenceIDsJSON(ids))
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	records := map[int]evidenceRecord{}
	sources := map[int]evidenceSourceNormalizationRow{}
	visible := []int{}
	for rows.Next() {
		var r evidenceRecord
		var source evidenceSourceNormalizationRow
		var timestamp, rawType, rawID sql.NullString
		if err := rows.Scan(&r.Seq, &source.ReportID, &r.PublicID, &timestamp, &rawType, &rawID, &source.FetcherID, &r.Kind, &r.Version, &r.Excerpt); err != nil {
			return nil, nil, nil, err
		}
		if !publicid.IsReportID(r.PublicID) {
			continue
		}
		source.Seq = r.Seq
		source.ReportTS = nullableEvidenceString(timestamp)
		source.RawSourceType = nullableEvidenceString(rawType)
		source.RawSourceID = nullableEvidenceString(rawID)
		records[r.Seq], sources[r.Seq] = r, source
		visible = append(visible, r.Seq)
	}
	return records, sources, visible, rows.Err()
}

func loadBrandPreviewAnalysis(ctx context.Context, tx *sql.Tx, f EvidenceFilter, ids []int) (map[int]evidenceRecord, error) {
	marks := make([]string, len(f.Brands))
	args := []interface{}{f.Language, evidenceIDsJSON(ids)}
	for i, brand := range f.Brands {
		marks[i] = "?"
		args = append(args, brand)
	}
	query := `WITH preview_analysis AS (
 SELECT ra.seq,COALESCE(ra.title,'') AS title,COALESCE(ra.summary,'') AS summary,
 COALESCE(ra.brand_display_name,'') AS brand_display_name,COALESCE(ra.language,'') AS language,
 ROW_NUMBER() OVER(PARTITION BY ra.seq ORDER BY (ra.language=?) DESC,(ra.language='en') DESC,ra.language ASC,ra.created_at DESC,ra.brand_name ASC) AS rank_no
 FROM ` + evidenceIDsTable + ` STRAIGHT_JOIN report_analysis ra ON ra.seq=ids.seq
 WHERE ra.is_valid=TRUE AND ra.classification='digital' AND ra.brand_name IN (` + strings.Join(marks, ",") + `))
 SELECT seq,title,summary,brand_display_name,language FROM preview_analysis WHERE rank_no=1`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int]evidenceRecord{}
	for rows.Next() {
		var r evidenceRecord
		if err := rows.Scan(&r.Seq, &r.Title, &r.Summary, &r.BrandDisplay, &r.Language); err != nil {
			return nil, err
		}
		result[r.Seq] = r
	}
	return result, rows.Err()
}

func previewText(value string, limit int, fallback string) string {
	if sensitiveEvidence.MatchString(value) || hasUnsafeEvidenceControls(value) {
		return fallback
	}
	if text := evidenceText(value, limit); text != nil {
		return *text
	}
	return fallback
}

func publicBrandPreviewReport(r evidenceRecord) models.PublicBrandPreviewReport {
	ev := r.publicEvidence()
	title, summary := previewText(r.Title, 240, "Reported issue"), previewText(r.Summary, 700, "")
	if r.PrivacyRestricted {
		title, summary, ev.SourceURL, ev.Excerpt = "Reported issue", "", nil, nil
	}
	return models.PublicBrandPreviewReport{
		PublicID: r.PublicID, CanonicalPath: "/digital/report/" + r.PublicID,
		Title: title, Summary: summary, Language: r.Language, ReportKind: ev.ReportKind,
		SourceURL: ev.SourceURL, SourcePublishedAt: ev.SourcePublishedAt, Excerpt: ev.Excerpt,
		IntakeMethod: ev.IntakeMethod, IntakeChannel: ev.IntakeChannel,
		ObservedAt: ev.ObservedAt, IngestedAt: ev.IngestedAt,
	}
}

func (d *Database) GetPublicBrandPreview(ctx context.Context, filter EvidenceFilter) (models.PublicBrandPreview, error) {
	f, err := NormalizeBrandPreviewFilter(filter)
	if err != nil {
		return models.PublicBrandPreview{}, err
	}
	result := models.PublicBrandPreview{BrandName: f.BrandName, BrandDisplay: f.BrandName, Classification: "digital", EvidenceKind: f.Kind, Reports: []models.PublicBrandPreviewReport{}}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	ids, err := loadBrandPreviewIDs(ctx, tx, f, f.Limit)
	if err != nil {
		return result, err
	}
	if len(ids) == 0 {
		if f.Kind != "all" {
			presence := f
			presence.Kind = "all"
			ids, err = loadBrandPreviewIDs(ctx, tx, presence, 1)
			if err != nil {
				return result, err
			}
		}
		result.BrandFound = len(ids) > 0
		return result, tx.Commit()
	}
	records, sources, visible, err := loadBrandPreviewMetadata(ctx, tx, ids)
	if err != nil {
		return result, err
	}
	if len(visible) == 0 {
		return result, tx.Commit()
	}
	if err := loadEvidenceIndexSources(ctx, tx, visible, sources); err != nil {
		return result, err
	}
	if err := loadEvidenceWireSources(ctx, tx, visible, sources); err != nil {
		return result, err
	}
	inputs := make([]evidenceSourceNormalizationRow, 0, len(visible))
	for _, seq := range visible {
		inputs = append(inputs, sources[seq])
	}
	normalized, err := normalizeEvidenceSources(ctx, tx, inputs)
	if err != nil {
		return result, err
	}
	analysis, err := loadBrandPreviewAnalysis(ctx, tx, f, visible)
	if err != nil {
		return result, err
	}
	for _, seq := range ids {
		base, exists := records[seq]
		if !exists {
			continue
		}
		display, exists := analysis[seq]
		if !exists {
			return result, fmt.Errorf("preview analysis is incomplete")
		}
		r := normalized[seq]
		r.PublicID, r.Kind, r.Version, r.Excerpt = base.PublicID, base.Kind, base.Version, base.Excerpt
		r.Title, r.Summary, r.Language = display.Title, display.Summary, display.Language
		result.Reports = append(result.Reports, publicBrandPreviewReport(r))
		if len(result.Reports) == 1 {
			result.BrandDisplay = previewText(display.BrandDisplay, 120, f.BrandName)
		}
	}
	result.BrandFound = len(result.Reports) > 0
	return result, tx.Commit()
}
