# Implement Evidence first report pages in CleanApp

This prompt replaces `codex_prompt_firsthand_vs_coverage.md`. Implement the backend and production frontend together. The goal is that an executive opening a property, company, or product page can see within ten seconds what failed, where the evidence originated, whether observations recur, and why the issue deserves attention. Preserve useful single observations and historical evidence.

## Repositories and working conventions

- Backend: `cleanappio/cleanapp_back_end_v2`, Go and Rust services with MySQL.
- Frontend: `cleanappio/cleanapp-frontend`, Next.js and TypeScript.
- Read applicable `AGENTS.md`, `CLAUDE.md`, backend `INVARIANTS.md`, and `ARCHITECTURE.md` before editing. Inspect current code rather than assuming the old proposal describes the deployed architecture.
- State implementation assumptions and a short plan; use isolated branches from current main and preserve unrelated local edits. Keep changes scoped to evidence enrichment, ingestion provenance, public presentation, and their tests.
- Follow current explicit migration entrypoints (`cmd/migrate` and the repository migration wrapper). Do not restore startup schema mutation or manually alter production tables.
- Write meaningful tests before implementing classification normalization, provenance resolution, filtering, source deduplication, pagination/counts, and UI behavior.
- Implement code and produce reviewable commits/PRs. Production migration, backfill writes, and deployment are separate release actions. Do not imply local implementation changes the live website.

## Product rules

1. Experience kind, intake provenance, and verification are independent facts. A public firsthand account collected by a bot is useful evidence; it is not a report filed through CleanApp. A direct submission can be coverage.
2. AI assists with extraction and classification. It cannot establish incident truth, source independence, customer identity, a reproduced defect, or a confirmed fix.
3. Classify original author material, not an AI-generated title, translation, summary, or report description that an indexer generated. If original material is unavailable, keep `unknown` with an explanation.
4. Keep all raw reports, original text/media, source metadata, timestamps, and coverage accessible. Presentation filters must not reduce intake recall or alter public visibility/quarantine rules.
5. A reproducible singleton can lead a page. Do not require recurrence before treating a specific observation as valuable.
6. A company mention does not establish that its product failed or that it owns the reported issue. Distinguish affected entity, integration dependency, coverage subject, and unrelated comparison.

## Backend: canonical evidence enrichment

Add additive evidence fields to `report_analysis` through explicit service migrations, with shared definitions and defensive normalization:

- `report_kind`: `firsthand`, `coverage`, or `unknown`; default unknown.
- `evidence_excerpt`: short literal excerpt from retained original text. Validate that it is an exact substring; reject fabricated, paraphrased, or oversized quotations.
- `reported_impact`: bounded description of the consequence the source reports, with no invented financial/user totals or inferred causation.
- `classification_reason`: bounded explanation grounded in original evidence.
- `entity_relationship`: `affected`, `dependency`, `mentioned`, or `unknown`.
- `evidence_version`: version of this enrichment contract; legacy rows default to zero.

Use consistent fields in pipeline models, inserts, updates, API models, and any translation copies. Keep Gemini/OpenAI evidence instructions and response schemas synchronized, preferably through a shared definition rather than duplicated prose.

Classification definitions:

- **Firsthand:** the original author describes personally encountering or observing a problem, including customer friction, a witnessed physical problem, a developer's own failure, or an original test. Concrete steps, exact error text, environment details, and the author's own screenshots strengthen evidence but are not prerequisites for every valid account.
- **Coverage:** relayed news, commentary, reposts, press releases, status/news syndication, or someone else's incident without the author's own observation. Seriousness does not convert coverage into firsthand experience.
- **Unknown:** the available original evidence cannot support a confident choice, or the original is unavailable.

Treat model output as untrusted extraction. Invalid enums become unknown. Unsafe/private excerpts are withheld from public presentation. Do not allow a client or model to assign independent verification, source-account independence, or direct human submission status.

Build a reusable original-evidence resolver. Inspect `external_ingest_index`, raw intake provenance, retained Wire submission JSON, `digital_share_reports`, and source indexer tables. Resolve GitHub issue bodies, X post text, and Bluesky post text using deterministic source identifiers. An imported summary in `reports.description` is not an acceptable fallback. Reuse the resolver for normal analysis, backfill, and grounded public metadata; keep read query cost bounded.

