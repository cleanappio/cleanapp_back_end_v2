# Identity and clustering

**In short.** `THEORY.md` calls the cluster "the fundamental unit of systemic
meaning". The code has thirteen separate mechanisms that put reports together.
None of them decides that two reports describe the same problem by meaning or
by picture in the main pipeline. The public "recurrence" count mostly removes
repeat imports of one source link. The only real grouping is the case, which a
person builds by hand. Four stores of grouping results exist and none feeds
another, except cases feeding recurrence.

Last reviewed: 2026-11-17, code at `bc799bd`. Status words and source tags
are defined in the [README](README.md).

## What "the same" has meant so far

There are four different questions hiding under "cluster", and the code
answers them in different places:

| Question | Where it is answered | How |
| --- | --- | --- |
| Is this the same *source item* imported twice? | evidence source identity (1), ingest idempotency (8), import hashes (9) | exact keys and hashes |
| Is this the same *place and object*, seen again? | photo matching (6) | a vision model compares two photos |
| Is this the same *issue*, reported by someone else? | cluster analysis (4), agent matching (7), cases (3) | word overlap, distance, a person's judgement |
| Is this about the same *entity*? | brand normalisation (10), ownership (10), tags (11) | string rules and alias lists |

## Inventory

Status words: **live** (runs in production), **on demand** (runs only when a
person asks), **write-only** (stored, nothing reads it), **offline** (a tool,
not a service), **plan** (document only).

### 1. Evidence source identity (repeat imports) — live

- **Rule.** Each report gets a `source_key`, chosen in this order: Bluesky
  `at://` URI; tweet status ID; `url:` plus a canonical URL; `source:<platform>:<id>`;
  `source:<platform>:<fetcher>:<id>`; `report:<seq>`. Rows sharing a key
  collapse to one. `import_count` is the size of the group.
- **Where.** `report-listener/database/evidence.go:200-268`, mirrored in
  `evidence_source_normalization.go` and `evidence_grouping.go:45`. Computed
  per request; nothing is stored.
- **Since.** `1f6543b9`, 2026-10-14.
- **Limit.** The same words posted under two links are two sources.

### 2. Recurrence block (`issue_id`) — live

- **Rule.** `group_key` is `family:<lowest case id>` when the report is in a
  case, otherwise its `source_key`. `issue_id` is a hash of that key.
  `supporting_report_count` counts unique sources in the group.
  `independence` is always the literal `"unassessed"`.
- **Where.** `evidence.go:267-272, 580-620`; shown on brand pages, discovery
  cards and report pages.
- **Since.** `1f6543b9`, 2026-10-14.
- **Limits.**
  - Outside a case, an `issue_id` identifies one source item, not an issue.
  - On a report's own page the count is fixed at 1 and no case is linked
    (`AttachPublicEvidence`).
  - `[live 2026-11-17]`: one Bluesky post about a Claude Code error appears as
    six firsthand reports with six different `issue_id`s, each with
    `supporting_report_count: 1` and `duplicate_import_count: 0`. Four copies
    of one billing complaint behave the same way. The six came through
    different post links to the same article, so rule 1 did not join them.

### 3. Cases — live, built by hand

- **Rule.** A signed-in person creates a case and adds reports. Cases can be
  merged. `saved_clusters.cluster_fingerprint` (a hash of classification,
  geometry and summary) is stored and never used for lookup.
- **Where.** `report-listener/database/cases.go`, `case_accumulation.go`;
  tables `cases`, `case_reports`, `saved_clusters`, `case_clusters`.
- **Since.** `aedafe27`, 2026-03-10.
- **Scale.** 31 cases covering 760 reports on 2026-11-11
  (`docs/architecture/notification-outcomes.md`).
- **Limit.** "New reports are not yet auto-matched into open cases"
  (`docs/cleanapp-cases.md:152`). The notification worker does not consult
  cases.

### 4. Cluster analysis (incident hypotheses) — on demand

- **Rule.** Within a drawn area (up to 250 reports) or 0.25 km of a seed
  report, every pair is scored: same classification +0.2; severity within 0.2
  +0.1; word-set overlap above 0.15 +0.4; closer than 100 m +0.2; same brand
  +0.1. Pairs at 0.55 or more are joined (union-find).
- **Where.** `report-listener/handlers/cases.go:918-1043`.
- **Since.** `aedafe27`, 2026-03-10.
- **Limit.** The result is returned to the screen and not stored. The weights
  and the 0.55 threshold have no calibration record.

### 5. Matching a cluster to existing cases — on demand

- **Rule.** Up to 12 open cases of the same classification whose bounding box
  overlaps the cluster. Score: base 0.12; shared reports up to +0.56; box
  overlap up to +0.22; centre distance within 60/150/300 m +0.18/+0.12/+0.06;
  title similarity up to +0.16. Auto-preferred at 0.58.
- **Where.** `cases.go:810-915`, `case_accumulation.go:44-101`.
- **Since.** `da03301a`, 2026-03-11.
- **Limit.** Runs only when a person is creating a case.

### 6. Photo matching (`report_clusters`) — write-only

- **Rule.** When the mobile app submits a photo, reports within 10 m
  (`REPORTS_RADIUS_METERS`) are compared pairwise by a vision model: "is this
  the same place… was the litter or hazard removed". If the old one was
  removed, it is marked resolved. Otherwise, at `same_place_probability` 0.7
  or more, a row goes into `report_clusters(primary_seq, related_seq)`.
