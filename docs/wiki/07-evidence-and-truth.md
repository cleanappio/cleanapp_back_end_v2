# Evidence and truth

**In short.** Since October 2026 every public record says what kind of thing
it is (a first-hand account, coverage of someone else's, or unknown), where it
came from, and the limits of the underlying evidence. Independence, factual
verification, remediation and the named party's disposition each need
their own evidence.

Last reviewed 2026-11-18. Notification services reported `bc799bd`.

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

`[live 2026-11-17]`, public briefing entries by company scope:

| Scope | All entries | First-hand | Coverage | Unknown |
| --- | --- | --- | --- | --- |
| Anthropic | 7,144 | 120 | 2,053 | 4,970 |
| OpenAI | 14,499 | 120 | not read | not read |
| Google | 61,045 | 28 | not read | not read |
| Microsoft | 53,175 | 11 | not read | not read |
| Bluesky | 12,867 | 293 | not read | not read |

The [briefing API](../../report-listener/docs/evidence-first-api.md#response-semantics)
counts source observations and existing families after grouping and
visibility filtering. A family can appear in multiple kinds. Keep the
company scope, kind and observation date with any reused count.

A closer look at Anthropic's 120 first-hand entries: about 90 are distinct,
genuine complaints from single voices over 19 months (12 of them submitted
directly to CleanApp); about 14 are repeats of the same posts; about 15 are
not about a product fault. Of 200 sampled "unknown" records, 160 are Reddit
archive stubs whose text reads "Stored original text is unavailable".

`[inference]` The first-hand entry count was 572 against 148,730 grouped
entries across these five scopes, roughly 0.4% in this dated sample.

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

- **Photo comparison:** the close-range matching handler can mark an
  earlier report resolved and create a response based on a model comparing
  two images. Treat that as a candidate for verification.
  [Match handler](../../report-processor/handlers/handlers.go#L274-L288)
- **Source of a resolution:** a responsible party can report a fix, a
  reporter or reviewer can independently observe improvement, or a public
  operational record can identify a change. Store the source, date, scope
  and confidence. A repeat observation should match the location and
  condition for a physical issue or the relevant product version and
  conditions for a digital issue.
- **Outcome and attribution:** record a verified change even if nobody
  responds through CleanApp. Credit CleanApp's role only where evidence
  links its notice to the action. Preserve unknown and unobservable
  outcomes explicitly. The 2026-11-11 snapshot contained zero `fixed`
  outcomes. [Scorecard](11-scorecard.md)

### 4. The other side

The inbox worker can associate a reply with a notification and classify the
message. A case operator can record a supported outcome with a note. The
outcome model still needs structured dispositions, a source reference and
evidence that the response concerns the issue and comes from the relevant
role. [Inbox outcomes](../architecture/notification-outcomes.md#what-1b-changed)
· [Case outcome handling](../../report-listener/handlers/case_escalations.go#L146-L164)

Make it easy for a verified party to correct a fact, forward the notice,
dispute a claim, identify an existing ticket or volunteer a fix. Preserve
original correspondence in the private operating record. Publish a reviewed
summary where appropriate, and show the observation window for any "no
response recorded" status.

`[founder]` Google Ads reportedly answered "intended behavior" through its
disclosure process. The [published case](https://www.cleanapp.io/clustersamples#google-ads)
documents displayed values and dates of relay acceptance. It links eight
standalone captures and four records sharing a later composite. Observer
independence, the source reply and the responding role are unverified in
this review.

## Privacy and visibility pull the other way

| Rule | Since | Effect |
| --- | --- | --- |
| Faces are blurred | 2025-09-03 `c00accdc` | Photos stay publishable. |
| Owner-private reports | 2025-08-19 `1e535020` | A qualifying private ownership row excludes a report from public surfaces (page 03). |
| Quarantine lane | 2026-02-14 `719061d` | Untrusted machine input is stored and not shown. |
| Opaque public IDs | 2026-03-11 `746c35f` | Reports cannot be enumerated by number. |
| Agent intake private by default | 2026-11-13 `b0f8bd8` | A machine's first reports are not public until reviewed. |
| Import suppression | 2026-11-07 `8931f82a` | Recovered archives never trigger notices or rewards. |

Apply the same publication rules to reports and to their notices, replies
and corrections. Keep moderation, privacy and private intake rules explicit.
Review any business rule that could make public visibility depend on who
pays. [Ownership filter](03-notice-and-routing.md#private-ownership-and-public-visibility)

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
