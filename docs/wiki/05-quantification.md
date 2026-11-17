# Quantification

**In short.** The claim that this data is "liability data" is from 2018. The
only money figures CleanApp has ever produced are sentences a language model
writes about one report at a time, following a fixed table of dollar ranges
added on 2025-12-09. Brands saw them for two days. No figure has ever been
added up for an issue, a brand or a place, and none is tied to evidence in the
report. Everything else the system computes is a score from 0 to 1, a count,
or a token balance.

Last reviewed: 2026-11-17, code at `bc799bd`.

## Where the idea comes from

- `[2018 wiki]` "Waste/hazard/pollution data is also **liability data**."
  "Real-time and historical time-series waste/hazard data is extremely
  valuable." "Minimizing waste/hazards leads to higher productivity."
- Email footer since 2025: "Trash is cash," (`email_sender.go:411`).
- Unused strings in the frontend: "LIVE insights, lower maintenance &
  liability costs, much higher margins." (`src/lib/i18n.ts:299-302`).
- `THEORY.md` §3: many similar reports are "far more than N× valuable". §4
  names "insurers and risk analysts" among interested parties.
- `[founder]` Earlier versions quantified liability costs and financial
  benefits. What is sold is feedback that improves the customer's results.

## Inventory

### A. Money

**A1. `legal_risk_estimate`: one sentence per report — live.**

- A free-text field the model fills in. The prompt gives it this table
  (`report-analyze-pipeline/llm/prompts.go:105-115`):

  | Situation | Text the model is told to produce |
  | --- | --- |
  | Slip and fall at an entrance | "minimum 7-figure liability (slip & fall settlements average $30K-$50K, serious injuries exceed $1M)" |
  | Sharp objects, broken glass | "injury liability: $10K-$500K depending on severity" |
  | Blocked emergency exit | "fire code violation: $10K-$100K fines plus liability" |
  | Unsanitary conditions | "health code violation: $1K-$25K per incident" |
  | Data breach or leak | "$150-$200 per affected record (GDPR/CCPA penalties)" |
  | Form submission errors | "lost leads valued at $50-$500 per conversion" |
  | Payment bugs | "cart abandonment: 1-3% revenue impact" |
  | Analytics errors | "decision-making risk: unquantifiable strategic cost" |

- Stored as text in `report_analysis.legal_risk_estimate`. Carried in the
  `report_analysed` event. Returned by one internal admin endpoint. No public
  API and no frontend component reads it.
- Added `af0b9997`, 2025-12-09. The wording has not changed since.

**A2. "Estimated Liability" box and "Legal Risk Factor" gauge — live on one
email path.**

- `email-service/email/email_sender.go:544-593`.
- The gauge is not a risk model. It is `HazardProbability * 100` under a new
  label (`:548`).
- Before 2025-12-09 the email showed three gauges: litter, hazard, severity.
- Two days after it was added, brand emails became a digest (`d263dcc0`). The
  digest shows counts only. Since 2026-10-16 it adds that a brand association
  "does not establish fault, ownership or legal responsibility"
  (`aggregate.go:259`).
- The box survives on the per-report path, which serves model-proposed
  addresses and area contacts. That it still reaches people is `[inference]`
  from the code paths, not from send logs.

**A3. Prices — live.** Free; Pro at $99 a month; Enterprise at $499 a month
for up to five brands or places (`cleanapp-frontend/src/pages/pricing.tsx`).
These price access, not reports.

### B. Scores on one report

| Score | Range | Since | Note |
| --- | --- | --- | --- |
| `litter_probability`, `hazard_probability`, `severity_level` | 0–1 | 2025-06-27 `411462f8` | Set by the model. Range-checked. |
| `digital_bug_probability` | 0–1 | 2025-08 | Schema key misspelled `digital_bug_probabilty`. |
| Sort-game urgency | 0–10 | 2026-04-04 `0a75cfe9` | People vote. Stored as mean, high-value count, spam count. |

Severity is shown as "N/10" on report pages (`severity_level*10`). The email
gauge code treats it as already 0–10, with cut-offs at 3 and 7
(`email_sender.go:639`), while the model writes 0–1.

### C. Scores on a group

