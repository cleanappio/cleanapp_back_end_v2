# Evidence and truth

**In short.** Since October 2026 every public record says what kind of thing
it is (a first-hand account, coverage of someone else's, or unknown), where it
came from, and what it does not prove. That is the most careful part of the
system. Four things are still missing: whether two reports are independent,
whether a report is true, whether a fix happened, and what the party named
has to say.

Last reviewed: 2026-11-17, code at `bc799bd`.

## What a record states today

Built in the "evidence first" release (`1f6543b9` 2026-10-14, `f88024d`
2026-10-16; `docs/evidence-first-release.md`).

| Field | Values | Meaning |
| --- | --- | --- |
| `report_kind` | firsthand, coverage, unknown | Did the author experience it, or describe someone else's experience? |
| `entity_relationship` | | How the record relates to the company or product. |
| `intake_method` | | How it reached CleanApp. |
| Dates | source, publication, observation, ingestion | Kept apart. An unknown incident time stays unknown. |
| Original excerpt | checked literally against the source | The model may summarise. It may not put words in the source's mouth. |
| `recurrence` | `issue_id`, `supporting_report_count`, `independence` | Page 06. |

What is deliberately never claimed (same document):

- "Source reports/accounts are never presented as affected-user totals."
- "Case association does not prove an identical defect, independent
  corroboration, or affected-user prevalence."
- "Nothing infers a fix from source closure."
- Brand emails: "CleanApp has not independently reproduced these findings. A
  brand association identifies a likely subject and does not establish fault,
  ownership or legal responsibility" (`email-service/email/aggregate.go:259`).

Principles behind it: "AI is a compression tool, not an authority"
(`THEORY.md` §6). "Record facts, never inferences"
(`notification-outcomes.md` §3).

## What the labels show

`[live 2026-11-17]`, by company scope:

| Scope | Records | First-hand | Coverage | Unknown |
| --- | --- | --- | --- | --- |
| Anthropic | 7,144 | 120 | 2,053 | 4,970 |
| OpenAI | 14,499 | 120 | not read | not read |
| Google | 61,045 | 28 | not read | not read |
| Microsoft | 53,175 | 11 | not read | not read |
| Bluesky | 12,867 | 293 | not read | not read |

A closer look at Anthropic's 120 first-hand records: about 90 are distinct,
genuine complaints from single voices over 19 months (12 of them submitted
directly to CleanApp); about 14 are repeats of the same posts; about 15 are
not about a product fault. Of 200 sampled "unknown" records, 160 are Reddit
archive stubs whose text reads "Stored original text is unavailable".

`[inference]` The labels are honest, and what they honestly show is that
first-hand accounts are a very small part of what is counted: 572 of the
148,730 records in these five scopes, or 0.4%.

## Four things that are not assessed

### 1. Independence

`independence` is the fixed word "unassessed" (`evidence.go:620`). `THEORY.md`
§3 rests on it: "When independent reports converge … they become a
qualitatively different object." Nothing checks whether five reports are five
people or one person five times, or five people repeating one post.

### 2. Truth

There is no verification step. History of attempts:

| When | Idea | Status |
| --- | --- | --- |
| 2018 | A peer-reviewed CleanApperRating; "high-quality data … also requires human verification". | `[2018 wiki]`; idea only |
| 2025-07 | `is_valid` flag set by the model. | live |
| 2025-09 | Photo comparison against earlier reports at the same place. | write-only (page 06) |
| 2026-02 | Quarantine lane for new or untrusted machine sources. | live |
| 2026-03 | `wire_reputation` (precision, novelty, evidence, routing scores) and `fetchers.reputation_score`. | tables only; nothing writes them |
| 2026-04 | Sort game: people rate urgency and flag spam. | live in the mobile app; only the game reads its results |
| 2026-10 | Literal excerpt checks; kind labels. | live |

`[inference]` The system checks that a record faithfully represents its
source. It does not check whether the source is right. Those are different
problems, and the second gets harder as reports gain consequences, because
then people have reasons to file false ones.

### 3. Whether a fix happened

- **Photos:** when a new photo of the same place shows the hazard gone, the
  earlier report is marked resolved
  (`report-processor/handlers/handlers.go:274-278`). This is the only
  automatic detection of a fix in the system.
- **Everything else:** "Upstream/resolution states remain unknown when the
  stored evidence does not establish them." A `fixed` outcome exists and has
  never been recorded `[db 2026-11-11]`.

### 4. The other side

A party named in a report cannot put anything on the record. There is:

- no claimed, verified identity for a brand that is not a paying customer;
- no response attached to a report or a case;
- no outcome type for "declined", "disputed" or "works as designed";
- no way to show that a company did answer, and what it said.

`[founder]` The Google Ads disclosure was answered "intended behavior". That
sentence is the most informative fact in the case, and it lives only in
someone's inbox.

`[inference]` A board that carries only the accusation is weaker than one that
carries the answer as well. An answer of "intended behavior", next to
screenshots of six billion installs, makes the case better than any summary
CleanApp could write. Silence, with a date, does the same.

## Privacy and visibility pull the other way

| Rule | Since | Effect |
| --- | --- | --- |
| Faces are blurred | 2025-09-03 `c00accdc` | Photos stay publishable. |
| Owner-private reports | 2025-08-19 `1e535020` | A customer's brand or area can keep its reports off the public record (page 03). |
| Quarantine lane | 2026-02-14 `719061d` | Untrusted machine input is stored and not shown. |
| Opaque public IDs | 2026-03-11 `746c35f` | Reports cannot be enumerated by number. |
| Agent intake private by default | 2026-11-13 `b0f8bd8` | A machine's first reports are not public until reviewed. |
| Import suppression | 2026-11-07 `8931f82a` | Recovered archives never trigger notices or rewards. |

Each rule is sound alone. Together they mean "public record" has several
exceptions, and one of them (owner-private) is controlled by the party being
reported on.

## Questions to reason through

1. What is the least that would let the system say "four independent people"
   and mean it?
2. Should truth be assessed by CleanApp, by other reporters, by the party
   named, or left open and labelled?
3. What should the party named be able to add to the record, and what should
   it never be able to remove?
4. Does a first-hand count of 120 out of 7,144 change what should be shown on
   the home page and in a notice?
5. How should the system defend against a report filed to harm a competitor?
