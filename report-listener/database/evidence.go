package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"cleanapp-common/evidence"
	"report-listener/models"
)

type EvidenceFilter struct {
	BrandName, Product, Kind, Sort, Language string
	Limit, Offset                            int
	Brands                                   []string
}

// Identity aliases are exact matches. Parent pages include their known products.
var evidenceProducts = map[string][]string{
	"anthropic": {"anthropic", "claude", "claude-code", "claudecode"},
	"claude":    {"claude", "claude-code", "claudecode"},
	"openai":    {"openai", "chatgpt", "chat-gpt", "codex", "openai-api", "openaiapi", "openai-node", "openainode", "openai-python", "openaipython", "whisper", "dall-e", "dalle"},
	"xai":       {"xai", "x-ai", "grok"}, "x-ai": {"xai", "x-ai", "grok"},
}

var evidenceProductAliases = map[string][]string{
	"claude-code": {"claude-code", "claudecode"}, "claudecode": {"claude-code", "claudecode"},
	"openai-node": {"openai-node", "openainode"}, "openainode": {"openai-node", "openainode"},
	"openai-python": {"openai-python", "openaipython"}, "openaipython": {"openai-python", "openaipython"},
	"openai-api": {"openai-api", "openaiapi"}, "openaiapi": {"openai-api", "openaiapi"},
	"chatgpt": {"chatgpt", "chat-gpt"}, "chat-gpt": {"chatgpt", "chat-gpt"},
	"dall-e": {"dall-e", "dalle"}, "dalle": {"dall-e", "dalle"},
}
var evidenceScopeProducts = map[string][]string{
	"anthropic": {"anthropic", "claude", "claude-code"}, "claude": {"claude", "claude-code"},
	"openai": {"openai", "chatgpt", "codex", "openai-api", "openai-node", "openai-python", "whisper", "dall-e"},
	"xai":    {"xai", "grok"}, "x-ai": {"xai", "grok"},
}

func NormalizeEvidenceFilter(f EvidenceFilter) (EvidenceFilter, error) {
	f.BrandName = strings.ToLower(strings.TrimSpace(f.BrandName))
	f.Product = strings.ToLower(strings.TrimSpace(f.Product))
	if f.BrandName == "" || len(f.BrandName) > 255 {
		return f, fmt.Errorf("brand_name is required and must be at most 255 characters")
	}
	if f.Kind == "" {
		f.Kind = "firsthand"
	}
	if f.Sort == "" {
		f.Sort = "signal"
	}
	if f.Language == "" {
		f.Language = "en"
	}
	switch f.Kind {
	case "firsthand", "coverage", "unknown", "all":
	default:
		return f, fmt.Errorf("kind must be firsthand, coverage, unknown, or all")
	}
	switch f.Sort {
	case "signal", "recent", "recurrence":
	default:
		return f, fmt.Errorf("sort must be signal, recent, or recurrence")
	}
	if f.Offset < 0 || f.Offset > 100000 {
		return f, fmt.Errorf("offset must be between 0 and 100000")
	}
	if f.Limit <= 0 {
		f.Limit = 12
	}
	if f.Limit > 50 {
		f.Limit = 50
	}
	f.Brands = append([]string(nil), evidenceProducts[f.BrandName]...)
	if len(f.Brands) == 0 {
		f.Brands = append([]string(nil), evidenceProductAliases[f.BrandName]...)
		if len(f.Brands) == 0 {
			f.Brands = []string{f.BrandName}
		}
	}
	if f.Product != "" {
		found := false
		for _, brand := range f.Brands {
			if f.Product == brand {
				found = true
			}
		}
		if !found {
			return f, fmt.Errorf("product must belong to the requested brand scope")
		}
		f.Brands = append([]string(nil), evidenceProductAliases[f.Product]...)
		if len(f.Brands) == 0 {
			f.Brands = []string{f.Product}
		}
	}
	return f, nil
}
func (f EvidenceFilter) Scope() models.EvidenceScope {
	products := append([]string(nil), evidenceScopeProducts[f.BrandName]...)
	if len(products) == 0 {
		products = []string{f.BrandName}
	}
	var product *string
	if f.Product != "" {
		product = &f.Product
	}
	return models.EvidenceScope{BrandName: f.BrandName, Product: product, IncludesProducts: len(products) > 1 && f.Product == "", Products: products}
}
func evidenceKindWhere(kind string) string {
	if kind == "all" {
		return "1 = 1"
	}
	return "report_kind = ?"
}