| Score | How | Where shown | Status |
| --- | --- | --- | --- |
| High and medium priority counts per brand | severity ≥ 0.7; 0.4–0.7 | by-brand API, brand pages | live |
| Brand intelligence bands and growth | critical 0.85, high 0.70, medium 0.40; last 7 days against the 7 before | intelligence chat | live |
| Area averages | mean severity, litter and hazard probability | area dashboards | live |
| Case scores | severity, urgency, confidence, exposure, criticality, trend | case workspace | on demand |
| Contact actionability | page 03 | internal | on demand |

Case scores are mostly fixed numbers. A new case starts with confidence 0.8,
exposure 0.6 and trend 0.4 (`handlers/cases.go:439-444`). Urgency rises 0.05
per added cluster. Trend is clusters divided by five. **Exposure is never
recomputed**: every case in the system has the same exposure.

### D. Rewards

One KITN per report, per photo match and per sort vote; a tenth per level to
referrers. See page 09. A token balance is a count of activity. It is not a
value placed on a report.

### E. Named and never built

- "Cost Impact Analysis" and "Predictive Risk Assessment": locked teaser
  items in the frontend (`AIInsights.tsx:138-142`). No backend.
- "Risk and hotspot forecasting" on the Enterprise plan. No forecasting code.
- `wire_reputation` and `fetchers.reputation_score`: tables with defaults.
  Nothing writes them.

## Where the code disagrees with itself

1. **Two prompts in one file.** The image prompt tells the model to use the
   dollar table. The text prompt, added 2026-03-25, says "Never invent …
   billing amounts … or metrics unless they appear in the provided
   text/context" (`prompts.go:127`). Both are live.
2. **The gauge is mislabelled.** "Legal Risk Factor" is hazard probability.
3. **Severity has two scales.**
4. **Evidence-first against liability.** The October 2026 release removed
   every claim the evidence does not support. The December 2025 table asserts
   seven-figure liability from a photograph.

## Why the present figures cannot carry weight

`[inference]`

- The ranges have no stated source.
- They attach to a category, not to the facts of the report. Every wet floor
  near a door gets "minimum 7-figure liability".
- They describe the worst case of one incident. They say nothing about how
  likely it is, how many people pass, or how long the condition has existed.
- For digital faults the unit is missing: "per affected record" and "per
  conversion" need a count the system does not have.
- A figure that a recipient can dismiss in one sentence weakens the notice it
  travels with.

## What a figure that holds up would need

| Input | Meaning | Does the system have it? |
| --- | --- | --- |
| Probability the report is true | page 07 | No. Kinds of evidence are labelled; truth is not assessed. |
| Number of independent people affected | prevalence | No. `independence` is "unassessed" (page 06). |
| Exposure | how many people or transactions meet the defect | No. A constant 0.6. |
| Harm if it happens, and how often | base rates | No. Only the unsourced table. |
| Time the party has known | from first notice to now | Partly. Send times are stored. Acknowledgement is not. |
| What fixing it costs and returns | the party's side | No. |
| What happened in similar cases | outcomes | No. Zero fixes recorded `[db 2026-11-11]`. |

`[inference]` Valuation sits downstream of identity, truth and outcomes. It
cannot be fixed by a better prompt.

What can be measured now, without any of the missing inputs, and is hard to
dismiss:

- how many distinct sources report an issue, and over how long;
- how long since the first notice was posted;
- whether the issue came back after it was said to be fixed;
- how long the party took to answer, or that it has not.

`[inference]` These four are also the facts that liability turns on: what was
known, since when, and what was done. They may be the honest first form of
"liability data".

## Three framings, to be kept apart

1. **Cost avoided** by the responsible party: injury claims, fines,
   penalties, recalls.
2. **Revenue kept or recovered** by the responsible party: lost conversions,
   churn, refunds, support load.
3. **Value of knowing** to an interested party: an insurer pricing a risk, a
   regulator choosing where to look, an investor, a competitor, a journalist,
   a claimant's lawyer.

The December 2025 table mixes the first two. The third has never been
attempted, though it is the oldest (2018: "interested parties pay a small
subscription fee").

## Questions to reason through

1. Which of the three framings is the product, for which buyer?
2. Is a dollar figure needed at all, or do count, duration and silence do the
   work?
3. If a figure is needed, what source would a sceptical recipient accept:
   court records, insurance loss data, regulator penalty tables, the
   company's own published numbers?
4. Should the figure belong to the issue rather than the report?
5. Who is allowed to see it: the party only, or the public board?
