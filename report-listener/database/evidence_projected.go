package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"cleanapp-common/evidencesourceprojection"
	"report-listener/models"
)

// Only preferred analysis, live publication/evidence, and revision-checked
// normalized source values participate in the full-cohort read. Cache misses
// never mean missing reports: they are normalized separately in this snapshot.
func projectedEvidenceBaseQuery(f EvidenceFilter) (string, []interface{}) {
	marks := make([]string, len(f.Brands))
	args := []interface{}{f.Language}
	for i, brand := range f.Brands {
		marks[i] = "?"
		args = append(args, brand)
	}
	query := `WITH preferred AS (
 SELECT ra.seq,ra.language,ra.created_at,ra.brand_name,ra.classification,CAST(ra.severity_level AS DOUBLE) AS severity_level,
 ROW_NUMBER() OVER (PARTITION BY ra.seq ORDER BY (ra.language=?) DESC,(ra.language='en') DESC,ra.language ASC,ra.created_at DESC) AS analysis_rank
 FROM report_analysis ra FORCE INDEX (` + evidenceBrandCoreIndex + `)
 WHERE ra.brand_name IN (` + strings.Join(marks, ",") + `) AND ra.classification='digital' AND ra.is_valid=TRUE
 ) SELECT /*+ MAX_EXECUTION_TIME(8000) SET_VAR(tmp_table_size=67108864) */ ra.seq,
 CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE fallback.public_id END COLLATE utf8mb4_bin AS public_id,
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.latitude ELSE fallback.latitude END,0),
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.longitude ELSE fallback.longitude END,0),
 ra.classification,ra.severity_level,ra.language,ra.brand_name,DATE_FORMAT(ra.created_at,'%Y-%m-%d %H:%i:%s.%f'),
 CASE WHEN COALESCE(ev.evidence_version,0)>0 AND COALESCE(ev.report_kind,'unknown') IN ('firsthand','coverage') THEN COALESCE(ev.report_kind,'unknown') ELSE 'unknown' END,
 CASE WHEN COALESCE(ev.evidence_excerpt<>'',FALSE) THEN '1' ELSE '' END,
 CASE WHEN COALESCE(ev.reported_impact<>'',FALSE) THEN '1' ELSE '' END,
 COALESCE(ev.entity_relationship,'unknown'),COALESCE(ev.evidence_version,0),
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.has_original_media ELSE LENGTH(fallback.image)>0 END,FALSE),
 COALESCE(` + evidencesourceprojection.CleanPredicate("p") + `,FALSE),
 COALESCE(p.source_platform,''),COALESCE(p.source_url,''),p.source_timestamp,p.observed_at,p.ts,
 COALESCE(p.ingestion_time_missing,FALSE),COALESCE(p.actor_kind,''),COALESCE(p.fetcher_id,''),
 COALESCE(p.privacy_restricted,FALSE),COALESCE(p.source_key,''),COALESCE(p.external_source,FALSE),
 COALESCE(p.intake_channel,''),COALESCE(p.generation_method,''),COALESCE(p.source_account_key,''),COALESCE(p.ingested_at_null,FALSE)
 FROM preferred ra
 LEFT JOIN report_metadata_projection m ON m.seq=ra.seq
 LEFT JOIN reports fallback ON fallback.seq=CASE WHEN m.seq IS NULL THEN ra.seq ELSE NULL END
 LEFT JOIN report_raw rr ON rr.report_seq=ra.seq
 LEFT JOIN report_evidence ev ON ev.seq=ra.seq
 LEFT JOIN ` + evidencesourceprojection.Table + ` p ON p.seq=ra.seq
 WHERE ra.analysis_rank=1 AND (CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE fallback.public_id END COLLATE utf8mb4_bin)<>'' AND ` + PublicVisibilityWhereSQL + `
 AND (SELECT COUNT(*) FROM report_status rs WHERE rs.seq=ra.seq AND rs.status IS NOT NULL AND rs.status<>'active')=0
 AND (SELECT COUNT(*) FROM reports_owners ro WHERE ro.seq=ra.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)=0`
	return query, args
}

type evidenceAnalysisSelection struct {
	created      sql.NullString
	languageNull bool
}