// Signal ranking keeps original evidence/impact and scoped entity relevance
// ahead of related-source recurrence. Case membership remains unassessed.
func evidenceOrderBy(sortBy string) string {
	switch sortBy {
	case "recent":
		return "last_signal_at DESC, seq DESC"
	case "recurrence":
		return "related_observation_count DESC, evidence_strength DESC, last_signal_at DESC, seq DESC"
	default:
		return "evidence_strength DESC, entity_relevance DESC, supporting_report_count DESC, severity_level DESC, last_signal_at DESC, seq DESC"
	}
}

// Count and page queries share the exact same universe. Translations and imports
// are collapsed before filtering; case links describe related observations only.
// Computed source fields use a binary collation: legacy indexed tables and Wire
// metadata have different collations, and source IDs / URL paths are case-sensitive.
// Keep the stored index columns untouched so indexed joins remain available.
func evidenceBrandCTE(f EvidenceFilter) (string, []interface{}) {
	placeholders := make([]string, len(f.Brands))
	args := []interface{}{f.Language}
	for i, brand := range f.Brands {
		placeholders[i] = "?"
		args = append(args, brand)
	}
	return evidenceCTE(f, "ra.brand_name IN ("+strings.Join(placeholders, ",")+") AND ra.classification = 'digital'", args)
}

