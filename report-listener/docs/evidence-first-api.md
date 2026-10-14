# Evidence first brand briefings

`GET /api/v3/public/discovery/by-brand` (and the v4 Go route) retains the opaque discovery token card contract and adds evidence and related issue family metadata. `GET /reports/by-brand` uses the same selection for both lite and full responses. Report detail endpoints add the same evidence object. Global, search, and physical nearby feeds keep their existing defaults.

## Request

- `brand_name` is required. Known company/product relationships use an explicit exact-match alias map. Anthropic includes Claude and Claude Code; Claude includes Claude Code; OpenAI includes ChatGPT, Codex, API, SDKs, Whisper and DALL-E; xAI includes Grok. Other subjects match exactly. Product names never come from substring matching.
- `kind=firsthand` is the default and excludes coverage and unknown. `coverage`, `unknown`, and `all` remain accessible. `content_kind` is a compatibility alias.
- `sort=signal` is the default: available original evidence and reported impact, then entity relevance (`affected`, `dependency`, `mentioned`/`unknown`), distinct supporting-source counts, existing severity, and recency. Evidence completeness and relevance take precedence over family size, so a strong singleton can outrank a weak large family. Related sources do not prove independence. `recent` orders families by their latest provided observation, otherwise source publication, otherwise ingestion when both are unknown. `recurrence` orders related source observations, without asserting independence.
- `product` narrows the explicit company scope; foreign products are rejected.
- `n` defaults to 12 and is capped at 50. `offset` defaults to zero. Every order ends in descending report sequence for stable ties.

## Response semantics

`items` contains one source observation or one existing related issue family, with an opaque discovery token for its representative report. Original source URLs, excerpts, publication/observation/ingestion dates, reported impact, entity relationship, and a concrete `why_surfaced` explanation are in `evidence`. Labels distinguish actual CleanApp intake from collected public material; a human-authored imported post remains imported.

`recurrence.grouping` is `source_observation` or `related_issue_family`. Existing case membership means contextual relatedness; it does not establish identical defects, affected-user counts, independent corroboration, or responsibility. `supporting_report_count` counts distinct source observations in the selected kind/product scope. Reimports and translated analyses do not increase it. `source_account_count` is null unless every supporting observation carries a deterministic source account identifier; these are accounts, not people. `supporting_sources` contains at most eight original-source links. Account identifiers and raw reporter identities are not exposed.

`total_count` and `kind_counts` count briefing entries after source and family grouping. `kind_counts` covers the same company/product/public universe before applying `kind`. An existing family can appear in multiple kind views, so the sum of individual kind counts can exceed `all`. Source keys normalize known public provider URLs and original IDs, including X/Twitter status aliases. Opaque issue IDs remain stable for a given family or source key.

Counts and page selection use one bounded read-only repeatable-read transaction and the same SQL universe. The legacy unfiltered brand count cache is bypassed. On query failure the endpoint returns 503 instead of substituting whole-corpus, stale, or partial counts as exact. Public, owner, and report-status rules match the permitted detail surface.

A source's publication date is separate from a provided observation date and ingestion. Known historical indexer fields that stored publication in `observed_at` are corrected in the read projection; raw data is retained. Genuine provided observations remain available. Native human intake now records whether observation time was supplied, distinguishing it from the old submission-time fallback.

No status is inferred from GitHub closure or CleanApp case state: validation remains `not_independently_reproduced`, and upstream and resolution status are `unknown` until deterministic, documented status evidence is integrated. Quotes are bounded literal source excerpts retained by versioned enrichment; privacy flags, obvious credentials, personal email addresses, and unsafe control characters suppress their public display. Source links reject credentials, sensitive query parameters, unsafe schemes, and local/private IP targets. Image previews require actual valid image headers; old 1x1 ingestion placeholders are not evidence.

## Migration and validation

The explicit listener migration `0024_report_evidence` calls the shared idempotent evidence schema migration. Existing rows become unknown without reclassifying or removing them. Apply migrations before enabling this read contract; original-only enrichment/backfill is separately controlled and should be reviewed before rollout.

`go test ./...` runs request, source safety, temporal, count/paging/snapshot, family, and public metadata tests. The opt-in real MySQL fixture requires `EVIDENCE_TEST_DSN` pointing to a dedicated local TCP database ending `_evidence_test`; it refuses other destinations and existing fixture tables. CI provisions an ephemeral MySQL 8.4 service for this test. The fixture covers translations, X/Twitter duplicate imports, known/unknown/coverage, privacy/quarantine/status visibility, scoped product counts, historical versus genuinely provided observation dates, strong singletons over weak families, and relevance-aware representative selection.