## Backend: retain source provenance at intake

Keep ingesting coverage. Do not change relevance thresholds or remove submissions.

For X, Bluesky, and GitHub submitters, preserve actual original body/text alongside the generated summary in existing normalized intake metadata. Use these common metadata keys:

- `original_text`
- `original_text_truncated` when the retained snapshot is bounded; resolve full text from the source table instead of classifying a cut-off snapshot.
- `source_url`
- `source_published_at`
- `source_updated_at`, where actually known

Preserve deterministic source IDs and platform/account metadata internally. Do not expose reporter identifiers unnecessarily. Retain provenance through both Wire and supported legacy ingestion paths. Use the canonical enrichment service as the authority for experience classification; avoid a competing unsynchronized intake classifier.

Set submission time to actual submission time. A post's publication time is not automatically the time its incident occurred. Set `observed_at` only when supplied as an actual observation time; leave it absent when unknown. Never manufacture incident recency from ingestion timestamps. Existing source-capture timestamps may be retained with their actual meaning.

## Backend: public report and brand contract

Add optional `evidence` and `recurrence` objects to full/lite reports, public discovery cards, and relevant report detail responses. Keep public card tokens and identity/access protections intact. Use the same contract and selection semantics across the active v3/v4 paths, including the Rust read service if it serves an independently routed brand endpoint.

`evidence` should expose these facts when supported:

- `report_kind`, `intake_method`, `intake_label`, `source_platform`, safe original `source_url`.
- Separate nullable `source_published_at`, `observed_at`, and actual `ingested_at`.
- Original `excerpt`, `reported_impact`, `classification_reason`, `entity_relationship`, and `evidence_version`.
- `has_original_media` only for valid retained original images; placeholder media is not evidence. Image-only observations must not receive fabricated quotations.
- Explicit `validation_status`; default `not_independently_reproduced`.
- Separate `upstream_status` and `resolution_status`; missing evidence is unknown. A source marked closed is not a demonstrated fix. Status snapshots must not imply current validation.
- `why_surfaced`: short, inspectable reasons such as original evidence available, specific reported impact, or supported additional observations. Do not claim novelty or additional corroboration merely because an existing upstream issue was imported.

`recurrence` should expose:

- Stable `issue_id` and nullable existing `case_id`.
- Deduplicated `supporting_report_count`, nullable `source_account_count`, and `duplicate_import_count`.
- `independence: unassessed` unless independence has actually been established.
- Separately labeled `related_observation_count` for existing cases/issue families whose membership does not establish the same defect.
- `counts_exact`, and source/observation dates where known.

Collapse repeated imports/reposts of the same original source using canonical source IDs/URLs. Count translations once. Do not convert comments, likes, copies of a story, multiple imports, or raw case membership into affected customers. Distinct accounts do not automatically mean independently verified people. Do not merge separate failure modes solely because they share a broad topic. Use existing saved cases for related observations with honest labels; retain distinct issues when the evidence supports different symptoms.

Provide a shared brand briefing selection implementation with:

- `kind=firsthand` by default, strictly excluding coverage and unknown.
- Accessible `kind=coverage`, `kind=unknown`, and `kind=all`.
- `sort=signal` by default; explicit `sort=recent` and `sort=recurrence`.
- Bounded limit/offset or existing cursor semantics, deterministic tie breakers, `has_more`, and scoped counts using the same filters and deduplication as the visible list.
- `kind_counts`, `counts_exact`, and scope metadata for company/product filters. Include all selection dimensions in cache keys.
- Evidence/recurrence enrichment in a bounded number of queries per page; no per-report query loop.

Signal ordering should use inspectable evidence completeness and concrete reported impact, entity relevance, deduplicated related-source counts as a tie-break, reported severity, and genuine observation/source recency. Related sources are not proven independent corroboration. A strong singleton must rank above a large weak cluster. Avoid a new opaque percentage that substitutes for evidence.

Add explicit company/product mappings for Anthropic/Claude/Claude Code and OpenAI's relevant products/SDKs, with scoped product filters and deduplication across aliases. Use exact supported relationships, not keyword substring joins. Preserve separate labels for integration dependencies. Physical/property pages should use the same evidence principles while retaining useful location context.

## Frontend: Evidence first production design