func evidenceCTE(f EvidenceFilter, analysisWhere string, args []interface{}) (string, []interface{}) {
	query := fmt.Sprintf(`WITH preferred AS (
  SELECT ra.seq,ra.language,ra.created_at,ra.brand_name,ra.brand_display_name,ra.classification,ra.title,ra.summary,ra.severity_level,ra.report_kind,ra.evidence_excerpt,ra.reported_impact,ra.classification_reason,ra.entity_relationship,ra.evidence_version, ROW_NUMBER() OVER (PARTITION BY ra.seq ORDER BY (ra.language = ?) DESC, (ra.language = 'en') DESC, ra.language ASC, ra.created_at DESC) AS analysis_rank
  FROM report_analysis ra WHERE %s AND ra.is_valid = TRUE
 ), candidates AS (
  SELECT r.seq, r.public_id, r.ts, r.latitude, r.longitude, (LENGTH(r.image) > 0) AS has_original_media,
   ra.title, ra.summary, ra.classification, ra.severity_level, ra.language, ra.brand_name, ra.brand_display_name,
   CASE WHEN ra.evidence_version > 0 AND ra.report_kind IN ('firsthand','coverage') THEN ra.report_kind ELSE 'unknown' END AS report_kind,
   ra.evidence_excerpt, ra.reported_impact, ra.classification_reason, ra.entity_relationship, ra.evidence_version,
   COALESCE(NULLIF(eii.source,''), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.platform')),''), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.legacy_source')),''), rr.source_type, '') COLLATE utf8mb4_bin AS source_platform,
   COALESCE(NULLIF(eii.source_url,''), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.source_url')),'null'), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.source_url')),'null'), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.url')),'null'), JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.evidence_bundle[0].uri')), '') COLLATE utf8mb4_bin AS source_url,
   COALESCE(eii.source_timestamp, CASE WHEN JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.source_published_at')) REGEXP '(Z|[+]00:00)$' THEN STR_TO_DATE(REPLACE(LEFT(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.source_published_at')),19),'T',' '),'%%Y-%%m-%%d %%H:%%i:%%s') ELSE NULL END, CASE WHEN JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.source_published_at') IS NULL AND JSON_UNQUOTE(JSON_EXTRACT(ws.provenance_json,'$.generation_method')) IN ('twitter_submitter','github_issue_submitter','bluesky_submitter','human_authored_public') THEN ws.observed_at ELSE NULL END) AS source_timestamp,
   ws.observed_at AS raw_observed_at, ws.submitted_at, (COALESCE(JSON_EXTRACT(ws.extensions_json,'$.observation_time_supplied'),FALSE) = TRUE) AS observation_time_supplied, (JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.source_published_at') IS NOT NULL) AS explicit_source_publication,
   COALESCE(ws.actor_kind, '') AS actor_kind, COALESCE(ws.channel,'') AS intake_channel,
   COALESCE(JSON_UNQUOTE(JSON_EXTRACT(ws.provenance_json,'$.generation_method')),'') AS generation_method,
   (eii.source IS NOT NULL) AS indexed_source, COALESCE(rr.fetcher_id,'') AS fetcher_id,
   COALESCE(eii.external_id, rr.source_id, '') COLLATE utf8mb4_bin AS original_id,
   COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.author_did')),'null'), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.author_username')),'null'), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.metadata.author_handle')),'null'), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(ws.report_json,'$.digital_context.author_handle')),'null')) AS source_author,
   CASE WHEN JSON_EXTRACT(ws.report_json,'$.privacy.contains_pii') = TRUE OR JSON_EXTRACT(ws.report_json,'$.privacy.requires_redaction') = TRUE THEN 1 ELSE 0 END AS privacy_restricted,
   ROW_NUMBER() OVER (PARTITION BY r.seq ORDER BY eii.source ASC, eii.external_id ASC, ws.submitted_at DESC) AS source_rank
  FROM preferred ra JOIN reports r ON r.seq = ra.seq
  LEFT JOIN report_raw rr ON rr.report_seq = r.seq
  LEFT JOIN external_ingest_index eii ON eii.seq = r.seq
  LEFT JOIN wire_submissions_raw ws ON ws.report_seq = r.seq
  WHERE ra.analysis_rank = 1 AND r.public_id <> '' AND %s
   AND NOT EXISTS (SELECT 1 FROM report_status rs WHERE rs.seq = r.seq AND rs.status IS NOT NULL AND rs.status <> 'active')
   AND NOT EXISTS (SELECT 1 FROM reports_owners ro WHERE ro.seq = r.seq AND ro.owner IS NOT NULL AND ro.owner <> '' AND ro.is_public = FALSE)
 ), provenance AS (
  SELECT candidates.*,
   (indexed_source OR source_platform IN ('twitter','x','bluesky','github','github_issue','reddit') OR generation_method = 'legacy_v3_bulk_ingest') AS external_source,
   CASE WHEN source_url REGEXP '^https?://' THEN
    CONCAT(LOWER(REGEXP_REPLACE(SUBSTRING_INDEX(SUBSTRING_INDEX(TRIM(source_url),'://',-1),'/',1),'^www[.]','')),
     REGEXP_REPLACE(CASE WHEN source_url REGEXP '^https?://(www[.])?(github[.]com|x[.]com|twitter[.]com|reddit[.]com|bsky[.]app)/' THEN TRIM(TRAILING '/' FROM SUBSTRING_INDEX(SUBSTRING_INDEX(TRIM(source_url),'#',1),'?',1)) ELSE SUBSTRING_INDEX(TRIM(source_url),'#',1) END,'^https?://[^/]+',''))
    ELSE '' END AS canonical_url
  FROM candidates WHERE source_rank = 1
 ), normalized AS (
  SELECT provenance.*,
   CASE WHEN source_platform='bluesky' AND original_id REGEXP '^at://' THEN CONCAT('source:bluesky:',original_id)
    WHEN canonical_url REGEXP '^(twitter[.]com|x[.]com)/[^/]+/status/[0-9]+' THEN CONCAT('source:twitter:',SUBSTRING_INDEX(SUBSTRING_INDEX(canonical_url,'/status/',-1),'/',1))
    WHEN canonical_url <> '' THEN CONCAT('url:',canonical_url)
    WHEN original_id <> '' AND source_platform IN ('twitter','x','bluesky','github','github_issue','reddit') THEN CONCAT('source:',CASE WHEN source_platform='x' THEN 'twitter' WHEN source_platform='github_issue' THEN 'github' ELSE source_platform END,':',REGEXP_REPLACE(original_id,'^(twitter|x|github_issue|github|reddit|bluesky):',''))
    WHEN original_id <> '' THEN CONCAT('source:',source_platform,':',fetcher_id,':',original_id)
    ELSE CONCAT('report:',seq) END AS source_key,
   CASE WHEN generation_method = 'legacy_v3_bulk_ingest' OR (generation_method='human_submission' AND raw_observed_at=submitted_at AND NOT observation_time_supplied) OR (generation_method='shared_submission' AND NOT observation_time_supplied) OR (generation_method IN ('twitter_submitter','github_issue_submitter','bluesky_submitter','human_authored_public') AND NOT explicit_source_publication) THEN NULL ELSE raw_observed_at END AS observed_at,
   CASE WHEN COALESCE(source_author,'') <> '' THEN CONCAT(CASE WHEN source_platform='github_issue' THEN 'github' WHEN source_platform='x' THEN 'twitter' ELSE source_platform END,':',LOWER(source_author)) ELSE NULL END AS source_account_key,
   CASE WHEN evidence_version > 0 AND (COALESCE(evidence_excerpt,'') <> '' OR has_original_media) AND privacy_restricted = 0 THEN 2 ELSE 0 END +
   CASE WHEN evidence_version > 0 AND COALESCE(reported_impact,'') <> '' AND privacy_restricted = 0 THEN 1 ELSE 0 END AS evidence_strength,
   CASE WHEN evidence_version < 1 THEN 0 WHEN entity_relationship='affected' THEN 2 WHEN entity_relationship='dependency' THEN 1 ELSE 0 END AS entity_relevance
  FROM provenance
 ), representatives AS (
  SELECT normalized.*, ROW_NUMBER() OVER (PARTITION BY source_key ORDER BY evidence_version DESC, evidence_strength DESC, entity_relevance DESC, seq DESC) AS duplicate_rank,
   COUNT(*) OVER (PARTITION BY source_key) AS import_count FROM normalized
 ), entries AS (SELECT * FROM representatives WHERE duplicate_rank = 1),
 case_links AS (SELECT e.seq, MIN(cr.case_id) AS case_id FROM case_reports cr JOIN normalized n ON n.seq = cr.seq JOIN entries e ON e.source_key = n.source_key GROUP BY e.seq),
 tagged AS (SELECT e.*, cl.case_id, CASE WHEN cl.case_id IS NULL THEN source_key ELSE CONCAT('family:',cl.case_id) END AS group_key FROM entries e LEFT JOIN case_links cl ON cl.seq=e.seq),
 filtered AS (SELECT tagged.*, ROW_NUMBER() OVER (PARTITION BY group_key ORDER BY evidence_strength DESC, entity_relevance DESC, severity_level DESC, COALESCE(observed_at,source_timestamp,ts) DESC,seq DESC) AS family_rank FROM tagged WHERE %s),
 family_stats AS (SELECT group_key, COUNT(*) AS supporting_report_count, CASE WHEN COUNT(source_account_key)=COUNT(*) THEN COUNT(DISTINCT source_account_key) ELSE NULL END AS source_account_count, SUM(import_count)-COUNT(*) AS duplicate_import_count, MAX(COALESCE(observed_at,source_timestamp,ts)) AS last_signal_at, MAX(observed_at) AS latest_observed_at, MAX(source_timestamp) AS latest_source_published_at FROM filtered GROUP BY group_key),
 family_samples AS (SELECT group_key, JSON_ARRAYAGG(JSON_OBJECT('title',title,'source_url',source_url,'report_kind',report_kind,'source_published_at',DATE_FORMAT(source_timestamp,'%%Y-%%m-%%dT%%H:%%i:%%sZ'))) AS supporting_sources FROM filtered WHERE family_rank <= 8 GROUP BY group_key),
 briefing AS (SELECT f.*, fs.supporting_report_count, fs.source_account_count, fs.duplicate_import_count, fs.supporting_report_count AS related_observation_count, fs.last_signal_at, fs.latest_observed_at, fs.latest_source_published_at, samples.supporting_sources FROM filtered f JOIN family_stats fs ON fs.group_key=f.group_key JOIN family_samples samples ON samples.group_key=f.group_key WHERE family_rank=1)
 `, analysisWhere, PublicVisibilityWhereSQL, evidenceKindWhere(f.Kind))
	if f.Kind != "all" {
		args = append(args, f.Kind)
	}
	return query, args
}

