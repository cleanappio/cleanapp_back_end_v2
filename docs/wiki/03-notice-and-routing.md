# Notice and routing

**In short.** The table below lists seventeen mechanisms for telling someone
about a report, or for working out whom to tell. For a party that has not
signed up, two are live: email to an address (three reviewed company
addresses, plus addresses a model proposes) and a chat notice to an EPC
address. Until November 2026 the system knew nothing after
"sent": the database held seven outcomes in total and no acknowledgement
`[db 2026-11-11]`. Every channel except the public page and the EPC ends at a
door the recipient controls.

Last reviewed: 2026-11-17, code at `bc799bd`.

## Every channel, in the order it was built

| # | Channel | Built | Reaches | Status | What is known about results |
| --- | --- | --- | --- | --- | --- |
| 1 | One report by email to an area's contacts (`backend/email`) | 2025-02-21 `a7cc9e2f` | People an admin registered for an area | retired; the call is commented out at `backend/db/db.go:982` | nothing recorded |
| 2 | Batch email per area (`email_sender/`) | 2025-07-13 `24f31c4c` | Same | built, never deployed `[inference]` | none |
| 3 | Per-report email (`email-service/`) | 2025-07-10 `0f8874ec`, active 2025-08-12 `eb95f308` | Addresses the model reads in the photo or infers on a known domain; area contacts | live for this path | 23 inferred-contact sends in 30 days `[db 2026-11-11]` |
| 4 | Customer dashboards (`customer-service`, `brand-dashboard`, `custom-area-dashboard`) | 2025-06 to 2025-10 | Parties that signed up: Montenegro, Red Bull, New York, DevConnect, Edge City | live | usage not recorded in the repositories |
| 5 | Ownership (`report-ownership-service`) | 2025-08-19 `1e535020` | A customer that registers a brand or an area | live | see "Ownership cuts against public notice" below |
| 6 | Email fetcher (`email-fetcher/`) | 2025-09-20 `354e612` | "Plausible vendor support emails" proposed by a model for a brand name | live role | feeds channel 3; accuracy never measured |
| 7 | Email service v3 (Rust, SendGrid, per brand) | 2025-09-25 `9eec1465` | Brands | built, off since 2025-10-08 (`ENABLE_EMAIL_V3=false`) | none |
| 8 | EPC chat notice (`epc-pusher/`) | 2025-10-07 `572a4cec`; AWS 2026-11-13 `47f3313e` | A public address derived from the brand name | live, pilot scope | page 04 |
| 9 | X replier (`replier-twitter/`) | 2025-11-16 `ff7dfe5` | The person who mentioned CleanApp, not the brand | not among the AWS release roles | none |
| 10 | Brand digest | 2025-12-11 `d263dcc0` | One email per brand instead of one per report | live, as company batching | below |
| 11 | Case escalations (`handlers/case_escalations.go`) | 2026-03-10 `8a2db17` | Targets a signed-in person chooses from a ranked plan | on demand | 7 outcomes: 5 sent, 2 bounced `[db 2026-11-11]` |
| 12 | Contact discovery (`handlers/case_contact_discovery.go`) | 2026-03-11 `019aedd` | Finds operators, owners and authorities: reverse geocoding, nearby places, Google Places, web search, contact pages | on demand, feeds 11 | not measured |
| 13 | Notify plans (`handlers/case_notify_routing.go`) | 2026-03-12 `878189e9`, `400d4f01` | Ranked waves: notify now; authorities and oversight; other stakeholders | on demand, feeds 11 | not measured |
| 14 | Push to the reporter (`handlers/mobile_push.go`) | 2026-03-15 `7925c866` | The reporter | live | not measured |
| 15 | Verified company routes (`email-service/service/company_routing.go`) | 2026-10-15 `9a7af00a` | One reviewed support address each for Anthropic, OpenAI, xAI | live | about 400 messages in 30 days covering about 10,700 report-recipient pairs `[db 2026-11-11]`; no reply ever recorded |
| 16 | Outcomes back to agents (`go-common/reportoutcome`) | 2026-11-14 `54c3d58a` | The agent that submitted | live | not measured |
| 17 | Outcome tracking: signed reply address, tracked link, inbox reading, silence | 2026-11-11 to 17 (`68aef91`, `02835f1`, `bc799bd`) | CleanApp itself | merged, not deployed; inbox reading needs mailbox access | none yet |

