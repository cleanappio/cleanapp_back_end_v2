package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Dates are nullable DATE_FORMAT('%Y-%m-%d %H:%i:%s.%f') values from the same
// reader snapshot. RawMessage preserves JSON null/objects/numbers independently
// from absent SQL NULL values; this DTO contains source metadata, never bodies.
type evidenceSourceNormalizationRow struct {
	Seq            int             `json:"seq"`
	ReportID       string          `json:"report_id"`
	ReportTS       *string         `json:"report_ts"`
	RawSourceType  *string         `json:"raw_source_type"`
	RawSourceID    *string         `json:"raw_source_id"`
	FetcherID      string          `json:"fetcher_id"`
	EiiSource      *string         `json:"eii_source"`
	EiiID          *string         `json:"eii_id"`
	EiiURL         *string         `json:"eii_url"`
	EiiPublished   *string         `json:"eii_published"`
	EiiCreated     *string         `json:"eii_created"`
	WireObserved   *string         `json:"wire_observed"`
	WireSubmitted  *string         `json:"wire_submitted"`
	WireActor      string          `json:"wire_actor"`
	WireChannel    string          `json:"wire_channel"`
	WireReport     json.RawMessage `json:"wire_report"`
	WireProvenance json.RawMessage `json:"wire_provenance"`
	WireExtensions json.RawMessage `json:"wire_extensions"`
}

// normalizeEvidenceSources keeps the existing MySQL source identity/date rules
// authoritative while avoiding legacy-table joins and windowed JSON payloads.
// Batches only bound the parameter size; every statement uses the caller's same
// transaction snapshot and source-ranked, visibility-checked inputs.
func normalizeEvidenceSourcesSQL(ctx context.Context, tx *sql.Tx, inputs []evidenceSourceNormalizationRow) (map[int]evidenceRecord, error) {
	output := make(map[int]evidenceRecord, len(inputs))
	for start := 0; start < len(inputs); {
		end := min(start+1000, len(inputs))
		payload, err := evidenceSourceNormalizationPayload(inputs[start:end])
		if err != nil {
			return nil, fmt.Errorf("encode compact source metadata: %w", err)
		}
		for len(payload) > 8<<20 && end-start > 1 {
			end = start + (end-start)/2
			payload, err = evidenceSourceNormalizationPayload(inputs[start:end])
			if err != nil {
				return nil, fmt.Errorf("encode compact source metadata: %w", err)
			}
		}
		if len(payload) > 8<<20 {
			return nil, fmt.Errorf("one compact source metadata row exceeds 8 MiB")
		}
		rows, err := tx.QueryContext(ctx, evidenceSourceNormalizationSQL(), payload)
		if err != nil {
			return nil, fmt.Errorf("normalize compact source metadata: %w", err)
		}
		for rows.Next() {
			var r evidenceRecord
			var ingested, observed, published sql.NullTime
			if err := rows.Scan(&r.Seq, &r.SourcePlatform, &r.SourceURL, &published, &observed, &ingested,
				&r.IngestionTimeMissing, &r.ActorKind, &r.FetcherID, &r.PrivacyRestricted,
				&r.SourceKey, &r.ExternalSource, &r.IntakeChannel, &r.GenerationMethod, &r.SourceAccountKey, &r.IngestedAtNull); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan normalized compact source metadata: %w", err)
			}
			if published.Valid {
				r.PublishedAt = &published.Time
			}
			if observed.Valid {
				r.ObservedAt = &observed.Time
			}
			if ingested.Valid {
				r.IngestedAt = ingested.Time
			}
			if _, duplicate := output[r.Seq]; duplicate {
				rows.Close()
				return nil, fmt.Errorf("duplicate compact source sequence")
			}
			output[r.Seq] = r
		}
		iterationErr := rows.Err()
		rows.Close()
		if iterationErr != nil {
			return nil, fmt.Errorf("read normalized compact source metadata: %w", iterationErr)
		}
		if len(output) != end {
			return nil, fmt.Errorf("compact source normalization returned an incomplete batch")
		}
		start = end
	}
	return output, nil
}

