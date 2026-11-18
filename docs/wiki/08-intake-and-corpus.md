# Intake and corpus

**In short.** Reports arrive from people (the mobile app, web forms, shares),
from programs CleanApp runs (indexers for X, Bluesky, Reddit, GitHub), from
other people's agents, and from bulk imports. The database holds about 1.1
million digital records and 441,000 physical ones. Most digital records were
imported, most imported records are not first-hand, and the importers appear
to have stopped when production moved to AWS.

Last reviewed: 2026-11-17.

## Sources

| Source | Since | How | Status |
| --- | --- | --- | --- |
| Mobile app: photo, location, note | 2023-10 | `backend/` | live |
| Web forms and shares | pages such as `/report-a-problem`; one intake core decided 2026-03-11 | `report-listener/handlers/human_ingest.go`, `digital_share.go` | live |
| Voice | 2025-09-24 `9cd65b3` | `voice-assistant-service/` | live role |
| News and X index | 2025-09-14 `741459b`; X 2025-11-04 `40783b3` | `news-indexer/` | not among the AWS release roles |
| GitHub issues | 2025-09-29 `1aa5f47` | fetcher | same |
| Bluesky | 2025-12-10 `53d891c` | `news-indexer-bluesky/` | same |
| Reddit, live and archive | 2025-12; funnel 2025-12-19 `7458c42c` | indexer; `tools/reddit_funnel` | same; funnel offline |
| Other people's agents | keys 2026-02-14 `a8ea35e`; Wire 2026-03-08 `d92e8ba9`; open intake 2026-11-13 `b0f8bd8` | `handlers/cleanapp_wire_v1.go`, `public_agent_ingest.go` | live |
| Agent tooling | OpenClaw skill 2026-02-14; command line 2026-02-16; MCP Registry 2026-11-14 | `openclaw/`, `cli/` | live |
| DataUnion archive | 2026-11-07 `8931f82a` | one import; notices and rewards suppressed for good | done |
| SolveSF (San Francisco 311) | 2026-11-16 `38038c5c` | reads the city's public cases; sends nothing back | live |

"Not among the AWS release roles": the release helper
(`platform_blueprint/deploy/prod/vm/aws_image_deploy.py`) lists 22 roles and
none is an indexer. `AWS_DEPLOY.md` says the helper cannot "activate
unregistered analyzer/social-worker roles". Whether an indexer runs on the
host outside the helper was not checked. The live numbers below are
consistent with none running.

Intake rules worth knowing:

- "Recall beats precision early" (`THEORY.md` §7): take everything, filter
  later.
- One key per source item per fetcher (`UNIQUE(fetcher_id, source_id)`). The
  same post through two fetchers lands twice (page 06).
- Raw input is never thrown away (`INVARIANTS.md` §5).

## Size

`[live 2026-11-17]`

| Measure | Value |
| --- | --- |
| Digital records | 1,099,412 |
| Brands with at least one | 53,461 |
| Brands with 1,000 or more | 97, holding 663,325 records |
| Physical reports | 441,080 |

## What is in it

`[live 2026-11-17]`, grouped public briefing entries by company scope.
See the [briefing API counting rules](../../report-listener/docs/evidence-first-api.md#response-semantics).

| Scope | All entries | First-hand entries | Added in the last 30 days |
| --- | --- | --- | --- |
| Google | 61,045 | 28 | 1 |
| Microsoft | 53,175 | 11 | 1 |
| OpenAI | 14,499 | 120 | 1 |
| Bluesky | 12,867 | 293 | 0 |
| Anthropic | 7,144 | 120 | 6 |

Three observations:

1. **First-hand entries were a small share of this sample.** The scope
   ratios ranged from 0.02% (Microsoft) to 2.3% (Bluesky). The five scopes
   showed 572 first-hand entries against 148,730 grouped entries.
2. **Recent additions were sparse in this sample.** The five scopes showed
   nine additions in thirty days. This view alone gives limited evidence
   about any individual intake source.
3. **Much of the "unknown" class is empty.** In a sample of 200 unknown
   Anthropic records, 160 were archive stubs with no text (page 07).

## Known defects in the corpus

| Defect | Example | Page |
| --- | --- | --- |
| Repeats under different links | One Bluesky post about a Claude Code error appears as six first-hand reports. | 06 |
| Brand spelling splits one subject | Google Ads sits under `google`, `google ads`, `googleads`, `google_ads`. | 06 |
| Stubs counted as records | "Stored original text is unavailable." | 07 |
| Wrong subject | About 15 of Anthropic's 120 first-hand records are not about a product fault. | 07 |
| Flagship case filed as unknown | The Google Ads records are marked unknown, not first-hand. | 01 |

## Questions to reason through

1. Which published count explains the value to the reader: corpus size,
   distinct source observations, or human-validated issues?
2. Should intake favour recall, as `THEORY.md` says, once notices are public
   and carry weight?
3. Which sources produce first-hand accounts at a useful rate? Bluesky's
   2.3% is a hundred times Microsoft's 0.02%. Direct submissions are first-hand by
   construction and are the rarest.
4. Should the indexers be restarted before identity is solved, or after?