func sourceProjectionDate(value sql.NullString) (*time.Time, bool) {
	if !value.Valid {
		return nil, true
	}
	// Match the writer's SQL-literal grammar before Go's broader date parser,
	// which also accepts commas and arbitrary fractional-second precision.
	literal := value.String
	if len(literal) < 19 || len(literal) > 26 || len(literal) == 20 {
		return nil, false
	}
	for i, b := range []byte(literal) {
		separator := byte(0)
		switch i {
		case 4, 7:
			separator = '-'
		case 10:
			separator = ' '
		case 13, 16:
			separator = ':'
		case 19:
			separator = '.'
		}
		if separator != 0 {
			if b != separator {
				return nil, false
			}
		} else if b < '0' || b > '9' {
			return nil, false
		}
	}
	if parsed, ok := archiveSourceDate(&value.String); ok {
		return parsed, true
	}
	if strings.HasPrefix(value.String, "0000-00-00 00:00:00.") {
		fraction := strings.TrimPrefix(value.String, "0000-00-00 00:00:00.")
		if len(fraction) >= 1 && len(fraction) <= 6 && strings.Trim(fraction, "0") == "" {
			zero := time.Time{}
			return &zero, true
		}
	}
	// The writer accepts SQL date literals with zero to six fraction digits.
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05.999999", value.String, time.UTC)
	if err != nil || parsed.Year() < 1000 {
		return nil, false
	}
	return &parsed, true
}

func loadProjectedEvidenceBase(ctx context.Context, tx *sql.Tx, f EvidenceFilter) ([]evidenceRecord, []evidenceAnalysisSelection, map[int]int, []int, error) {
	query, args := projectedEvidenceBaseQuery(f)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read projected evidence base: %w", err)
	}
	defer rows.Close()
	all := make([]evidenceRecord, 0, 1024)
	selections := make([]evidenceAnalysisSelection, 0, 1024)
	bySeq := map[int]int{}
	missing := []int{}
	var r evidenceRecord
	var selected evidenceAnalysisSelection
	var severity sql.NullFloat64
	var language, published, observed, ingested sql.NullString
	var clean bool
	dest := []interface{}{&r.Seq, &r.PublicID, &r.Latitude, &r.Longitude, &r.Classification, &severity, &language, &r.Brand, &selected.created,
		&r.Kind, &r.Excerpt, &r.Impact, &r.Relationship, &r.Version, &r.HasOriginalMedia, &clean,
		&r.SourcePlatform, &r.SourceURL, &published, &observed, &ingested, &r.IngestionTimeMissing,
		&r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.IngestedAtNull}
	for rows.Next() {
		r, selected = evidenceRecord{}, evidenceAnalysisSelection{}
		if err := rows.Scan(dest...); err != nil {
			return nil, nil, nil, nil, err
		}
		if _, duplicate := bySeq[r.Seq]; duplicate {
			return nil, nil, nil, nil, fmt.Errorf("projected evidence has duplicate report")
		}
		r.Severity, r.SeverityNull = severity.Float64, !severity.Valid
		r.Language, selected.languageNull = language.String, !language.Valid
		if clean {
			pub, pubOK := sourceProjectionDate(published)
			obs, obsOK := sourceProjectionDate(observed)
			added, addedOK := sourceProjectionDate(ingested)
			clean = pubOK && obsOK && addedOK && r.IngestedAtNull == (added == nil)
			if clean {
				r.PublishedAt, r.ObservedAt = pub, obs
				if added != nil {
					r.IngestedAt = *added
				}
			}
		}
		if !clean {
			missing = append(missing, r.Seq)
		}
		bySeq[r.Seq] = len(all)
		all, selections = append(all, r), append(selections, selected)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return all, selections, bySeq, missing, nil
}

func mergeEvidenceSourceRecord(r *evidenceRecord, s evidenceRecord) {
	r.SourcePlatform, r.SourceURL, r.SourceKey = s.SourcePlatform, s.SourceURL, s.SourceKey
	r.PublishedAt, r.ObservedAt, r.IngestedAt = s.PublishedAt, s.ObservedAt, s.IngestedAt
	r.IngestionTimeMissing, r.IngestedAtNull = s.IngestionTimeMissing, s.IngestedAtNull
	r.ActorKind, r.FetcherID, r.PrivacyRestricted = s.ActorKind, s.FetcherID, s.PrivacyRestricted
	r.ExternalSource, r.IntakeChannel, r.GenerationMethod, r.SourceAccountKey = s.ExternalSource, s.IntakeChannel, s.GenerationMethod, s.SourceAccountKey
}