type evidenceRecord struct {
	Seq                                                           int
	PublicID                                                      string
	IngestedAt                                                    time.Time
	Latitude, Longitude                                           float64
	Title, Summary, Classification, Language, Brand, BrandDisplay string
	Severity                                                      float64
	Kind, Excerpt, Impact, Reason, Relationship                   string
	Version                                                       int
	SourcePlatform, SourceURL                                     string
	PublishedAt, ObservedAt                                       *time.Time
	ActorKind, FetcherID                                          string
	PrivacyRestricted                                             bool
	HasOriginalMedia                                              bool
	SourceKey                                                     string
	ExternalSource                                                bool
	IntakeChannel, GenerationMethod, SourceAccountKey             string
	GroupKey                                                      string
	SupportingCount                                               int
	SourceAccountCount                                            *int
	FamilyDuplicates                                              int
	SupportingSourcesJSON                                         []byte
	LatestObservedAt, LatestPublishedAt                           *time.Time
	ImportCount                                                   int
	CaseID                                                        *string
	RelatedCount                                                  int
}
type EvidenceBrandPage struct {
	Reports             []models.ReportWithMinimalAnalysis
	Total, High, Medium int
	KindCounts          models.EvidenceKindCounts
	HasMore             bool
}

func (d *Database) GetEvidenceBrandPage(ctx context.Context, f EvidenceFilter) (EvidenceBrandPage, error) {
	var err error
	f, err = NormalizeEvidenceFilter(f)
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
	cte, args := evidenceBrandCTE(f)
	page := EvidenceBrandPage{Reports: []models.ReportWithMinimalAnalysis{}}
	countsQuery := cte + `SELECT COUNT(DISTINCT group_key), COUNT(DISTINCT CASE WHEN report_kind='firsthand' THEN group_key END), COUNT(DISTINCT CASE WHEN report_kind='coverage' THEN group_key END), COUNT(DISTINCT CASE WHEN report_kind='unknown' THEN group_key END) FROM tagged`
	if err := tx.QueryRowContext(queryCtx, countsQuery, args...).Scan(&page.KindCounts.All, &page.KindCounts.Firsthand, &page.KindCounts.Coverage, &page.KindCounts.Unknown); err != nil {
		return page, fmt.Errorf("count evidence brand scope: %w", err)
	}
	countArgs := append([]interface{}(nil), args...)

	countsQuery = cte + `SELECT COUNT(*), COALESCE(SUM(severity_level>=0.7),0), COALESCE(SUM(severity_level>=0.4 AND severity_level<0.7),0) FROM briefing`
	if err := tx.QueryRowContext(queryCtx, countsQuery, countArgs...).Scan(&page.Total, &page.High, &page.Medium); err != nil {
		return page, fmt.Errorf("count filtered evidence brand scope: %w", err)
	}
	listArgs := append([]interface{}(nil), countArgs...)
	listArgs = append(listArgs, f.Limit, f.Offset)
	query := cte + `SELECT seq,public_id,ts,latitude,longitude,title,summary,classification,severity_level,language,brand_name,brand_display_name,
 report_kind,COALESCE(evidence_excerpt,''),COALESCE(reported_impact,''),COALESCE(classification_reason,''),COALESCE(entity_relationship,'unknown'),evidence_version,
 source_platform,source_url,source_timestamp,observed_at,actor_kind,fetcher_id,privacy_restricted,source_key,import_count,case_id,related_observation_count,has_original_media,external_source,intake_channel,generation_method,COALESCE(source_account_key,''),group_key,supporting_report_count,source_account_count,duplicate_import_count,supporting_sources,latest_observed_at,latest_source_published_at
 FROM briefing` + ` ORDER BY ` + evidenceOrderBy(f.Sort) + ` LIMIT ? OFFSET ?`
	mediaSeqs := []int{}
	rows, err := tx.QueryContext(queryCtx, query, listArgs...)
	if err != nil {
		return page, fmt.Errorf("query evidence brand briefing: %w", err)
	}
	for rows.Next() {
		var r evidenceRecord
		if err := rows.Scan(&r.Seq, &r.PublicID, &r.IngestedAt, &r.Latitude, &r.Longitude, &r.Title, &r.Summary, &r.Classification, &r.Severity, &r.Language, &r.Brand, &r.BrandDisplay,
			&r.Kind, &r.Excerpt, &r.Impact, &r.Reason, &r.Relationship, &r.Version, &r.SourcePlatform, &r.SourceURL, &r.PublishedAt, &r.ObservedAt, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey, &r.ImportCount, &r.CaseID, &r.RelatedCount, &r.HasOriginalMedia, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.GroupKey, &r.SupportingCount, &r.SourceAccountCount, &r.FamilyDuplicates, &r.SupportingSourcesJSON, &r.LatestObservedAt, &r.LatestPublishedAt); err != nil {
			rows.Close()
			return page, err
		}
		ev := r.publicEvidence()
		if ev.HasOriginalMedia {
			mediaSeqs = append(mediaSeqs, r.Seq)
		}
		rec := r.publicRecurrence()
		page.Reports = append(page.Reports, models.ReportWithMinimalAnalysis{Report: models.Report{Seq: r.Seq, PublicID: r.PublicID, Timestamp: r.IngestedAt, Latitude: r.Latitude, Longitude: r.Longitude, SourceTimestamp: r.PublishedAt, SourceURL: ev.SourceURL}, Analysis: []models.MinimalAnalysis{{Title: r.Title, Summary: r.Summary, Classification: r.Classification, SeverityLevel: r.Severity, Language: r.Language, BrandName: r.Brand, BrandDisplayName: r.BrandDisplay}}, Evidence: &ev, Recurrence: &rec})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(mediaSeqs) > 0 {
		valid, err := loadEvidenceImageValidity(queryCtx, tx, mediaSeqs)
		if err != nil {
			return page, err
		}
		for i := range page.Reports {
			if page.Reports[i].Evidence.HasOriginalMedia {
				page.Reports[i].Evidence.HasOriginalMedia = valid[page.Reports[i].Report.Seq]
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return page, err
	}
	page.HasMore = f.Offset+len(page.Reports) < page.Total
	return page, nil
}

func safeEvidenceURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast())) {
		return ""
	}
	for key, value := range u.Query() {
		key = strings.ToLower(key)
		if len(value) > 0 && (strings.Contains(key, "token") || strings.Contains(key, "signature") || key == "key" || key == "api_key" || key == "password" || key == "auth" || key == "authorization") {
			return ""
		}
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	switch strings.TrimPrefix(host, "www.") {
	case "github.com", "x.com", "twitter.com", "reddit.com", "bsky.app":
		u.RawQuery = ""
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	return u.String()
}

var sensitiveEvidence = regexp.MustCompile(`(?i)([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}|sk-[a-z0-9_-]{12,}|(?:api[_ -]?key|password|secret|token|authorization)["']?\s*[:=]\s*["']?[^\s"']{8,}|bearer\s+[a-z0-9._-]{12,})`)

func hasUnsafeEvidenceControls(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return true
		}
	}
	return false
}