**Named in documents, never built:** phone calls ("soon", `ARCHITECTURE.md:7`),
web-form submission and phone tasks (`docs/architecture/domain-model.md`), a
Bluesky replier, webhooks and a partner API (`ARCHITECTURE.md` dispatch
diagram), posts on social media addressed to a brand, any submission to a
city's 311 system or to a regulator. SolveSF (2026-11-16) reads San
Francisco's cases and sends nothing back.

## How a notice goes out today

1. Analysis names a brand. `email-service` maps it, by a hand-kept alias list,
   to one of three companies.
2. Pending reports for that company wait up to five minutes and go out
   together: at most 20 per email and 10 emails per company per day
   (`company_notifications.go`). The batch is by clock. Two reports of one
   fault filed six minutes apart are two cold emails.
3. The email goes through the Google Workspace relay from `info@cleanapp.io`.
   "Sent" means the relay accepted it.
4. For every other brand, a report is emailed only if a model proposed an
   address. "Most reports about other brands are never sent anywhere"
   (`notification-outcomes.md`, section 2).
5. Separately, the EPC worker queues one chat notice per public report to the
   brand's EPC address (page 04).

## The routing engine that is waiting

March 2026 produced a full design, and the code for it
(`docs/architecture/domain-model.md`, decisions of 2026-03-11 to 13):

- **Contact observation → escalation target → notify plan → execution task →
  outcome → endpoint memory.** Discovery is kept apart from sending because
  "broad discovery is useful; indiscriminate sending is harmful".
- **Actionability score** per target: 0.30 source quality, 0.25 role fit,
  0.20 site match, 0.15 channel, 0.10 outcome memory, 0.08 confidence
  (`case_notify_routing.go:511-539`).
- **Waves**, with a cap per organisation per wave, so that there is "escalation
  headroom".
- **Authority directory rules** (`authority_directory_rules`, seeded in
  `migration_helpers.go`) for regulators and public bodies.
- **Endpoint memory** per address: starts at 0.56, rises with acknowledgements
  and fixes, falls with bounces, misroutes and silence.

None of this runs for an ordinary report. It runs when a person opens a case
and presses send. The outcome memory has almost no data to learn from.

## What the record shows

- **One company replied "no" through its own official channel** and the
  system could not record it (Google Ads, page 01).
- **No acknowledgement, fix or resolution signal has ever been recorded**
  `[db 2026-11-11]`. Until the work of 2026-11 nothing read the inbox, so
  bounces and auto-replies arrived and were never seen.
- **Large companies mostly publish no address.** A search for the official
  contact of the 24 most-reported brands found one published support email;
  the rest offer forms behind a login `[not verified: search results only]`.
- **Scale mismatch.** 97 brands hold 663,325 of the 1,099,412 digital records
  `[live 2026-11-17]`. Three have a route.

## Ownership cuts against public notice

A customer that registers a brand (`customer_brands`) or an area
(`customer_areas`) with `is_public` false makes matching reports private to
that customer (`report-ownership-service/database/service.go`). The public
readers and the EPC worker both skip a report with a private owner. This is
the "enterprise report" of the 2018 glossary: a report that "is not
published". It also means the party named on the public board can, by
subscribing, take its reports off the board. Whether that is intended for
reports the public files about a brand, as opposed to reports from inside a
customer's own premises, is not written down anywhere.

## Patterns

`[inference]`

1. **Every push channel ends at a door the recipient owns or the model
   guessed.** If the door is shut, missing or answers "no", the attempt is
   over.
2. **For 21 months the last known fact about any notice was "sent".**
3. **Discovery is far ahead of delivery.** The engine can find an operator, an
   owner and a regulator for a pothole. The only automatic delivery is email
   to a support address.
4. **Notices carry reports, not issues.** The recipient gets a list sorted by
   arrival time.
5. **There is no word for "no".** Outcome types are sent, bounced, viewed,
   acknowledged, replied, out of office, no response, misrouted, fixed. A
   party that answers and declines has no entry, so nothing can be triggered
   by it.
6. **Interested parties appear in every plan and in no delivery.** Waves two
   and three exist in the model. Nothing sends them.

## Questions to reason through

1. Should a private channel ever be the primary notice, or only a courtesy
   copy of a public one?
2. What should happen, automatically, when a party says "no", and when it
   says nothing?
3. Who are the interested parties for a software fault, concretely, and what
   would each accept as a notice?
4. Should an owner be able to make public reports about itself private?
5. What is the smallest end-to-end loop that would produce one recorded fix?
