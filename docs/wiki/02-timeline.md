# Timeline

**In short.** The idea dates from 2013. Running code dates from October 2023.
The work came in five bursts, each adding a layer: the reporting app and
rewards (2023–24), analysis and email and dashboards (mid-2025), social intake
and the first public notices (late 2025), agents, cases and the routing engine
(early 2026), then evidence, outcomes and the move to AWS (autumn 2026).
Measuring what happens after a notice is sent was the last thing built, 21
months after the first email.

Sources: the 2018 wiki for the first section, `git log` for the rest. Each
commit below was checked against the repository on 2026-11-17.

## Before the repositories (2013–2022)

| When | What | Source |
| --- | --- | --- |
| 2013-04 | CleanApp starts as a wiki community around waste and hazard mapping, and as a defensive patent project. | `[2018 wiki]` |
| 2013 onward | A US utility patent filing on "harmonized waste/hazard reporting, analytics, response processes". The wiki links publication US20150031343A1. | `[2018 wiki]`; status `[not verified]` |
| 2015 | Research into blockchain begins: rewards for reporting, verifying and responding; shared storage; resistance to censorship. | `[2018 wiki]` |
| 2018-07-09 | The GitHub wiki opens (81 edits to 2019-12-21). It names a CleanApp Foundation, a CleanApp Standard, a white paper and a planned "yellow paper". | `[2018 wiki]` |
| 2018–2019 | The wiki's glossary defines the report types still in use as ideas: anonymous, aggregate (many reporters), composite (one reporter, many reports), chronological (over time), enterprise (not published), private. It defines CleanAppTrack and a peer-reviewed CleanApperRating. | `[2018 wiki]` |
| 2022-11 to 12 | About ten small repositories appear within two weeks: map overlay notes (`CleanAppMap`), a Twitter listener and "dumpsite" map (`Twitter-integration`), a sorting game (`CleanAppGame`), robot and API stubs. They hold code excerpts and notes, not services. | repositories; `[inference]` on their nature |
| 2023-08-27 | `cleanapp-mobile-latest`: the React Native app rebuilt. | repository |

**Not in the repositories:** what happened between 2013 and 2018, and between
2019 and 2022. The founder is the only source.

## Commits per month, backend

```
2023-10  15   2024-04  15   2025-02  22   2025-09  94   2026-03 120
2023-11  59   2024-05   5   2025-04   2   2025-10  69   2026-04   2
2023-12  63   2024-06   7   2025-05   2   2025-11  56   2026-09  15
2024-01  41   2024-07  30   2025-06  49   2025-12 103   2026-10 100
2024-02   4   2024-08  26   2025-07 139   2026-02 175   2026-11  44
2024-03   9   2024-09–12 15 2025-08  74   (2026-05 to 08: none)
```

## 2023–2024: the reporting app and rewards

| Date | Commit | What |
| --- | --- | --- |
| 2023-10-18 | first commit | Backend begins. |
| 2023-10-25 | `d7028298` | Referral service. |
| 2023-11-22 | `cd8928f0` | Teams, green against blue. |
| 2023-11-29 | `d2c18432` | Top scores. |
| 2023-12-24 | `72fac8a9` | KITN token balance stored per user. |
| 2024-02-25 | `656528eb` | Referral rewards paid up the chain. |
| 2024-04-20 | `b079cd93` | Daily on-chain KITN payout. |
| 2024-06-30 | `c61fb5d0` | Link to the reporter's token history on a block explorer. |
| 2024-09-26 | `8c6d20c` | "Actions" (campaigns with an end date). |
| 2024-11-04 | `c0058952` | Scheduled payout through STXN smart transactions. |

## 2025: analysis, email, dashboards, social intake, first public notices

