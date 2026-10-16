package database

import (
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

// This candidate keeps the executable reference's source, visibility, grouping,
// facet and ordering rules. Only scalar/presence columns enter its all-cohort
// windows. Display bodies are hydrated for the selected page and its <=8 samples.
// It is intentionally not a timeout fallback: a caller must select one reader.
func narrowEvidenceBrandPageQuery(f EvidenceFilter) (string, []interface{}, error) {
	query, args := evidenceBrandPageQuery(f)
	replaceOne := func(old, next string) error {
		if strings.Count(query, old) != 1 {
			return fmt.Errorf("narrow evidence reference contract changed")
		}
		query = strings.Replace(query, old, next, 1)
		return nil
	}
	changes := [][2]string{
		// Bound server work as well as the client context: MySQL can otherwise
		// continue materializing intermediate rows after the client disconnects.
		{"SELECT sc.all_count,", fmt.Sprintf("SELECT /*+ MAX_EXECUTION_TIME(%d) */ sc.all_count,", publicDiscoveryQueryTimeout.Milliseconds())},
		{"ra.seq,ra.language,ra.created_at,ra.brand_name,ra.brand_display_name,ra.classification,ra.title,ra.summary,", "ra.seq,ra.language,ra.created_at,ra.brand_name,'' AS brand_display_name,ra.classification,'' AS title,'' AS summary,"},
		{"COALESCE(ev.evidence_excerpt,'') AS evidence_excerpt", "CASE WHEN COALESCE(ev.evidence_excerpt<>'',FALSE) THEN '1' ELSE '' END AS evidence_excerpt"},
		{"COALESCE(ev.reported_impact,'') AS reported_impact", "CASE WHEN COALESCE(ev.reported_impact<>'',FALSE) THEN '1' ELSE '' END AS reported_impact"},
		{"COALESCE(ev.classification_reason,'') AS classification_reason", "'' AS classification_reason"},
		{"FROM report_analysis ra LEFT JOIN report_evidence ev", "FROM report_analysis ra FORCE INDEX (" + evidenceBrandCoreIndex + ") LEFT JOIN report_evidence ev"},
		{"ra.title, ra.summary, ra.classification,", "ra.created_at AS analysis_created_at,ra.title, ra.summary, ra.classification,"},
		{"ws.submitted_at, (", "ws.submitted_at AS submitted_at, ("},
		{"SELECT normalized.*, ROW_NUMBER() OVER (PARTITION BY source_key", "SELECT " + narrowEvidenceRankColumns + ", ROW_NUMBER() OVER (PARTITION BY source_key"},
		{"LEFT JOIN external_ingest_index eii ON eii.seq = r.seq", `LEFT JOIN external_ingest_index eii_identity FORCE INDEX(idx_seq) ON eii_identity.seq=r.seq
  LEFT JOIN external_ingest_header_projection eii_header ON eii_header.seq=eii_identity.seq AND eii_header.source=eii_identity.source AND eii_header.external_id=eii_identity.external_id
  LEFT JOIN external_ingest_index eii_fallback FORCE INDEX(PRIMARY) ON eii_fallback.source=CASE WHEN eii_header.seq IS NULL THEN eii_identity.source ELSE NULL END AND eii_fallback.external_id=eii_identity.external_id AND eii_fallback.seq=eii_identity.seq`},
		{"LEFT JOIN wire_submissions_raw ws ON ws.report_seq = r.seq", `LEFT JOIN wire_submissions_raw wire_identity ON wire_identity.report_seq=r.seq
  LEFT JOIN wire_evidence_projection wire_header ON wire_header.submission_id=wire_identity.submission_id AND wire_header.report_seq=wire_identity.report_seq
  LEFT JOIN wire_submissions_raw wire_fallback ON wire_fallback.submission_id=CASE WHEN wire_header.submission_id IS NULL THEN wire_identity.submission_id ELSE NULL END AND wire_fallback.report_seq=wire_identity.report_seq`},
		{"JSON_OBJECT('title',title,", "JSON_OBJECT('core'," + narrowEvidenceAnalysisJSON("n") + ",'title',n.title,"},
		{"COALESCE(p.ts IS NULL,FALSE) FROM scope_counts", "COALESCE(p.ts IS NULL,FALSE)," + narrowEvidenceAnalysisJSON("p") + " FROM scope_counts"},
	}
	for _, change := range changes {
		if err := replaceOne(change[0], change[1]); err != nil {
			return "", nil, err
		}
	}
	for _, field := range []string{"source_url", "source_timestamp", "created_at"} {
		query = strings.ReplaceAll(query, "eii."+field, "(CASE WHEN eii_header.seq IS NOT NULL THEN eii_header."+field+" ELSE eii_fallback."+field+" END)")
	}
	query = strings.NewReplacer("eii.external_id", "eii_identity.external_id", "eii.source", "eii_identity.source").Replace(query)
	// JSON column assignment and URI projection expressions have different SQL
	// types. Explicit JSON casts preserve missing SQL NULL, literal JSON null,
	// numeric types, and the non-HTTP URI blocker before the reference uses them.
	paths := [][2]string{
		{"report_json", "$.digital_context.platform"}, {"report_json", "$.digital_context.legacy_source"},
		{"report_json", "$.digital_context.metadata.source_url"}, {"report_json", "$.digital_context.source_url"},
		{"report_json", "$.digital_context.url"}, {"report_json", "$.evidence_bundle[0].uri"},
		{"report_json", "$.digital_context.metadata.source_published_at"}, {"report_json", "$.digital_context.metadata.author_did"},
		{"report_json", "$.digital_context.metadata.author_username"}, {"report_json", "$.digital_context.metadata.author_handle"},
		{"report_json", "$.digital_context.author_handle"}, {"report_json", "$.privacy.contains_pii"},
		{"report_json", "$.privacy.requires_redaction"}, {"provenance_json", "$.generation_method"},
		{"extensions_json", "$.observation_time_supplied"},
	}
	columns, originals := wiremeta.NodeColumns(), wiremeta.NodeExpressions("wire_fallback")
	if len(paths) != len(columns) {
		return "", nil, fmt.Errorf("narrow evidence Wire node contract changed")
	}
	for i, node := range paths {
		old := "JSON_EXTRACT(ws." + node[0] + ",'" + node[1] + "')"
		if !strings.Contains(query, old) {
			return "", nil, fmt.Errorf("narrow evidence Wire reference changed")
		}
		next := "CAST(CASE WHEN wire_header.submission_id IS NOT NULL THEN wire_header." + columns[i] + " ELSE " + originals[i] + " END AS JSON)"
		query = strings.ReplaceAll(query, old, next)
	}
	for _, field := range []string{"observed_at", "submitted_at", "actor_kind", "channel"} {
		query = strings.ReplaceAll(query, "ws."+field, "(CASE WHEN wire_header.submission_id IS NOT NULL THEN wire_header."+field+" ELSE wire_fallback."+field+" END)")
	}
	if strings.Contains(query, "ws.") || strings.Contains(query, "eii.") {
		return "", nil, fmt.Errorf("narrow evidence has an unprojected source reference")
	}
	// Only rank/count inputs cross the source and family windows. Full source,
	// identity and selected-analysis fields rejoin after LIMIT, also for samples.
	// The source_rank window guarantees normalized has one row per report seq.
	start, end := strings.Index(query, " family_samples AS ("), strings.Index(query, "\n briefing AS (")
	if start < 0 || end <= start {
		return "", nil, fmt.Errorf("narrow evidence sample reference changed")
	}
	samples := strings.TrimSuffix(strings.TrimSpace(query[start:end]), ",")
	query = query[:start] + query[end:]
	if err := replaceOne("COALESCE(samples.supporting_sources,JSON_ARRAY()) AS supporting_sources", "JSON_ARRAY() AS supporting_sources"); err != nil {
		return "", nil, err
	}
	if err := replaceOne(" LEFT JOIN family_samples samples ON samples.family_representative_seq=f.family_representative_seq", ""); err != nil {
		return "", nil, err
	}
	for _, change := range [][2]string{
		{"SELECT family_representative_seq,", "SELECT f.family_representative_seq,"},
		{"JSON_OBJECT('core',", "JSON_OBJECT('rank',f.family_rank,'core',"},
		{"'source_url',source_url", "'source_url',n.source_url"},
		{"'report_kind',report_kind", "'report_kind',n.report_kind"},
		{"YEAR(source_timestamp)", "YEAR(n.source_timestamp)"},
		{"DATE_FORMAT(source_timestamp,", "DATE_FORMAT(n.source_timestamp,"},
		{"FROM filtered WHERE family_rank <= 8 AND privacy_restricted = 0 GROUP BY family_representative_seq", "FROM filtered f JOIN normalized n ON n.seq=f.seq WHERE f.family_representative_seq IN (SELECT seq FROM paged) AND f.family_rank <= 8 AND f.privacy_restricted = 0 GROUP BY f.family_representative_seq"},
	} {
		if strings.Count(samples, change[0]) != 1 {
			return "", nil, fmt.Errorf("narrow evidence sample detail contract changed")
		}
		samples = strings.Replace(samples, change[0], change[1], 1)
	}
	paged := " paged AS (SELECT * FROM briefing ORDER BY " + evidenceOrderBy(f.Sort) + " LIMIT ? OFFSET ?)"
	if err := replaceOne(paged, paged+`,
 paged_details AS (SELECT n.*,p.import_count,p.case_id,p.group_key,p.supporting_report_count,p.source_account_count,p.duplicate_import_count,p.related_observation_count,p.last_signal_at,p.latest_observed_at,p.latest_source_published_at FROM paged p JOIN normalized n ON n.seq=p.seq),
 `+samples); err != nil {
		return "", nil, err
	}
	if err := replaceOne("COALESCE(p.supporting_sources,JSON_ARRAY())", "COALESCE(samples.supporting_sources,JSON_ARRAY())"); err != nil {
		return "", nil, err
	}
	if err := replaceOne("LEFT JOIN paged p ON TRUE ORDER BY", "LEFT JOIN paged_details p ON TRUE LEFT JOIN family_samples samples ON samples.family_representative_seq=p.seq ORDER BY"); err != nil {
		return "", nil, err
	}
	return query, args, nil
}

// Kept small independently of public display/source widths. All other fields
// are restored from the same normalized snapshot only for chosen report seqs.
const narrowEvidenceRankColumns = "seq,source_key,evidence_version,evidence_strength,entity_relevance,severity_level,report_kind,source_account_key,observed_at,source_timestamp,ts,privacy_restricted"

func narrowEvidenceAnalysisJSON(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return "JSON_OBJECT('seq'," + prefix + "seq,'language'," + prefix + "language,'created',DATE_FORMAT(" + prefix + "analysis_created_at,'%Y-%m-%d %H:%i:%s.%f'),'brand'," + prefix + "brand_name,'classification'," + prefix + "classification,'severity'," + prefix + "severity_level)"
}

type narrowEvidenceAnalysis struct {
	Seq            int      `json:"seq"`
	Language       *string  `json:"language"`
	Created        *string  `json:"created"`
	Brand          string   `json:"brand"`
	Classification string   `json:"classification"`
	Severity       *float64 `json:"severity"`
}

func (n narrowEvidenceAnalysis) core() evidenceAnalysisCore {
	c := evidenceAnalysisCore{CreatedLiteral: n.Created, LanguageNull: n.Language == nil}
	c.Record.Seq, c.Record.Brand, c.Record.Classification = n.Seq, n.Brand, n.Classification
	if n.Language != nil {
		c.Record.Language = *n.Language
	}
	c.Record.SeverityNull = n.Severity == nil
	if n.Severity != nil {
		c.Record.Severity = *n.Severity
	}
	return c
}

type narrowEvidenceSample struct {
	Rank      int                    `json:"rank"`
	Core      narrowEvidenceAnalysis `json:"core"`
	SourceURL string                 `json:"source_url"`
	Kind      string                 `json:"report_kind"`
	Published *time.Time             `json:"source_published_at"`
}

// The method stays off the public route until one explicit serving strategy is
// chosen after the shared MySQL parity suite and the unchanged 8s runtime check.
func (d *Database) getEvidenceBrandPageNarrowSQL(ctx context.Context, f EvidenceFilter) (EvidenceBrandPage, error) {
	f, err := NormalizeEvidenceFilter(f)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	query, args, err := narrowEvidenceBrandPageQuery(f)
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
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}, Overview: evidenceOverview(nil, f.asOf)}
	grouped := evidenceGroupedPage{Records: []evidenceRecord{}, SupportingSamples: map[int][]evidenceRecord{}}
	chosen := map[int]evidenceAnalysisCore{}
	rows, err := tx.QueryContext(queryCtx, query, args...)
	if err != nil {
		return EvidenceBrandPage{}, fmt.Errorf("query narrow evidence brand briefing: %w", err)
	}
	for rows.Next() {
		var r evidenceRecord
		var coreJSON []byte
		if err := rows.Scan(&page.KindCounts.All, &page.KindCounts.Firsthand, &page.KindCounts.Coverage, &page.KindCounts.Unknown, &page.Total, &page.High, &page.Medium, &page.Overview.TotalReports, &page.Overview.ReportsLast30Days, &page.Overview.ReportsLast7Days, &page.Overview.ReportsPrevious7Days, &page.Overview.LatestReportAt,
			&r.Seq, &r.PublicID, &r.IngestedAt, &r.Latitude, &r.Longitude, &r.Title, &r.Summary, &r.Classification, &r.Severity, &r.Language, &r.Brand, &r.BrandDisplay,
			&r.Kind, &r.Excerpt, &r.Impact, &r.Reason, &r.Relationship, &r.Version, &r.SourcePlatform, &r.SourceURL, &r.PublishedAt, &r.ObservedAt, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey, &r.ImportCount, &r.CaseID, &r.RelatedCount, &r.HasOriginalMedia, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.GroupKey, &r.SupportingCount, &r.SourceAccountCount, &r.FamilyDuplicates, &r.SupportingSourcesJSON, &r.LatestObservedAt, &r.LatestPublishedAt, &r.IngestionTimeMissing, &r.IngestedAtNull, &coreJSON); err != nil {
			rows.Close()
			return EvidenceBrandPage{}, err
		}
		if r.Seq == 0 {
			continue
		}
		var selection narrowEvidenceAnalysis
		if err := json.Unmarshal(coreJSON, &selection); err != nil || selection.Seq != r.Seq {
			rows.Close()
			return EvidenceBrandPage{}, fmt.Errorf("narrow evidence page selection is invalid")
		}
		chosen[r.Seq] = selection.core()
		var samples []narrowEvidenceSample
		if err := json.Unmarshal(r.SupportingSourcesJSON, &samples); err != nil {
			rows.Close()
			return EvidenceBrandPage{}, fmt.Errorf("narrow evidence samples are invalid")
		}
		// JSON_ARRAYAGG order is unspecified once late detail joins are added;
		// retain the existing deterministic family rank across that join.
		sort.SliceStable(samples, func(i, j int) bool { return samples[i].Rank < samples[j].Rank })
		for _, sample := range samples {
			if sample.Core.Seq <= 0 || sample.Rank < 1 || sample.Rank > 8 {
				rows.Close()
				return EvidenceBrandPage{}, fmt.Errorf("narrow evidence sample selection is invalid")
			}
			chosen[sample.Core.Seq] = sample.Core.core()
			grouped.SupportingSamples[r.Seq] = append(grouped.SupportingSamples[r.Seq], evidenceRecord{Seq: sample.Core.Seq, SourceURL: sample.SourceURL, Kind: sample.Kind, PublishedAt: sample.Published})
		}
		grouped.Records = append(grouped.Records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	if err := hydrateEvidenceDisplay(queryCtx, tx, f, chosen, &grouped); err != nil {
		return EvidenceBrandPage{}, err
	}
	mediaSeqs := []int{}
	for _, r := range grouped.Records {
		if r.HasOriginalMedia && !r.PrivacyRestricted {
			mediaSeqs = append(mediaSeqs, r.Seq)
		}
	}
	valid, err := loadEvidenceImageValidity(queryCtx, tx, mediaSeqs)
	if err != nil {
		return EvidenceBrandPage{}, err
	}
	for _, r := range grouped.Records {
		ev, rec := r.publicEvidence(), r.publicRecurrence()
		ev.HasOriginalMedia = ev.HasOriginalMedia && valid[r.Seq]
		page.Reports = append(page.Reports, models.ReportWithMinimalAnalysis{Report: models.Report{Seq: r.Seq, PublicID: r.PublicID, Timestamp: r.IngestedAt, Latitude: r.Latitude, Longitude: r.Longitude, SourceTimestamp: ev.SourcePublishedAt, SourceURL: ev.SourceURL}, Analysis: []models.MinimalAnalysis{{Title: r.Title, Summary: r.Summary, Classification: r.Classification, SeverityLevel: r.Severity, Language: r.Language, BrandName: r.Brand, BrandDisplayName: r.BrandDisplay}}, Evidence: &ev, Recurrence: &rec})
	}
	if err := tx.Commit(); err != nil {
		return EvidenceBrandPage{}, err
	}
	page.HasMore = f.Offset+len(page.Reports) < page.Total
	return page, nil
}