func loadEvidenceBrandPageProjected(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (EvidenceBrandPage, error) {
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}, Overview: evidenceOverview(nil, f.asOf)}
	all, selections, bySeq, missing, err := loadProjectedEvidenceBase(ctx, tx, f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return EvidenceBrandPage{}, err
	}
	if len(all) == 0 {
		return page, nil
	}
	if len(missing) != 0 {
		fresh, err := loadFreshEvidenceSourceRecords(ctx, tx, missing)
		if err != nil {
			return EvidenceBrandPage{}, err
		}
		for _, seq := range missing {
			source, ok := fresh[seq]
			if !ok || source.Seq != seq {
				return EvidenceBrandPage{}, fmt.Errorf("projected evidence fallback is incomplete")
			}
			mergeEvidenceSourceRecord(&all[bySeq[seq]], source)
		}
	}
	return finishProjectedEvidencePage(ctx, tx, f, page, all, selections, bySeq)
}

func finishProjectedEvidencePage(ctx context.Context, tx *sql.Tx, f EvidenceFilter, page EvidenceBrandPage, all []evidenceRecord, selections []evidenceAnalysisSelection, bySeq map[int]int) (EvidenceBrandPage, error) {
	ids := make([]int, len(all))
	for i := range all {
		ids[i] = all[i].Seq
	}
	page.Overview = evidenceOverview(all, f.asOf)
	cases, err := loadEvidenceCaseReferences(ctx, tx, ids)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	grouped := groupEvidenceRecords(all, cases, f)
	if err := ctx.Err(); err != nil {
		return EvidenceBrandPage{}, err
	}
	chosen := make(map[int]evidenceAnalysisCore, len(grouped.Records)*8)
	choose := func(seq int) error {
		i, ok := bySeq[seq]
		if !ok {
			return fmt.Errorf("projected evidence selected analysis is missing")
		}
		r, selected := all[i], selections[i]
		chosen[seq] = evidenceAnalysisCore{Record: evidenceRecord{Seq: seq, Language: r.Language, Brand: r.Brand, Classification: r.Classification, Severity: r.Severity, SeverityNull: r.SeverityNull}, CreatedLiteral: nullableEvidenceString(selected.created), LanguageNull: selected.languageNull}
		return nil
	}
	for _, r := range grouped.Records {
		if err := choose(r.Seq); err != nil {
			return EvidenceBrandPage{}, err
		}
		samples := grouped.SupportingSamples[r.Seq]
		for i := range samples {
			if err := choose(samples[i].Seq); err != nil {
				return EvidenceBrandPage{}, err
			}
			// The SQL oracle's sample JSON formats whole seconds; the page's
			// own source date retains its original timestamp precision.
			if samples[i].PublishedAt != nil {
				published := samples[i].PublishedAt.Truncate(time.Second)
				samples[i].PublishedAt = &published
			}
		}
		grouped.SupportingSamples[r.Seq] = samples
	}
	if err := hydrateEvidenceDisplay(ctx, tx, f, chosen, &grouped); err != nil {
		return EvidenceBrandPage{}, err
	}
	mediaSeqs := []int{}
	for _, r := range grouped.Records {
		if r.HasOriginalMedia && !r.PrivacyRestricted {
			mediaSeqs = append(mediaSeqs, r.Seq)
		}
	}
	valid, err := loadEvidenceImageValidity(ctx, tx, mediaSeqs)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	for _, r := range grouped.Records {
		if r.IngestedAtNull {
			r.IngestedAt = time.Unix(0, 0).UTC()
		}
		ev, rec := r.publicEvidence(), r.publicRecurrence()
		ev.HasOriginalMedia = ev.HasOriginalMedia && valid[r.Seq]
		page.Reports = append(page.Reports, models.ReportWithMinimalAnalysis{Report: models.Report{Seq: r.Seq, PublicID: r.PublicID, Timestamp: r.IngestedAt, Latitude: r.Latitude, Longitude: r.Longitude, SourceTimestamp: ev.SourcePublishedAt, SourceURL: ev.SourceURL}, Analysis: []models.MinimalAnalysis{{Title: r.Title, Summary: r.Summary, Classification: r.Classification, SeverityLevel: r.Severity, Language: r.Language, BrandName: r.Brand, BrandDisplayName: r.BrandDisplay}}, Evidence: &ev, Recurrence: &rec})
	}
	page.Total, page.High, page.Medium = grouped.Total, grouped.High, grouped.Medium
	page.KindCounts, page.HasMore = grouped.KindCounts, grouped.HasMore
	return page, nil
}

// Remains off the public route until actual source-bound deadline proofs pass.
func (d *Database) getEvidenceBrandPageProjected(ctx context.Context, f EvidenceFilter) (EvidenceBrandPage, error) {
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
	page, err := loadEvidenceBrandPageProjected(queryCtx, tx, f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return EvidenceBrandPage{}, err
	}
	return page, nil
}
