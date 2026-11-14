package database

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cleanapp-common/reportoutcome"
	"report-listener/models"
)

const publicSubmissionProjectionLimit = 64 << 10

// This private DTO contains a bounded retained description only to extract literal
// links. The description, agent identity, chain-of-custody and raw payload are
// never serialized in public evidence. None of these claims establish authorship.
type publicSubmissionProvenance struct {
	Client struct {
		Transport  string `json:"transport"`
		Name       string `json:"name"`
		NameSource string `json:"name_source"`
	} `json:"client"`
	Product           string `json:"product"`
	Platform          string `json:"platform"`
	SourceURL         string `json:"source_url"`
	MetadataSourceURL string `json:"metadata_source_url"`
	PublishedAt       string `json:"published_at"`
	ReporterContext   string `json:"reporter_context"`
	Description       string `json:"description"`
	DescriptionStatus string `json:"description_status"`
	Upstream          []struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"upstream"`
	ContainsPII       bool `json:"contains_pii"`
	RequiresRedaction bool `json:"requires_redaction"`
}

func agentProvenanceEligible(r evidenceRecord) bool {
	return !r.PrivacyRestricted && r.ActorKind == "machine" && (r.IntakeChannel == "wire" || r.IntakeChannel == "public_agent") &&
		(r.GenerationMethod == "human_assisted" || r.GenerationMethod == "cleanapp_cli" || r.GenerationMethod == "openclaw_skill" || r.IntakeChannel == "public_agent")
}

// Read only the selected public page/detail, using the same canonical fetcher
// and submitted_at/submission_id tie-break as the analyzer and outcome store.
// The original JSON and consent/side-effect flags are never updated. New fields
// therefore work for supported retained history without a whole-table backfill.
func hydratePublicSubmissionProvenance(ctx context.Context, reader evidenceQueryer, records []evidenceRecord) error {
	indices := map[int][]int{}
	ids := []int{}
	for i, r := range records {
		if !agentProvenanceEligible(r) {
			continue
		}
		if _, exists := indices[r.Seq]; !exists {
			ids = append(ids, r.Seq)
		}
		indices[r.Seq] = append(indices[r.Seq], i)
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 100 {
		return fmt.Errorf("submission provenance batch exceeds 100 reports")
	}
	marks, args := buildIntInClause(ids)
	query := `WITH selected AS (
 SELECT w.submission_id,w.report_seq,
 ROW_NUMBER() OVER(PARTITION BY w.report_seq ORDER BY w.submitted_at DESC,w.submission_id DESC) AS source_rank
 FROM wire_submissions_raw w JOIN report_raw rr ON rr.report_seq=w.report_seq AND rr.fetcher_id=w.fetcher_id
 JOIN reports r ON r.seq=rr.report_seq
 WHERE w.report_seq IN (` + marks + `) AND w.actor_kind='machine' AND w.channel IN ('wire','public_agent')
 AND ` + PublicVisibilityWhereSQL + `
 AND NOT EXISTS(SELECT 1 FROM report_status rs WHERE rs.seq=r.seq AND rs.status IS NOT NULL AND rs.status<>'active')
 AND NOT EXISTS(SELECT 1 FROM reports_owners ro WHERE ro.seq=r.seq AND ro.owner IS NOT NULL AND ro.owner<>'' AND ro.is_public=FALSE)
), projected AS (
 SELECT w.report_seq,JSON_OBJECT(
 'client',JSON_EXTRACT(w.extensions_json,'$.cleanapp_client'),
 'product',JSON_EXTRACT(w.report_json,'$.digital_context.product_or_site'),
 'platform',JSON_EXTRACT(w.report_json,'$.digital_context.platform'),
 'source_url',JSON_EXTRACT(w.report_json,'$.digital_context.source_url'),
 'metadata_source_url',JSON_EXTRACT(w.report_json,'$.digital_context.metadata.source_url'),
 'published_at',JSON_EXTRACT(w.report_json,'$.digital_context.metadata.source_published_at'),
 'reporter_context',JSON_EXTRACT(w.report_json,'$.digital_context.metadata.reporter_context'),
 'description',CASE WHEN OCTET_LENGTH(JSON_UNQUOTE(JSON_EXTRACT(w.report_json,'$.description')))<=16384 THEN JSON_EXTRACT(w.report_json,'$.description') ELSE NULL END,
 'description_status',CASE WHEN OCTET_LENGTH(JSON_UNQUOTE(JSON_EXTRACT(w.report_json,'$.description')))>16384 THEN 'omitted' ELSE 'available' END,
 'upstream',JSON_EXTRACT(w.provenance_json,'$.upstream_sources'),
 'contains_pii',JSON_EXTRACT(w.report_json,'$.privacy.contains_pii'),
 'requires_redaction',JSON_EXTRACT(w.report_json,'$.privacy.requires_redaction')) AS metadata
 FROM selected s JOIN wire_submissions_raw w ON w.submission_id=s.submission_id WHERE s.source_rank=1
) SELECT /*+ MAX_EXECUTION_TIME(3000) */ report_seq,CASE WHEN OCTET_LENGTH(metadata)<=65536 THEN metadata ELSE NULL END FROM projected`
	rows, err := reader.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read selected submission provenance: %w", err)
	}
	defer rows.Close()
	seen := map[int]bool{}
	for rows.Next() {
		var seq int
		var raw []byte
		if err := rows.Scan(&seq, &raw); err != nil {
			return err
		}
		if _, ok := indices[seq]; !ok || seen[seq] {
			return fmt.Errorf("submission provenance identity is outside selected scope or duplicated")
		}
		seen[seq] = true
		var value publicSubmissionProvenance
		if len(raw) == 0 || len(raw) > publicSubmissionProjectionLimit || json.Unmarshal(raw, &value) != nil {
			continue
		}
		if value.ContainsPII || value.RequiresRedaction {
			for _, i := range indices[seq] {
				records[i].PrivacyRestricted = true
			}
			continue
		}
		for _, i := range indices[seq] {
			records[i].SubmissionProvenance = &value
		}
	}
	return rows.Err()
}

var literalSubmissionURL = regexp.MustCompile(`https?://[^\s<>"'\x00-\x1f]+`)
var publicXPostPath = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})/status/([0-9]{1,20})(?:/.*)?$`)

func publicProvenanceURL(raw string) string {
	if hasUnsafeEvidenceControls(raw) || sensitiveEvidence.MatchString(raw) {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || reportoutcome.PrivacyHold(nil, 0, false, false, raw, decoded) != "" {
		return ""
	}
	// Reject secret-bearing URLs before canonicalization drops a query/fragment.
	for _, query := range []string{u.RawQuery, u.Fragment} {
		values, err := url.ParseQuery(query)
		if err != nil {
			return ""
		}
		for key := range values {
			key = strings.ToLower(key)
			if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "signature") || key == "key" || key == "api_key" || key == "password" || key == "auth" || key == "authorization" || key == "code" || key == "state" {
				return ""
			}
		}
	}
	host, path := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), strings.ToLower(u.Path)
	if ((host == "x.com" || host == "twitter.com") && (strings.HasPrefix(path, "/messages") || strings.HasPrefix(path, "/i/grok"))) ||
		(host == "grok.com" && (strings.HasPrefix(path, "/chat/") || strings.HasPrefix(path, "/c/"))) {
		return ""
	}
	return safeEvidenceURL(raw)
}

func publicPostIdentity(raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "x.com" && host != "twitter.com" {
		return "", ""
	}
	m := publicXPostPath.FindStringSubmatch(u.Path)
	if len(m) != 3 {
		return "", ""
	}
	return "twitter", m[2]
}

func xPostURLTime(raw string, now time.Time) *time.Time {
	_, id := publicPostIdentity(raw)
	if len(id) < 16 {
		return nil
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil
	}
	millis := int64(n>>22) + 1288834974657
	t := time.UnixMilli(millis).UTC()
	if t.Before(time.Date(2010, 11, 4, 1, 42, 54, 657000000, time.UTC)) || t.After(now.Add(24*time.Hour)) {
		return nil
	}
	return &t
}

func safeProvenanceText(value string, limit int) *string {
	if hasUnsafeEvidenceControls(value) || sensitiveEvidence.MatchString(value) || reportoutcome.PrivacyHold(nil, 0, false, false, value) != "" {
		return nil
	}
	return evidenceText(value, limit)
}

func applyPublicSubmissionProvenance(ev *models.PublicEvidence, r evidenceRecord, now time.Time) {
	ev.ReferencedURLs = []string{}
	ev.SourceReferences = []models.PublicSourceReference{}
	ev.ObservedAtBasis, ev.SourcePublishedAtBasis = "not_recorded", "not_recorded"
	if ev.ObservedAt != nil {
		ev.ObservedAtBasis = "provided"
	}
	if ev.SourcePublishedAt != nil {
		ev.SourcePublishedAtBasis = "source_metadata"
	}
	if ev.IntakeChannel != nil {
		ev.SubmissionTransport = evidenceText(*ev.IntakeChannel, 32)
	}
	if r.PrivacyRestricted || r.SubmissionProvenance == nil {
		return
	}
	p := r.SubmissionProvenance
	if p.Client.Transport == "mcp" || p.Client.Transport == "web" {
		ev.SubmissionTransport = evidenceText(p.Client.Transport, 8)
		if p.Client.NameSource == "caller_provided" {
			ev.SubmissionClient = safeProvenanceText(p.Client.Name, 80)
			ev.SubmissionClientClaimed = ev.SubmissionClient != nil
		}
	}
	if p.ReporterContext == "firsthand" || p.ReporterContext == "relayed" || p.ReporterContext == "unknown" {
		ev.ReporterContext = evidenceText(p.ReporterContext, 16)
	}
	ev.ReportedProductOrSite = safeProvenanceText(p.Product, 200)
	if !r.ExternalSource && p.Platform != "" {
		ev.SourcePlatform = safeProvenanceText(p.Platform, 64)
	}
	if r.GenerationMethod == "human_assisted" && p.Platform == "" && !r.ExternalSource && r.SourcePlatform == "web" {
		ev.SourcePlatform = nil
	}
	refs := map[string]bool{}
	referencesWithheld := false
	addSource := func(raw, kind string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		safe := publicProvenanceURL(raw)
		if safe == "" {
			referencesWithheld = true
			return
		}
		platform, id := publicPostIdentity(safe)
		key := safe
		if id != "" {
			key = "twitter:" + id
		}
		if refs[key] {
			return
		}
		refs[key] = true
		if len(ev.SourceReferences) < 32 {
			ev.SourceReferences = append(ev.SourceReferences, models.PublicSourceReference{URL: safe, Kind: kind, Platform: platform})
		}
	}
	for _, value := range []string{p.MetadataSourceURL, p.SourceURL} {
		addSource(value, "declared_source")
	}
	for _, ref := range p.Upstream {
		if ref.Kind == "url" {
			addSource(ref.Value, "declared_source")
		}
	}
	seenURLs := map[string]bool{}
	for _, value := range literalSubmissionURL.FindAllString(p.Description, -1) {
		value = strings.TrimRight(value, ".,;:!?]}")
		for strings.HasSuffix(value, ")") && strings.Count(value, ")") > strings.Count(value, "(") {
			value = strings.TrimSuffix(value, ")")
		}
		if platform, _ := publicPostIdentity(value); platform != "" {
			addSource(value, "linked_post")
		}
		safe := publicProvenanceURL(value)
		if safe == "" || seenURLs[safe] {
			continue
		}
		seenURLs[safe] = true
		if len(ev.ReferencedURLs) < 32 {
			ev.ReferencedURLs = append(ev.ReferencedURLs, safe)
		}
	}
	count := len(refs)
	if p.DescriptionStatus != "omitted" && !referencesWithheld {
		ev.SourceReferenceCount = &count
	}
	if p.PublishedAt != "" {
		if date, err := time.Parse(time.RFC3339Nano, p.PublishedAt); err == nil && !date.IsZero() && (ev.SourcePublishedAt == nil || ev.SourcePublishedAt.Equal(date)) {
			date = date.UTC()
			ev.SourcePublishedAt = &date
			ev.SourcePublishedAtBasis = "provided"
		}
	} else if ev.SourcePublishedAt == nil && count == 1 && p.DescriptionStatus != "omitted" && !referencesWithheld {
		if date := xPostURLTime(ev.SourceReferences[0].URL, now); date != nil {
			ev.SourcePublishedAt = date
			ev.SourcePublishedAtBasis = "derived_x_status_id"
		}
	}
}
