package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"report-listener/models"
)

// One authoritative normalized row per visible report, with no SQL source or
// family aggregation. Grouping and overview reuse the established Go adapter.
func hybridEvidenceNormalizedQuery(f EvidenceFilter) (string, []interface{}, error) {
	query, args, err := narrowEvidenceBrandPageQuery(f)
	if err != nil {
		return "", nil, err
	}
	const boundary = "), rank_input AS ("
	if strings.Count(query, boundary) != 1 {
		return "", nil, fmt.Errorf("hybrid evidence normalized boundary changed")
	}
	query = query[:strings.Index(query, boundary)+1]
	// The fused stream has one normalization consumer. The narrow SQL reader
	// needs this materialization for several consumers; here it only postpones
	// the first row and copies the complete selected metadata scope needlessly.
	const sharedScope = "SELECT /*+ NO_MERGE(r) */ r.seq, r.public_id,"
	if strings.Count(query, sharedScope) != 1 {
		return "", nil, fmt.Errorf("hybrid evidence source scope changed")
	}
	query = strings.Replace(query, sharedScope, "SELECT r.seq, r.public_id,", 1)
	scopeArgs := 1 + len(f.Brands)
	extraArgs := 3 // the full reader's overview clock, page limit and offset
	if f.Kind != "all" {
		extraArgs++
	}
	if len(args) != scopeArgs+extraArgs {
		return "", nil, fmt.Errorf("hybrid evidence scope parameters changed")
	}
	query += fmt.Sprintf(`
 SELECT /*+ MAX_EXECUTION_TIME(%d) SET_VAR(tmp_table_size=%d) */
 seq,public_id,ts,COALESCE(latitude,0),COALESCE(longitude,0),classification,severity_level,language,brand_name,
 report_kind,evidence_excerpt,reported_impact,entity_relationship,evidence_version,
 source_platform,source_url,source_timestamp,observed_at,actor_kind,fetcher_id,privacy_restricted,source_key,
 has_original_media,external_source,intake_channel,generation_method,source_account_key,ingestion_time_missing,
 DATE_FORMAT(analysis_created_at,'%%Y-%%m-%%d %%H:%%i:%%s.%%f') FROM normalized`, publicDiscoveryQueryTimeout.Milliseconds(), narrowEvidenceTempTableBudgetBytes)
	return query, args[:scopeArgs], nil
}

// Other selected analysis fields already live in the single record slice.
// Keep only nullable selection metadata beside it, not a full-record value map.
type hybridEvidenceAnalysisSelection struct {
	created      sql.NullString
	languageNull bool
}

func loadHybridEvidenceNormalized(ctx context.Context, tx *sql.Tx, f EvidenceFilter) ([]evidenceRecord, []hybridEvidenceAnalysisSelection, map[int]int, error) {
	query, args, err := hybridEvidenceNormalizedQuery(f)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read hybrid normalized evidence: %w", err)
	}
	defer rows.Close()
	all := []evidenceRecord{}
	selections := []hybridEvidenceAnalysisSelection{}
	bySeq := map[int]int{}
	for rows.Next() {
		var r evidenceRecord
		var selected hybridEvidenceAnalysisSelection
		var added sql.NullTime
		var severity sql.NullFloat64
		var language, account sql.NullString
		var hasMedia sql.NullBool
		if err := rows.Scan(&r.Seq, &r.PublicID, &added, &r.Latitude, &r.Longitude, &r.Classification, &severity, &language, &r.Brand,
			&r.Kind, &r.Excerpt, &r.Impact, &r.Relationship, &r.Version,
			&r.SourcePlatform, &r.SourceURL, &r.PublishedAt, &r.ObservedAt, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey,
			&hasMedia, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &account, &r.IngestionTimeMissing, &selected.created); err != nil {
			return nil, nil, nil, err
		}
		if _, duplicate := bySeq[r.Seq]; duplicate {
			return nil, nil, nil, fmt.Errorf("hybrid evidence has duplicate normalized sequence")
		}
		r.IngestedAt, r.IngestedAtNull = added.Time, !added.Valid
		r.Severity, r.SeverityNull = severity.Float64, !severity.Valid
		r.Language, selected.languageNull = language.String, !language.Valid
		r.HasOriginalMedia = hasMedia.Valid && hasMedia.Bool
		r.SourceAccountKey = account.String
		bySeq[r.Seq] = len(all)
		all = append(all, r)
		selections = append(selections, selected)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	return all, selections, bySeq, nil
}

func loadEvidenceBrandPageHybrid(ctx context.Context, tx *sql.Tx, f EvidenceFilter) (EvidenceBrandPage, error) {
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}, Overview: evidenceOverview(nil, f.asOf)}
	all, selections, bySeq, err := loadHybridEvidenceNormalized(ctx, tx, f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return EvidenceBrandPage{}, err
	}
	if len(all) == 0 {
		return page, nil
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
