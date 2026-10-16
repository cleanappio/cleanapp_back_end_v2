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

// Compact analysis inputs, live publication/evidence, and revision-checked
// source values participate in the full-cohort read. Native SQL comparison keys
// feed the existing Go analysis comparator without a window/materialization.
// Cache misses remain exact source reads in this same snapshot.
func projectedEvidenceBaseQuery(f EvidenceFilter) (string, []interface{}) {
	marks := make([]string, len(f.Brands))
	args := []interface{}{f.Language}
	for i, brand := range f.Brands {
		marks[i] = "?"
		args = append(args, brand)
	}
	query := `WITH preferred AS (
 SELECT ra.seq,ra.language,ra.created_at,ra.brand_name,ra.classification,CAST(ra.severity_level AS DOUBLE) AS severity_level,
 (ra.language=?) AS analysis_requested,(ra.language='en') AS analysis_english,
 WEIGHT_STRING(ra.language AS CHAR(2)) AS analysis_language_weight
 FROM report_analysis ra FORCE INDEX (` + evidenceBrandCoreIndex + `)
 WHERE ra.brand_name IN (` + strings.Join(marks, ",") + `) AND ra.classification='digital' AND ra.is_valid=TRUE
 ) SELECT /*+ MAX_EXECUTION_TIME(8000) SET_VAR(tmp_table_size=67108864) */ ra.seq,
 ra.classification,ra.severity_level,ra.language,ra.brand_name,DATE_FORMAT(ra.created_at,'%Y-%m-%d %H:%i:%s.%f'),
 CASE WHEN COALESCE(ev.evidence_version,0)>0 AND COALESCE(ev.report_kind,'unknown') IN ('firsthand','coverage') THEN COALESCE(ev.report_kind,'unknown') ELSE 'unknown' END,
 CASE WHEN COALESCE(ev.evidence_excerpt<>'',FALSE) THEN '1' ELSE '' END,
 CASE WHEN COALESCE(ev.reported_impact<>'',FALSE) THEN '1' ELSE '' END,
 COALESCE(ev.entity_relationship,'unknown'),COALESCE(ev.evidence_version,0),
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.has_original_media ELSE LENGTH(fallback.image)>0 END,FALSE),
 COALESCE(` + evidencesourceprojection.CleanPredicate("p") + `,FALSE),
 p.source_timestamp,p.observed_at,p.ts,COALESCE(p.ingestion_time_missing,FALSE),
 COALESCE(p.privacy_restricted,FALSE),COALESCE(p.source_key,''),
 COALESCE(p.source_account_key,''),COALESCE(p.ingested_at_null,FALSE),
 ra.analysis_requested,ra.analysis_english,ra.analysis_language_weight,ra.created_at
 FROM preferred ra
 LEFT JOIN report_metadata_projection m ON m.seq=ra.seq
 LEFT JOIN reports fallback ON fallback.seq=CASE WHEN m.seq IS NULL THEN ra.seq ELSE NULL END
 LEFT JOIN report_raw rr ON rr.report_seq=ra.seq
 LEFT JOIN report_evidence ev ON ev.seq=ra.seq
 LEFT JOIN ` + evidencesourceprojection.Table + ` p ON p.seq=ra.seq
 WHERE ra.seq IS NOT NULL AND (CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE fallback.public_id END COLLATE utf8mb4_bin)<>'' AND ` + PublicVisibilityWhereSQL + `
 AND NOT EXISTS(SELECT 1 FROM report_status rs WHERE rs.seq=ra.seq AND rs.status IS NOT NULL AND rs.status<>'active')
 AND (SELECT /*+ JOIN_INDEX(ro idx_is_public) */ COUNT(*) FROM reports_owners ro WHERE ro.seq=ra.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)=0`
	return query, args
}

type evidenceAnalysisSelection struct {
	created            sql.NullString
	languageNull       bool
	requested, english sql.NullInt64
	languageWeight     []byte
	createdTime        sql.NullTime
	sourceClean        bool
}

func (a evidenceAnalysisSelection) preference() evidenceAnalysisCore {
	return evidenceAnalysisCore{Created: a.createdTime, Requested: a.requested, English: a.english, LanguageWeight: a.languageWeight}
}

func sameProjectedReport(a, b evidenceRecord) bool {
	sameDate := func(a, b *time.Time) bool {
		return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
	}
	// Only the analysis tuple can differ across this stream's rows for one
	// report. Output-only fields are deferred and remain empty on both sides.
	return a.Seq == b.Seq && a.Kind == b.Kind && a.Excerpt == b.Excerpt && a.Impact == b.Impact && a.Relationship == b.Relationship && a.Version == b.Version && a.HasOriginalMedia == b.HasOriginalMedia &&
		sameDate(a.PublishedAt, b.PublishedAt) && sameDate(a.ObservedAt, b.ObservedAt) && a.IngestedAt.Equal(b.IngestedAt) &&
		a.IngestionTimeMissing == b.IngestionTimeMissing && a.IngestedAtNull == b.IngestedAtNull &&
		a.PrivacyRestricted == b.PrivacyRestricted && a.SourceKey == b.SourceKey && a.SourceAccountKey == b.SourceAccountKey
}