func evidenceSourceNormalizationPayload(inputs []evidenceSourceNormalizationRow) ([]byte, error) {
	rows := append([]evidenceSourceNormalizationRow(nil), inputs...)
	// mysql.ParseTime represents a stored all-zero SQL date as time.Time{}.
	// JSON_TABLE rejects that SQL zero date under NO_ZERO_DATE, so carry its
	// driver's identical year-one value through normalization. It remains
	// non-NULL for COALESCE/equality and scans back to the same zero time; real
	// stored MySQL dates start at year 1000 and cannot collide with this value.
	zeroTime := "0001-01-01 00:00:00.000000"
	for i := range rows {
		for _, date := range []**string{&rows[i].ReportTS, &rows[i].EiiPublished, &rows[i].EiiCreated, &rows[i].WireObserved, &rows[i].WireSubmitted} {
			if *date != nil && (**date == "0000-00-00 00:00:00.000000" || **date == "0000-00-00 00:00:00") {
				*date = &zeroTime
			}
		}
	}
	return json.Marshal(rows)
}

func evidenceSourceNormalizationSQL() string {
	aliases := strings.NewReplacer("r.id", "s.report_id", "eii.source_url", "s.eii_url", "eii.source", "s.eii_source", "eii.external_id", "s.eii_id", "ws.report_json", "s.wire_report")
	trusted := aliases.Replace(trustedRedditArchiveSQL)
	sourceURL := aliases.Replace(evidenceSourceURLSQL)
	return fmt.Sprintf(`WITH source_rows AS (
 SELECT * FROM JSON_TABLE(?, '$[*]' COLUMNS(
 seq INT PATH '$.seq' ERROR ON EMPTY ERROR ON ERROR,
 report_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.report_id',
 report_ts DATETIME(6) PATH '$.report_ts',
 raw_source_type VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.raw_source_type',
 raw_source_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.raw_source_id',
 fetcher_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.fetcher_id',
 eii_source VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.eii_source',
 eii_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.eii_id',
 eii_url VARCHAR(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci PATH '$.eii_url',
 eii_published DATETIME(6) PATH '$.eii_published',eii_created DATETIME(6) PATH '$.eii_created',
 wire_observed DATETIME(6) PATH '$.wire_observed',wire_submitted DATETIME(6) PATH '$.wire_submitted',
 wire_actor VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.wire_actor',
 wire_channel VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci PATH '$.wire_channel',
 wire_report JSON PATH '$.wire_report',wire_provenance JSON PATH '$.wire_provenance',wire_extensions JSON PATH '$.wire_extensions'
 )) AS source_table
), candidates AS (
 SELECT s.seq,CASE WHEN %[1]s AND s.eii_created IS NOT NULL THEN s.eii_created ELSE s.report_ts END AS ts,
 COALESCE((%[1]s AND s.eii_created IS NULL),FALSE) AS ingestion_time_missing,
 COALESCE(NULLIF(s.eii_source,''),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.platform')),''),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.legacy_source')),''),s.raw_source_type,'') COLLATE utf8mb4_bin AS source_platform,
 CASE WHEN LOWER(LEFT(TRIM(%[2]s),8)) REGEXP '^https?://' THEN %[2]s ELSE '' END COLLATE utf8mb4_bin AS source_url,
 COALESCE(s.eii_published,CASE WHEN JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.source_published_at')) REGEXP '(Z|[+]00:00)$' THEN STR_TO_DATE(REPLACE(LEFT(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.source_published_at')),19),'T',' '),'%%Y-%%m-%%d %%H:%%i:%%s') ELSE NULL END,
 CASE WHEN JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.source_published_at') IS NULL AND JSON_UNQUOTE(JSON_EXTRACT(s.wire_provenance,'$.generation_method')) IN ('twitter_submitter','github_issue_submitter','bluesky_submitter','human_authored_public') THEN s.wire_observed ELSE NULL END,CASE WHEN %[1]s THEN s.report_ts ELSE NULL END) AS source_timestamp,
 s.wire_observed AS raw_observed_at,s.wire_submitted AS submitted_at,
 (COALESCE(JSON_EXTRACT(s.wire_extensions,'$.observation_time_supplied'),FALSE)=TRUE) AS observation_time_supplied,
 (JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.source_published_at') IS NOT NULL) AS explicit_source_publication,
 COALESCE(s.wire_actor,'') AS actor_kind,COALESCE(s.wire_channel,'') AS intake_channel,
 COALESCE(JSON_UNQUOTE(JSON_EXTRACT(s.wire_provenance,'$.generation_method')),'') AS generation_method,
 (s.eii_source IS NOT NULL) AS indexed_source,COALESCE(s.fetcher_id,'') AS fetcher_id,
 COALESCE(s.eii_id,s.raw_source_id,'') COLLATE utf8mb4_bin AS original_id,
 COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.author_did')),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.author_username')),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.metadata.author_handle')),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(s.wire_report,'$.digital_context.author_handle')),'null')) AS source_author,
 CASE WHEN JSON_EXTRACT(s.wire_report,'$.privacy.contains_pii')=TRUE OR JSON_EXTRACT(s.wire_report,'$.privacy.requires_redaction')=TRUE THEN 1 ELSE 0 END AS privacy_restricted
 FROM source_rows s
), provenance AS (
 SELECT /*+ NO_MERGE(candidates) */ candidates.*,(indexed_source OR source_platform IN ('twitter','x','bluesky','github','github_issue','reddit') OR generation_method='legacy_v3_bulk_ingest') AS external_source,
 CASE WHEN source_url REGEXP '^https?://' THEN CONCAT(LOWER(REGEXP_REPLACE(SUBSTRING_INDEX(SUBSTRING_INDEX(TRIM(source_url),'://',-1),'/',1),'^www[.]','')),
 REGEXP_REPLACE(CASE WHEN source_url REGEXP '^https?://(www[.])?(github[.]com|x[.]com|twitter[.]com|reddit[.]com|bsky[.]app)/' THEN TRIM(TRAILING '/' FROM SUBSTRING_INDEX(SUBSTRING_INDEX(TRIM(source_url),'#',1),'?',1)) ELSE SUBSTRING_INDEX(TRIM(source_url),'#',1) END,'^https?://[^/]+','')) ELSE '' END AS canonical_url
 FROM candidates
), normalized AS (
 SELECT /*+ NO_MERGE(provenance) */ provenance.*,
 CASE WHEN source_platform='bluesky' AND original_id REGEXP '^at://' THEN CONCAT('source:bluesky:',original_id)
 WHEN canonical_url REGEXP '^(twitter[.]com|x[.]com)/[^/]+/status/[0-9]+' THEN CONCAT('source:twitter:',SUBSTRING_INDEX(SUBSTRING_INDEX(canonical_url,'/status/',-1),'/',1))
 WHEN canonical_url<>'' THEN CONCAT('url:',canonical_url)
 WHEN original_id<>'' AND source_platform IN ('twitter','x','bluesky','github','github_issue','reddit') THEN CONCAT('source:',CASE WHEN source_platform='x' THEN 'twitter' WHEN source_platform='github_issue' THEN 'github' ELSE source_platform END,':',REGEXP_REPLACE(original_id,'^(twitter|x|github_issue|github|reddit|bluesky):',''))
 WHEN original_id<>'' THEN CONCAT('source:',source_platform,':',fetcher_id,':',original_id) ELSE CONCAT('report:',seq) END AS source_key,
 CASE WHEN generation_method='legacy_v3_bulk_ingest' OR (generation_method='human_submission' AND raw_observed_at=submitted_at AND NOT observation_time_supplied) OR (generation_method='shared_submission' AND NOT observation_time_supplied) OR (generation_method IN ('twitter_submitter','github_issue_submitter','bluesky_submitter','human_authored_public') AND NOT explicit_source_publication) THEN NULL ELSE raw_observed_at END AS observed_at,
 CASE WHEN COALESCE(source_author,'')<>'' THEN CONCAT(CASE WHEN source_platform='github_issue' THEN 'github' WHEN source_platform='x' THEN 'twitter' ELSE source_platform END,':',LOWER(source_author)) ELSE NULL END AS source_account_key
 FROM provenance
)
SELECT /*+ NO_MERGE(normalized) */ seq,source_platform,source_url,source_timestamp,observed_at,ts,ingestion_time_missing,actor_kind,fetcher_id,privacy_restricted,source_key,external_source,intake_channel,generation_method,COALESCE(source_account_key,''),ts IS NULL FROM normalized`, trusted, sourceURL)
}