func evidenceText(value string, max int) *string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value))
	if value == "" {
		return nil
	}
	runes := []rune(value)
	if len(runes) > max {
		value = string(runes[:max])
	}
	return &value
}
func (r evidenceRecord) publicEvidence() models.PublicEvidence {
	kind := r.Kind
	if r.Version < 1 || (kind != "firsthand" && kind != "coverage") {
		kind = "unknown"
	}
	sourceURL := safeEvidenceURL(r.SourceURL)
	platform := strings.TrimSpace(r.SourcePlatform)
	intake, label := "unknown", "Submission provenance unavailable"
	nativeChannel := r.IntakeChannel == "mobile" || r.IntakeChannel == "web" || r.IntakeChannel == "share" || r.IntakeChannel == "legacy_v2"
	if r.ActorKind == "human" && nativeChannel && (r.GenerationMethod == "human_submission" || r.GenerationMethod == "shared_submission") {
		intake, label = "cleanapp_submission", "Submitted through CleanApp"
	} else if r.ExternalSource {
		intake, label = "imported", "Imported public report"
	} else if platform != "" || r.FetcherID != "" || r.ActorKind == "machine" {
		intake, label = "imported", "Imported public report"
	}
	if platform == "" && sourceURL != "" {
		u, _ := url.Parse(sourceURL)
		platform = u.Hostname()
	}
	if intake == "imported" && strings.Contains(strings.ToLower(platform), "github") {
		label = "Imported GitHub report"
	}
	relationship := r.Relationship
	switch relationship {
	case "affected", "dependency", "mentioned":
	default:
		relationship = "unknown"
	}
	ev := models.PublicEvidence{ReportKind: kind, IntakeMethod: intake, IntakeLabel: label, SourcePlatform: evidenceText(platform, 64), SourceURL: evidenceText(sourceURL, 2048), SourcePublishedAt: r.PublishedAt, ObservedAt: r.ObservedAt, IngestedAt: r.IngestedAt, EntityRelationship: relationship, ValidationStatus: "not_independently_reproduced", UpstreamStatus: "unknown", ResolutionStatus: "unknown", WhySurfaced: []string{}, EvidenceVersion: r.Version, HasOriginalMedia: r.HasOriginalMedia && !r.PrivacyRestricted}
	if r.Version > 0 && !r.PrivacyRestricted {
		if !sensitiveEvidence.MatchString(r.Excerpt) && !hasUnsafeEvidenceControls(r.Excerpt) {
			ev.Excerpt = evidenceText(r.Excerpt, 500)
		}
		if !sensitiveEvidence.MatchString(r.Impact) {
			ev.ReportedImpact = evidenceText(r.Impact, 500)
		}
		if !sensitiveEvidence.MatchString(r.Reason) {
			ev.ClassificationReason = evidenceText(r.Reason, 500)
		}
	}
	if ev.Excerpt != nil {
		ev.WhySurfaced = append(ev.WhySurfaced, "Original source excerpt available")
	}
	if ev.ReportedImpact != nil {
		ev.WhySurfaced = append(ev.WhySurfaced, "Reported consequence documented")
	}
	if relationship == "dependency" {
		ev.WhySurfaced = append(ev.WhySurfaced, "Reported integration dependency; product responsibility is unverified")
	}
	if r.RelatedCount > 1 {
		ev.WhySurfaced = append(ev.WhySurfaced, "Related observations linked by CleanApp; independence and defect equivalence unassessed")
	}
	if len(ev.WhySurfaced) == 0 {
		ev.WhySurfaced = append(ev.WhySurfaced, "Individual observation; further evidence review needed")
	}
	return ev
}
func (r evidenceRecord) publicRecurrence() models.PublicRecurrence {
	sourceKey := r.GroupKey
	if sourceKey == "" {
		sourceKey = r.SourceKey
	}
	if sourceKey == "" {
		sourceKey = fmt.Sprintf("report:%d", r.Seq)
	}
	digest := sha256.Sum256([]byte(sourceKey))
	count := r.SupportingCount
	if count < 1 {
		count = 1
	}
	duplicates := r.FamilyDuplicates
	if duplicates == 0 && r.ImportCount > 1 {
		duplicates = r.ImportCount - 1
	}
	var related *int
	grouping := "source_observation"
	if r.CaseID != nil {
		related = &r.RelatedCount
		if count > 1 {
			grouping = "related_issue_family"
		}
	}
	samples := []models.PublicSupportingSource{}
	if err := json.Unmarshal(r.SupportingSourcesJSON, &samples); err == nil {
		for i := range samples {
			safe := safeEvidenceURL(stringValue(samples[i].SourceURL))
			samples[i].SourceURL = evidenceText(safe, 2048)
			samples[i].Title = stringValue(evidenceText(samples[i].Title, 255))
		}
	} else {
		samples = []models.PublicSupportingSource{}
	}
	return models.PublicRecurrence{IssueID: fmt.Sprintf("issue_%x", digest[:8]), CaseID: r.CaseID, Grouping: grouping, SupportingReportCount: count, SourceAccountCount: r.SourceAccountCount, DuplicateImportCount: duplicates, Independence: "unassessed", RelatedObservationCount: related, CountsExact: true, SupportingSources: samples, LatestObservedAt: r.LatestObservedAt, LatestSourcePublishedAt: r.LatestPublishedAt}
}
func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// AttachPublicEvidence enriches a bounded detail batch without fetching raw
// source bodies or exposing reporter identity. Historical ingestion never stands
// in for an original-source or incident date.
func (d *Database) AttachPublicEvidence(ctx context.Context, reports []*models.ReportWithAnalysis) error {
	if len(reports) == 0 {
		return nil
	}
	if len(reports) > 100 {
		return fmt.Errorf("evidence detail batch exceeds 100 reports")
	}
	args := []interface{}{"en"}
	placeholders := make([]string, len(reports))
	for i, r := range reports {
		args = append(args, r.Report.Seq)
		placeholders[i] = "?"
	}
	cte, args := evidenceCTE(EvidenceFilter{Kind: "all"}, "ra.seq IN ("+strings.Join(placeholders, ",")+")", args)
	query := cte + `SELECT seq,public_id,ts,latitude,longitude,title,summary,classification,severity_level,language,brand_name,brand_display_name,
 report_kind,COALESCE(evidence_excerpt,''),COALESCE(reported_impact,''),COALESCE(classification_reason,''),COALESCE(entity_relationship,'unknown'),evidence_version,
 source_platform,source_url,source_timestamp,observed_at,actor_kind,fetcher_id,privacy_restricted,source_key,1,NULL,1,has_original_media,external_source,intake_channel,generation_method,COALESCE(source_account_key,''),source_key,1,CASE WHEN source_account_key IS NULL THEN NULL ELSE 1 END,0,JSON_ARRAY(),observed_at,source_timestamp FROM normalized`
	queryCtx, cancel := withPublicDiscoveryTimeout(ctx)
	defer cancel()
	rows, err := d.db.QueryContext(queryCtx, query, args...)
	if err != nil {
		return fmt.Errorf("query public detail evidence: %w", err)
	}
	defer rows.Close()
	bySeq := make(map[int]evidenceRecord, len(reports))
	for rows.Next() {
		var r evidenceRecord
		if err := rows.Scan(&r.Seq, &r.PublicID, &r.IngestedAt, &r.Latitude, &r.Longitude, &r.Title, &r.Summary, &r.Classification, &r.Severity, &r.Language, &r.Brand, &r.BrandDisplay,
			&r.Kind, &r.Excerpt, &r.Impact, &r.Reason, &r.Relationship, &r.Version, &r.SourcePlatform, &r.SourceURL, &r.PublishedAt, &r.ObservedAt, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted, &r.SourceKey, &r.ImportCount, &r.CaseID, &r.RelatedCount, &r.HasOriginalMedia, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.GroupKey, &r.SupportingCount, &r.SourceAccountCount, &r.FamilyDuplicates, &r.SupportingSourcesJSON, &r.LatestObservedAt, &r.LatestPublishedAt); err != nil {
			return err
		}
		bySeq[r.Seq] = r
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	mediaSeqs := []int{}
	for _, r := range bySeq {
		if r.HasOriginalMedia && !r.PrivacyRestricted {
			mediaSeqs = append(mediaSeqs, r.Seq)
		}
	}
	valid, err := loadEvidenceImageValidity(queryCtx, d.db, mediaSeqs)
	if err != nil {
		return err
	}
	for _, report := range reports {
		if r, ok := bySeq[report.Report.Seq]; ok {
			ev := r.publicEvidence()
			rec := r.publicRecurrence()
			rec.CountsExact = false
			ev.HasOriginalMedia = ev.HasOriginalMedia && valid[report.Report.Seq]
			report.Evidence = &ev
			report.Recurrence = &rec
			report.Report.SourceURL = ev.SourceURL
			report.Report.SourceTimestamp = ev.SourcePublishedAt
		}
	}
	return nil
}

// Decode only the bounded selected-page image headers, never report blobs across
// the brand corpus. Old 1x1 ingestion placeholders are not evidence previews.
type evidenceQueryer interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func loadEvidenceImageValidity(ctx context.Context, reader evidenceQueryer, seqs []int) (map[int]bool, error) {
	valid := map[int]bool{}
	if len(seqs) == 0 {
		return valid, nil
	}
	placeholders, args := buildIntInClause(seqs)
	query := fmt.Sprintf(`SELECT r.seq, SUBSTRING(r.image,1,65536) FROM reports r LEFT JOIN report_raw rr ON rr.report_seq=r.seq WHERE r.seq IN (%s) AND %s AND NOT EXISTS (SELECT 1 FROM report_status rs WHERE rs.seq=r.seq AND rs.status IS NOT NULL AND rs.status<>'active') AND NOT EXISTS (SELECT 1 FROM reports_owners ro WHERE ro.seq=r.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)`, placeholders, PublicVisibilityWhereSQL)
	rows, err := reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var header []byte
		if err := rows.Scan(&seq, &header); err != nil {
			return nil, err
		}
		valid[seq] = evidence.ValidOriginalImage(header)
	}
	return valid, rows.Err()
}