- **Where.** `report-processor/handlers/handlers.go:325-381`,
  `report-processor/openai/client.go:103-117`.
- **Since.** `bde0b5a3`, 2025-09-04.
- **Scale.** 4,628 rows on 2026-11-11.
- **Limit.** No code outside `report-processor` reads the table. This is the
  only visual matching in the system, and its output goes nowhere.

### 7. Agent submission matching (`report_outcomes`) — live, agents only

- **Rule.** A context key from classification, brand or site, and (physical)
  location to four decimals. Among the last 25 public outcomes in the same
  context: identical normalised words is a "matching observation"; three or
  more shared words and Dice 0.65 or more is a "similar observation".
- **Where.** `go-common/reportoutcome/matching.go:92-200`.
- **Since.** `54c3d58a`, 2026-11-14.
- **Limit.** Tells the submitting agent only. Not joined to cases or
  recurrence.

### 8. Ingest idempotency — live

- **Rule.** `UNIQUE(fetcher_id, source_id)`; the legacy path uses
  `external_ingest_index(source, external_id)`.
- **Limit.** Per fetcher. The same post arriving through two fetchers lands
  twice.

### 9. Import hashes — live at import time

- **DataUnion import** (`8931f82a`, 2026-11-07): byte-exact SHA-256 of the
  photo.
- **SolveSF import** (`38038c5c`, 2026-11-16): the city's own case ID.
  "Native case identity is not a photo hash."

### 10. Brand and entity normalisation — live

- `NormalizeBrandName`: lower case, strip punctuation, collapse spaces, plus
  eight product aliases (`report-analyze-pipeline/services/brand_service.go`,
  `e3e7e57d` 2025-07-17, `f1629631` 2026-10-15).
- Company alias maps for Anthropic, OpenAI and xAI exist twice
  (`email-service/service/company_routing.go`, `report-listener/database/evidence.go`)
  and are kept in step by hand.
- `report-ownership-service`: exact brand match after normalising; areas by
  polygon containment (`1e535020`, 2025-08-19).
- **Limit.** `[live 2026-11-17]`: Google Ads reports sit under four brand
  keys (`google`, `google ads`, `googleads`, `google_ads`).

### 11. Tags — live

Tags people type, normalised (`report-tags`, `e4e9bd81`, 2025-10-16).

### 12. Map binning — display only

One-degree cells for hotspots; a 64×64 grid per map tile
(`report-fast-renderer/src/tiles.rs`, `9bce5092`, 2026-11-16). Groups dots on
a screen. Says nothing about identity.

### 13. Reddit funnel clustering — offline

`tools/reddit_funnel` (2025-12-19) computes an LLM `cluster_key` and a
deterministic `cluster_id_coarse` (brand, issue type, two "atoms").
`ingest_to_cleanapp.py` builds a `cluster_id` and then does not write it, so
it is lost at import.

## Production gaps and offline evidence

- **Production text matching.** The services reviewed here use word overlap
  (4, 5, 7). The Reddit funnel documents embeddings as a proposed stage.
  Offline retrieval experiments should keep their inputs, review judgments
  and errors in the internal operating record so future work can reuse them.
- **Production visual matching.** The 10 m comparison path is the automated
  matcher in this inventory. The [published case examples](https://www.cleanapp.io/clustersamples)
  include a source-reviewed set of nine views of one wall, matched using
  details such as the ceiling corner, doorway and crack pattern. The
  [showcase data](https://www.cleanapp.io/clustersamples/samples.json)
  preserves individual observations and limits, and provides a public
  regression example for grouping and provenance.
- **A single store.** `report_clusters`, `case_reports`, `report_outcomes`
  candidates and the request-time `source_key` are separate and unconnected.

## What the documents already admit

- Notifications merge "by clock, not by content"
  (`notification-outcomes.md`, section 2). "Merge by issue" is a plan
  (section 4.4); the `thread_key` column exists and nothing writes it.
- Case membership "does not prove an identical defect, independent
  corroboration, or affected-user prevalence" (`docs/evidence-first-release.md`).

## Timeline

| Date | Commit | Change |
| --- | --- | --- |
| 2025-07-17 | `e3e7e57d` | Brand name normalisation |
| 2025-08-19 | `1e535020` | Ownership by area polygon and brand |
| 2025-09-04 | `bde0b5a3` | Photo comparison writing `report_clusters` |
| 2025-10-16 | `e4e9bd81` | Tags |
| 2025-12-19 | `7458c42c` | Reddit funnel clustering (offline) |
| 2026-03-10 | `aedafe27` | Cases, saved clusters, pairwise hypotheses |
| 2026-03-11 | `da03301a` | Cluster-to-case matching, attach, merge |
| 2026-10-14 | `1f6543b9` | Source-key deduplication, recurrence, `issue_id` |
| 2026-11-07 | `8931f82a` | DataUnion hash deduplication |
| 2026-11-14 | `54c3d58a` | Agent submission matching |
| 2026-11-16 | `38038c5c` | SolveSF case identity |

## Questions to reason through

1. What is the unit: the source item, the observation, the issue, or the
   case? Each has a different owner in the code today.
2. What would count as evidence that two observations are *independent*?
   The field exists and is never assessed.
3. The system can say "same place, 0.7" for two photos 10 m apart. What is
   the equivalent statement for two descriptions of a software fault?
4. Which grouping, if any, should a notice be built from?
5. Can two reviewers consistently distinguish a duplicated source, a related
   issue and independently corroborating evidence? Include near-match
   negatives in evaluation and measure false merges separately.