Use the Evidence first variant of the reviewed concept as the hierarchy reference. The later request explicitly selects Evidence first. The artifact is a reference, not production content: do not ship its historical demonstration record, fabricated metrics, design controls, or concept annotations.

Visual thesis: CleanApp's restrained green identity, strong product typography, and original evidence provide the visual anchor. Content plan: brand/company scope, visible issue evidence, inspectable source details, then questions about the evidence. Interaction thesis: accessible evidence disclosure, filter/sort transitions, and restrained feedback with reduced-motion support.

On a cold brand page, put these above the AI chat panel:

1. Correct company/product name and concise scope copy.
2. Explicit experience-kind tabs and product scope controls.
3. A small set of specific issues with reported impact, original excerpt or evidence preview, intake/source labels, dates, and validation status.
4. Supporting observations with honest recurrence labels and a short why-surfaced explanation.

Each item must distinguish original author wording from a CleanApp summary. Provide a safe original-source link and an accessible Inspect evidence disclosure containing supported details, source dates versus ingestion dates, source/upstream status, and reproduction information only when actually available. Missing metadata must remain unknown; never manufacture a source, observation date, step list, or verification badge.

Move Live CleanApp Intelligence below the evidence. Questions and generated answers must use the same scope/kind semantics as the displayed briefing; if broader scope is used, label it explicitly. Preserve legitimate subscriber workflows and protected details. Evidence should be useful without requiring a chat interaction or login.

Replace unexplained severity percentages with concrete reported impact and evidence status. Remove generic Locations/Monitoring Zone/24-7-operational filler from digital brand presentation where it has no measured basis; keep relevant physical location context. Do not delete unrelated shared components merely because their brand-page use changes.

Support sparse datasets honestly: an empty firsthand view can show a clear message and counts/links to unknown or coverage. It must not silently broaden to unknown and keep the firsthand heading. Distinguish empty, loading, failed, and partially enriched states.

Use existing components and design tokens where useful, minimal chrome, readable original excerpts, visible focus, keyboard operation, safe external links, and responsive layouts down to 320px. Avoid introducing a promotional hero or decorative dashboard metrics.

## Backfill, validation, and release preparation

Extend `report-analysis-backfill` with evidence-only enrichment using the same production original resolver and classifier. Support bounded brand selections, existing rate limits, restart safety, and:

```
--dry-run --brand grok --limit 50
--brands grok,anthropic,claude,openai
```

Dry run writes nothing: no analysis updates, audit-table mutations, migrations, or progress checkpoints. Print report identifier, proposed kind, original-source availability, and title. Print **label counts**, not a mislabeled precision summary. Compare a representative source-reviewed sample against the classifier to measure precision separately. Include quotes/reposts, syndication, ambiguous material, direct reports, historical closed issues, genuine singletons, and third-party integration complaints.

Tests must cover:

- Enum normalization and missing original evidence.
- Literal excerpt validation and public privacy/source-link restrictions.
- Original versus generated descriptions, and retained Wire/source-table resolution.
- Submission/publication/occurrence timestamps with unknown occurrence retained.
- Strict kind filters, matching counts/cache dimensions, company/product scope, deterministic paging/sorts, translations/source deduplication, and unknown independence.
- Upstream closure without proven resolution.
- Dry-run zero writes and clearly named label-count output.
- UI provenance/status labels, unknown empty state, source links, disclosure accessibility, and evidence before chat.

Run affected Go/Rust tests and frontend type/build checks. Inspect production components locally at desktop and 320px mobile; capture representative screenshots. If private production access is unavailable, use explicit synthetic fixtures for local integration checks and label them as such. Never present local fixtures as real corpus statistics or claim a production backfill ran.

Deliver the revised prompt, reviewable implementation across both repositories, linked PRs or patches, test results, release commands, and remaining operational prerequisites. Keep the final description organized around the actual shipped behavior, with CHANGES MADE / THINGS I DIDN'T TOUCH / POTENTIAL CONCERNS where repository conventions require it.

Acceptance: within ten seconds, a visitor can identify a concrete problem, inspect its source, distinguish direct submission from imported experience and coverage, understand its supported recurrence and age, and see what CleanApp adds. No top item obtains credibility through an unexplained score, misleading timestamp, or invented verification.