| Date | Commit | What |
| --- | --- | --- |
| 2025-02-14 | `f19b3e28` | Areas: polygons with contact emails. |
| 2025-02-21 | `a7cc9e2f` | **First notice.** One report, photo and map, emailed to an area's contacts. |
| 2025-06-14 | `82eaa94e` | Customer accounts, plans, Stripe. |
| 2025-06-27 | `411462f8`, `c69da232` | Language-model analysis (litter, hazard, severity scores). Report listener. |
| 2025-07-10 | `0f8874ec`, `28bea869` | Email service. Montenegro dashboard. |
| 2025-07-11 | `dc8eac4e` | Public live feed of new reports. |
| 2025-07-13 | `24f31c4c` | Batch email per area. Never deployed. |
| 2025-07-16 to 24 | `50cd9e25`, `e3e7e57d`, `c2b68b73`, `8d693b6a` | Brand dashboard, brand-name normalising, Red Bull and New York dashboards. |
| 2025-08-12 | `eb95f308` | Email service activated, with addresses the model reads or infers. |
| 2025-08-19 | `1e535020` | Ownership: a customer's brand or area can make its reports private. |
| 2025-08-26 | `afc9d5c5` | Reports pushed to STXN as "objectives". |
| 2025-09-03 | `c00accdc` | Face blurring. |
| 2025-09-04 | `bde0b5a3` | Photo matching: same place, hazard removed. |
| 2025-09-14 | `741459b` | News indexer. |
| 2025-09-15 | `3b15671e` | **EPC code imported.** |
| 2025-09-20 | `354e612` | Email fetcher: the model proposes support addresses for brands. |
| 2025-09-24 | `9cd65b3` | Voice assistant. |
| 2025-09-25 | `9eec1465` | Email service v3. Switched off 2025-10-08 (`10ab5a09`). |
| 2025-10-07 | `572a4cec` | **EPC pipeline working**, sending from the development environment. |
| 2025-10-16 | `e4e9bd81` | Tags. |
| 2025-10-25, 28 | `89869af8`, `fb9a766b` | DevConnect and Edge City dashboards. |
| 2025-11-04 | `40783b3` | X indexing. |
| 2025-11-16 | `ff7dfe5` | X replier: answers the person who mentioned CleanApp. |
| 2025-11-24 | `3255745` | Analysis moved to Gemini. |
| 2025-12-09 | `af0b9997` | **Dollar liability estimates** in prompts and emails. |
| 2025-12-10 | `53d891c` | Bluesky intake. |
| 2025-12-11 | `d263dcc0` | One email per brand, after "100 reports = 100 emails". The digest has never carried a liability figure. |
| 2025-12-17, 18 | `41183454`, `1c2098be` | By-brand API; high and medium priority counts. |
| 2025-12-18 | `2da9958a` | `THEORY.md` (and `WHY.md`). |
| 2025-12-19 | `7458c42c` | Reddit funnel with offline clustering. |

## 2026: agents, cases, routing, then evidence and outcomes

| Date | Commit | What |
| --- | --- | --- |
| 2026-02-12 | `839df3d4` | Brand intelligence chat, severity bands, the Pro tier. |
| 2026-02-14 | `719061d`, `a8ea35e`, `690bad1e`, `0632da0a` | Quarantine lane, agent keys, OpenClaw skill, public uptime page. |
| 2026-02-16 | `08310697` | Command-line client. |
| 2026-03-07, 08 | `7d80e96e`, `d92e8ba9` | CleanApp Wire: one intake core for machines. Decision log begins. |
| 2026-03-10 | `aedafe27`, `8a2db17` | **Cases**, cluster hypotheses, escalations. |
| 2026-03-11 | `019aedd`, `da03301a`, `746c35f` | Contact discovery, cluster-to-case matching, opaque public IDs. |
| 2026-03-12 | `878189e9`, `400d4f01`, `d401fee` | **Routing engine**: ranked targets, waves, authority rules, endpoint memory. |
| 2026-03-15 | `7925c866` | Push notifications to the reporter. |
| 2026-03-25 | `79993727` | Text-only analysis prompt: "Never invent … billing amounts … or metrics". |
| 2026-04-04 | `0a75cfe9` | Sort game: people rate reports for urgency. |
| 2026-04 to 08 | | Two commits in April, none from May to August. |
| 2026-10-14 | `1f6543b9` | Source deduplication, recurrence block, `issue_id`. |
| 2026-10-15 | `9a7af00a`, `293f9061` | **Verified company routes** (three). Notification history. |
| 2026-10-16, 17 | `52c943d0`, `f88024d`, `183688c`, `b3587d24` | Evidence-first brand pages and emails. Claude added as an analysis provider. |
| 2026-11-05 | `cb85ec9` | AWS release path. |
| 2026-11-07 | `8931f82a` | DataUnion import, with every side effect suppressed. |
| 2026-11-11 | decisions | Outcomes are recorded from evidence, never inferred from a send. Reporters see outcomes free. |
| 2026-11-13 | `47f3313e`, `ebbbd02e`, `b0f8bd8` | **EPC notices in AWS production.** Agent intake, private by default. |
| 2026-11-14 | `54c3d58a` | Agents get outcomes back. Listed in the MCP Registry. |
| 2026-11-16 | `38038c5c`, `9bce5092` | SolveSF import. Map tiles. |
| 2026-11-17 | `68aef91`, `02835f1`, `bc799bd` | Reading replies, recording silence, tracked links. Merged, not yet deployed. |

## What the order shows

`[inference]`

1. **Intake always ran ahead of everything else.** Each burst added sources.
   The corpus holds over a million records, and no automatic grouping by
   issue feeds anything (page 06).
2. **The email notice was rebuilt five times in 2025** (February, July,
   August, September, December) and again in October 2026. Each rebuild
   changed who is addressed or how reports are batched. None changed what is
   known after sending.
3. **The liability figure was tried once**, in December 2025. Two days later
   the brand-facing email was replaced, for an unrelated reason (email
   volume), by a digest that does not carry it. No decision against the
   figure is recorded (page 05).
4. **The routing engine of March 2026 is the most complete design in the
   repository and the least used.** It serves escalations that a person
   sends by hand (page 03).
5. **Public notice has been one step from complete since October 2025.**
   Addresses and sending work. The public board and the contracts do not
   exist (page 04).
