package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"cleanapp-common/wiremeta"
	"report-listener/models"
)

// Stream chosen source scalars once. The existing normalizer handles inference,
// using its proven native archive path or bounded SQL fallback in the same tx.
func hybridEvidenceInputQuery(f EvidenceFilter) (string, []interface{}, error) {
	query, args, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		return "", nil, err
	}
	const before = "), candidates AS ("
	const from = "FROM source_winners r"
	const after = "), provenance AS ("
	if strings.Count(query, before) != 1 || strings.Count(query, from) != 1 || strings.Count(query, after) != 1 {
		return "", nil, fmt.Errorf("hybrid evidence input boundary changed")
	}
	begin, joinStart, end := strings.Index(query, before), strings.Index(query, from), strings.Index(query, after)
	if begin >= joinStart || joinStart >= end {
		return "", nil, fmt.Errorf("hybrid evidence input scope changed")
	}
	prefix, joins := query[:begin+1], query[joinStart:end]
	scopeArgs := 1 + len(f.Brands)
	extraArgs := 3 // the full reader's overview clock, page limit and offset
	if f.Kind != "all" {
		extraArgs++
	}
	if len(args) != scopeArgs+extraArgs {
		return "", nil, fmt.Errorf("hybrid evidence scope parameters changed")
	}
	choose := func(header, original, present string) string {
		return "CASE WHEN " + present + " IS NOT NULL THEN " + header + " ELSE " + original + " END"
	}
	date := func(value string) string { return "DATE_FORMAT(" + value + ",'%Y-%m-%d %H:%i:%s.%f')" }
	eii := func(field string) string { return choose("eii_header."+field, "eii_fallback."+field, "eii_header.seq") }
	wire := func(field string) string {
		return choose("wire_header."+field, "wire_fallback."+field, "wire_header.submission_id")
	}
	columns := []string{"r.seq", "r.public_id", "COALESCE(r.latitude,0)", "COALESCE(r.longitude,0)", "r.classification", "r.severity_level", "r.language", "r.brand_name", date("r.created_at"),
		"CASE WHEN COALESCE(ev.evidence_version,0)>0 AND COALESCE(ev.report_kind,'unknown') IN ('firsthand','coverage') THEN COALESCE(ev.report_kind,'unknown') ELSE 'unknown' END",
		"CASE WHEN COALESCE(ev.evidence_excerpt<>'',FALSE) THEN '1' ELSE '' END", "CASE WHEN COALESCE(ev.reported_impact<>'',FALSE) THEN '1' ELSE '' END", "COALESCE(ev.entity_relationship,'unknown')", "COALESCE(ev.evidence_version,0)", "r.has_original_media",
		"COALESCE(r.id,'')", date("r.ts"), "rr.source_type", "rr.source_id", "COALESCE(rr.fetcher_id,'')", "r.source", "r.external_id", eii("source_url"), date(eii("source_timestamp")), date(eii("created_at")),
		"r.wire_submission_id", "(wire_header.submission_id IS NOT NULL OR wire_fallback.submission_id IS NOT NULL)", date(wire("observed_at")), date(wire("submitted_at")), "COALESCE(" + wire("actor_kind") + ",'')", "COALESCE(" + wire("channel") + ",'')"}
	originalNodes := wiremeta.NodeExpressions("wire_fallback")
	for i, name := range wiremeta.NodeColumns() {
		columns = append(columns, "CAST("+choose("wire_header."+name, originalNodes[i], "wire_header.submission_id")+" AS JSON)")
	}
	query = prefix + fmt.Sprintf("\n SELECT /*+ MAX_EXECUTION_TIME(%d) SET_VAR(tmp_table_size=%d) */ ", publicDiscoveryQueryTimeout.Milliseconds(), narrowEvidenceTempTableBudgetBytes) + strings.Join(columns, ",") + " " + joins
	return query, args[:scopeArgs], nil
}

// Other selected analysis fields already live in the single record slice.
// Keep only nullable selection metadata beside it, not a full-record value map.
type hybridEvidenceAnalysisSelection struct {
	created      sql.NullString
	languageNull bool
}