// Keep the full cohort without repeatedly copying its wide records as a single
// slice grows. Flatten only after the stream has completed successfully.
type projectedEvidenceAccumulator struct {
	chunks [][]evidenceRecord
	length int
}

const projectedEvidenceChunkSize = 1024

func (a *projectedEvidenceAccumulator) add(record evidenceRecord) {
	if len(a.chunks) == 0 || len(a.chunks[len(a.chunks)-1]) == projectedEvidenceChunkSize {
		a.chunks = append(a.chunks, make([]evidenceRecord, 0, projectedEvidenceChunkSize))
	}
	i := len(a.chunks) - 1
	a.chunks[i] = append(a.chunks[i], record)
	a.length++
}

func (a *projectedEvidenceAccumulator) record(index int) *evidenceRecord {
	return &a.chunks[index/projectedEvidenceChunkSize][index%projectedEvidenceChunkSize]
}

func (a *projectedEvidenceAccumulator) records() []evidenceRecord {
	if len(a.chunks) == 1 {
		return a.chunks[0]
	}
	records := make([]evidenceRecord, 0, a.length)
	for _, chunk := range a.chunks {
		records = append(records, chunk...)
	}
	return records
}

// Native analysis selection carries owned comparison bytes and nullable dates.
// Retain it in fixed chunks too, so slice growth does not recopy the wider tuple.
type projectedEvidenceSelectionAccumulator struct {
	chunks [][]evidenceAnalysisSelection
	length int
}

func (a *projectedEvidenceSelectionAccumulator) add(selection evidenceAnalysisSelection) {
	if len(a.chunks) == 0 || len(a.chunks[len(a.chunks)-1]) == projectedEvidenceChunkSize {
		a.chunks = append(a.chunks, make([]evidenceAnalysisSelection, 0, projectedEvidenceChunkSize))
	}
	i := len(a.chunks) - 1
	a.chunks[i] = append(a.chunks[i], selection)
	a.length++
}

func (a *projectedEvidenceSelectionAccumulator) selection(index int) *evidenceAnalysisSelection {
	return &a.chunks[index/projectedEvidenceChunkSize][index%projectedEvidenceChunkSize]
}

func (a *projectedEvidenceSelectionAccumulator) selections() []evidenceAnalysisSelection {
	if len(a.chunks) == 1 {
		return a.chunks[0]
	}
	selections := make([]evidenceAnalysisSelection, 0, a.length)
	for _, chunk := range a.chunks {
		selections = append(selections, chunk...)
	}
	return selections
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
	if strings.HasPrefix(literal, "0000-00-00 00:00:00") && (len(literal) == 19 || strings.Trim(literal[20:], "0") == "") {
		zero := time.Time{}
		return &zero, true
	}
	// The lexical check already restricts separators and fraction precision;
	// parse once to validate calendar values without allocating a format copy.
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05.999999", literal, time.UTC)
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
	var all projectedEvidenceAccumulator
	var selections projectedEvidenceSelectionAccumulator
	bySeq := map[int]int{}
	missing := []int{}
	var r evidenceRecord
	var selected evidenceAnalysisSelection
	var severity sql.NullFloat64
	var language, published, observed, ingested sql.NullString
	var clean bool
	dest := []interface{}{&r.Seq, &r.Classification, &severity, &language, &r.Brand, &selected.created,
		&r.Kind, &r.Excerpt, &r.Impact, &r.Relationship, &r.Version, &r.HasOriginalMedia, &clean,
		&published, &observed, &ingested, &r.IngestionTimeMissing,
		&r.PrivacyRestricted, &r.SourceKey, &r.SourceAccountKey, &r.IngestedAtNull,
		&selected.requested, &selected.english, &selected.languageWeight, &selected.createdTime}
	for rows.Next() {
		r, selected = evidenceRecord{}, evidenceAnalysisSelection{}
		if err := rows.Scan(dest...); err != nil {
			return nil, nil, nil, nil, err
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
		selected.sourceClean = clean
		if prior, duplicate := bySeq[r.Seq]; duplicate {
			previous := all.record(prior)
			previousSelection := selections.selection(prior)
			if clean != previousSelection.sourceClean || !sameProjectedReport(r, *previous) {
				return nil, nil, nil, nil, fmt.Errorf("projected evidence has inconsistent report tuple")
			}
			if preferredEvidenceAnalysis(selected.preference(), previousSelection.preference()) {
				*previous, *previousSelection = r, selected
			}
			continue
		}
		if !clean {
			missing = append(missing, r.Seq)
		}
		bySeq[r.Seq] = all.length
		all.add(r)
		selections.add(selected)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return all.records(), selections.selections(), bySeq, missing, nil
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
	// Only the bounded output rows need these source/display fields. A dirty
	// fallback already supplied its exact source tuple, so never replace it with
	// values from the stale projection during late hydration.
	freshSources := make(map[int]bool, len(missing))
	for _, seq := range missing {
		freshSources[seq] = true
	}
	return finishProjectedEvidencePage(ctx, tx, f, page, all, selections, bySeq, freshSources)
}

func finishProjectedEvidencePage(ctx context.Context, tx *sql.Tx, f EvidenceFilter, page EvidenceBrandPage, all []evidenceRecord, selections []evidenceAnalysisSelection, bySeq map[int]int, freshSources map[int]bool) (EvidenceBrandPage, error) {
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
	if err := hydrateProjectedEvidenceOutput(ctx, tx, &grouped, freshSources); err != nil {
		return EvidenceBrandPage{}, err
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

// The full cohort carries only fields needed for exact grouping, counts and
// analysis selection. Hydrate output-only fields for the page and its first
// eight supporting members in the same repeatable-read snapshot. No query is
// repeated over the full cohort, and no cached value replaces fresh fallback.
func hydrateProjectedEvidenceOutput(ctx context.Context, tx *sql.Tx, grouped *evidenceGroupedPage, freshSources map[int]bool) error {
	ids := []int{}
	seen := map[int]bool{}
	add := func(seq int) {
		if !seen[seq] {
			seen[seq] = true
			ids = append(ids, seq)
		}
	}
	for _, r := range grouped.Records {
		add(r.Seq)
		for _, sample := range grouped.SupportingSamples[r.Seq] {
			add(sample.Seq)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	query := `SELECT ids.seq,
 CASE WHEN m.seq IS NOT NULL THEN m.public_id ELSE fallback.public_id END,
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.latitude ELSE fallback.latitude END,0),
 COALESCE(CASE WHEN m.seq IS NOT NULL THEN m.longitude ELSE fallback.longitude END,0),
 COALESCE(` + evidencesourceprojection.CleanPredicate("p") + `,FALSE),
 COALESCE(p.source_platform,''),COALESCE(p.source_url,''),COALESCE(p.actor_kind,''),COALESCE(p.fetcher_id,''),
 COALESCE(p.external_source,FALSE),COALESCE(p.intake_channel,''),COALESCE(p.generation_method,'')
 FROM ` + evidenceIDsTable + `
 LEFT JOIN report_metadata_projection m ON m.seq=ids.seq
 LEFT JOIN reports fallback ON fallback.seq=CASE WHEN m.seq IS NULL THEN ids.seq ELSE NULL END
 LEFT JOIN ` + evidencesourceprojection.Table + ` p ON p.seq=ids.seq`
	rows, err := tx.QueryContext(ctx, query, evidenceIDsJSON(ids))
	if err != nil {
		return fmt.Errorf("hydrate projected evidence output: %w", err)
	}
	defer rows.Close()
	output := make(map[int]evidenceRecord, len(ids))
	for rows.Next() {
		var r evidenceRecord
		var publicID sql.NullString
		var clean bool
		if err := rows.Scan(&r.Seq, &publicID, &r.Latitude, &r.Longitude, &clean, &r.SourcePlatform, &r.SourceURL, &r.ActorKind, &r.FetcherID, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod); err != nil {
			return err
		}
		if !seen[r.Seq] || !publicID.Valid || publicID.String == "" || (!clean && !freshSources[r.Seq]) {
			return fmt.Errorf("projected evidence output identity or generation is incomplete")
		}
		if _, duplicate := output[r.Seq]; duplicate {
			return fmt.Errorf("projected evidence output has duplicate report")
		}
		r.PublicID = publicID.String
		output[r.Seq] = r
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(output) != len(ids) {
		return fmt.Errorf("projected evidence output is incomplete")
	}
	apply := func(r *evidenceRecord) {
		d := output[r.Seq]
		r.PublicID, r.Latitude, r.Longitude = d.PublicID, d.Latitude, d.Longitude
		if !freshSources[r.Seq] {
			r.SourcePlatform, r.SourceURL = d.SourcePlatform, d.SourceURL
			r.ActorKind, r.FetcherID = d.ActorKind, d.FetcherID
			r.ExternalSource, r.IntakeChannel, r.GenerationMethod = d.ExternalSource, d.IntakeChannel, d.GenerationMethod
		}
	}
	for i := range grouped.Records {
		r := &grouped.Records[i]
		apply(r)
		samples := grouped.SupportingSamples[r.Seq]
		for j := range samples {
			apply(&samples[j])
		}
		grouped.SupportingSamples[r.Seq] = samples
	}
	return nil
}

// Public brand pages use one read-only snapshot and the unchanged eight-second
// deadline for live publication, exact source fallback, grouping and hydration.
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