func loadHybridEvidenceInputs(ctx context.Context, tx *sql.Tx, f EvidenceFilter) ([]evidenceRecord, []hybridEvidenceAnalysisSelection, map[int]int, []evidenceSourceNormalizationRow, error) {
	query, args, err := hybridEvidenceInputQuery(f)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("read hybrid evidence inputs: %w", err)
	}
	defer rows.Close()
	all := []evidenceRecord{}
	selections := []hybridEvidenceAnalysisSelection{}
	bySeq := map[int]int{}
	inputs := []evidenceSourceNormalizationRow{}
	nodeCount := len(wiremeta.NodeColumns())
	for rows.Next() {
		var r evidenceRecord
		var selected hybridEvidenceAnalysisSelection
		var severity sql.NullFloat64
		var language, reportTS, rawType, rawID, platform, externalID, url, published, created, wireID, observed, submitted sql.NullString
		var hasMedia sql.NullBool
		var s evidenceSourceNormalizationRow
		var wireLoaded bool
		nodes := make([][]byte, nodeCount)
		dest := []interface{}{&r.Seq, &r.PublicID, &r.Latitude, &r.Longitude, &r.Classification, &severity, &language, &r.Brand, &selected.created,
			&r.Kind, &r.Excerpt, &r.Impact, &r.Relationship, &r.Version, &hasMedia,
			&s.ReportID, &reportTS, &rawType, &rawID, &s.FetcherID, &platform, &externalID, &url, &published, &created,
			&wireID, &wireLoaded, &observed, &submitted, &s.WireActor, &s.WireChannel}
		for i := range nodes {
			dest = append(dest, &nodes[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, nil, nil, nil, err
		}
		if _, duplicate := bySeq[r.Seq]; duplicate {
			return nil, nil, nil, nil, fmt.Errorf("hybrid evidence has duplicate input sequence")
		}
		if wireID.Valid != wireLoaded {
			return nil, nil, nil, nil, fmt.Errorf("hybrid evidence selected Wire metadata is incomplete")
		}
		r.Severity, r.SeverityNull = severity.Float64, !severity.Valid
		r.Language, selected.languageNull = language.String, !language.Valid
		r.HasOriginalMedia = hasMedia.Valid && hasMedia.Bool
		s.Seq = r.Seq
		s.ReportTS, s.RawSourceType, s.RawSourceID = nullableEvidenceString(reportTS), nullableEvidenceString(rawType), nullableEvidenceString(rawID)
		s.EiiSource, s.EiiID, s.EiiURL = nullableEvidenceString(platform), nullableEvidenceString(externalID), nullableEvidenceString(url)
		s.EiiPublished, s.EiiCreated = nullableEvidenceString(published), nullableEvidenceString(created)
		if wireID.Valid {
			s.WireObserved, s.WireSubmitted = nullableEvidenceString(observed), nullableEvidenceString(submitted)
			if err := assembleEvidenceWireNodes(&s, nodes); err != nil {
				return nil, nil, nil, nil, err
			}
		}
		bySeq[r.Seq] = len(all)
		all = append(all, r)
		selections = append(selections, selected)
		inputs = append(inputs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return all, selections, bySeq, inputs, nil
}

func normalizeHybridEvidenceInputs(ctx context.Context, tx *sql.Tx, all []evidenceRecord, inputs []evidenceSourceNormalizationRow) error {
	normalized, err := normalizeEvidenceSources(ctx, tx, inputs)
	if err != nil {
		return err
	}
	if len(normalized) != len(all) {
		return fmt.Errorf("hybrid evidence source normalization is incomplete")
	}
	for i := range all {
		s, ok := normalized[all[i].Seq]
		if !ok || s.Seq != all[i].Seq {
			return fmt.Errorf("hybrid evidence source identity differs")
		}
		r := &all[i]
		r.SourcePlatform, r.SourceURL, r.SourceKey = s.SourcePlatform, s.SourceURL, s.SourceKey
		r.PublishedAt, r.ObservedAt, r.IngestedAt = s.PublishedAt, s.ObservedAt, s.IngestedAt
		r.IngestionTimeMissing, r.IngestedAtNull = s.IngestionTimeMissing, s.IngestedAtNull
		r.ActorKind, r.FetcherID, r.PrivacyRestricted = s.ActorKind, s.FetcherID, s.PrivacyRestricted
		r.ExternalSource, r.IntakeChannel, r.GenerationMethod, r.SourceAccountKey = s.ExternalSource, s.IntakeChannel, s.GenerationMethod, s.SourceAccountKey
	}
	return nil
}

func loadEvidenceBrandPageHybrid(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (EvidenceBrandPage, error) {
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}, Overview: evidenceOverview(nil, f.asOf)}
	all, selections, bySeq, inputs, err := loadHybridEvidenceInputs(ctx, tx, f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return EvidenceBrandPage{}, err
	}
	if len(all) == 0 {
		return page, nil
	}
	if err := normalizeHybridEvidenceInputs(ctx, tx, all, inputs); err != nil {
		return EvidenceBrandPage{}, err
	}
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
			return fmt.Errorf("hybrid evidence selected analysis is missing")
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

// Explicit candidate until dual-MySQL parity and unchanged deadline proof pass.
// There is no route switch or retry through another reader on failure.
func (d *Database) getEvidenceBrandPageHybrid(ctx context.Context, f EvidenceFilter) (EvidenceBrandPage, error) {
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
	page, err := loadEvidenceBrandPageHybrid(queryCtx, tx, f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return EvidenceBrandPage{}, err
	}
	return page, nil
}
