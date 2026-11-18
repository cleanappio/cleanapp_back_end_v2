# Notice and routing

**In short.** CleanApp can publish a record, send private outreach and record
what happens next. Public posting, transport acceptance, a tracked link
request, acknowledgment by the accountable role and remediation each require
different evidence. The 2026-11-11 database snapshot contained seven outcomes,
all from manual case escalation. See [notice and remediation](12-notice-and-remediation.md)
for the proposed operating model.

Last reviewed 2026-11-18. Notification services reported `bc799bd`.

## Every channel, in the order it was built

| # | Channel | Built | Reaches | Status | What is known about results |
| --- | --- | --- | --- | --- | --- |
| 1 | One report by email to an area's contacts (`backend/email`) | 2025-02-21 `a7cc9e2f` | People an admin registered for an area | retired; the call is commented out at `backend/db/db.go:982` | nothing recorded |
| 2 | Batch email per area (`email_sender/`) | 2025-07-13 `24f31c4c` | Same | built, never deployed `[inference]` | none |
| 3 | Per-report email (`email-service/`) | 2025-07-10 `0f8874ec`, active 2025-08-12 `eb95f308` | Addresses the model reads in the photo or infers on a known domain; area contacts | live for this path | 23 inferred-contact sends in 30 days `[db 2026-11-11]` |
| 4 | Customer dashboards (`customer-service`, `brand-dashboard`, `custom-area-dashboard`) | 2025-06 to 2025-10 | Parties that signed up: Montenegro, Red Bull, New York, DevConnect, Edge City | live | usage not recorded in the repositories |
| 5 | Ownership (`report-ownership-service`) | 2025-08-19 `1e535020` | A customer that registers a brand or an area | live filter | see "Private ownership and public visibility" below |
| 6 | Email fetcher (`email-fetcher/`) | 2025-09-20 `354e612` | "Plausible vendor support emails" proposed by a model for a brand name | live role | feeds channel 3; accuracy never measured |
| 7 | Email service v3 (Rust, SendGrid, per brand) | 2025-09-25 `9eec1465` | Brands | built, off since 2025-10-08 (`ENABLE_EMAIL_V3=false`) | none |
| 8 | EPC chat notice (`epc-pusher/`) | 2025-10-07 `572a4cec`; AWS 2026-11-13 `47f3313e` | A public address derived from the brand name | live, pilot scope | page 04 |
| 9 | X replier (`replier-twitter/`) | 2025-11-16 `ff7dfe5` | The person who mentioned CleanApp, not the brand | not among the AWS release roles | none |
| 10 | Brand digest | 2025-12-11 `d263dcc0` | One email per brand instead of one per report | live, as company batching | below |
| 11 | Case escalations (`handlers/case_escalations.go`) | 2026-03-10 `8a2db17` | Targets a signed-in person chooses from a ranked plan | on demand | 7 outcomes: 5 sent, 2 bounced `[db 2026-11-11]` |
| 12 | Contact discovery (`handlers/case_contact_discovery.go`) | 2026-03-11 `019aedd` | Finds operators, owners and authorities: reverse geocoding, nearby places, Google Places, web search, contact pages | on demand, feeds 11 | not measured |
| 13 | Notify plans (`handlers/case_notify_routing.go`) | 2026-03-12 `878189e9`, `400d4f01` | Ranked waves: notify now; authorities and oversight; other stakeholders | on demand, feeds 11 | not measured |
| 14 | Push to the reporter (`handlers/mobile_push.go`) | 2026-03-15 `7925c866` | The reporter | live | not measured |
| 15 | Reviewed company routes (`email-service/service/company_routing.go`) | 2026-10-15 `9a7af00a` | One reviewed support address each for Anthropic, OpenAI, xAI | live | about 400 messages in 30 days covering about 10,700 report-recipient pairs `[db 2026-11-11]`; accountable-role reach unverified |
| 16 | Outcomes back to agents (`go-common/reportoutcome`) | 2026-11-14 `54c3d58a` | The agent that submitted | live | not measured |
| 17 | Outcome tracking: signed reply address, tracked link, inbox reading, silence | 2026-11-11 to 17 (`68aef91`, `02835f1`, `bc799bd`) | CleanApp itself | notification binaries deployed; tracked links depend on a route check; inbox and silence workers default off | live operational coverage unverified |

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

The ordinary report sender uses a separate path. In a case, an operator can
[select email-capable targets and send a reviewed draft](../../report-listener/handlers/case_escalations.go#L205-L246),
then [record a supported outcome and a note](../../report-listener/handlers/case_escalations.go#L90-L164).
Use a private evidence reference for original correspondence. The case workflow
has typed delivery outcomes and freeform notes. A verified role, issue-specific
disposition and confidential source reference still need a consistent record.

## Deployment and proof

The 2026-11-17 release receipt records the listener and email binaries at
`bc799bd` and successful application of the inbox migration. The
[listener](https://live.cleanapp.io/version) and
[email](https://email.cleanapp.io/version) version endpoints returned that
revision on 2026-11-18. The [tracking release](https://github.com/cleanappio/cleanapp_back_end_v2/pull/229)
defaults signed reply and view tracking on for reviewed company notices.
Tracked links have a readiness gate. Mailbox processing and silence detection
have separate configuration, both default off, and their live coverage is
unverified.

A relay acceptance receipt establishes that the relay accepted a message. A
tracked request passes a browser heuristic that can also admit automated
checks or forwarded links. A reply to a batch is recorded against each
included report, so issue-specific attribution requires review. Log
acknowledgment by the accountable role only with evidence of the role and the
particular issue addressed. Treat later work and remediation as separate
observations. [Link handler](../../report-listener/handlers/notification_links.go#L33-L67)
· [Inbox outcome recording](../../report-listener/database/inbox.go#L125-L139)

## What the record shows

- **An off-platform disposition is missing from CleanApp's structured
  history.** The founder reports an "intended behavior" reply to the Google
  Ads disclosure. The original reply and authority of the responding role
  are unverified in this review. A case note could record that account with
  its source and limitations. [Published case](https://www.cleanapp.io/clustersamples#google-ads)
- **Seven outcomes were recorded as of 2026-11-11.** Five were `sent` and two
  were `bounced`. The snapshot contained zero acknowledgments or fixes.
  Activity in unmonitored channels remains unknown.
- **Large companies mostly publish no address.** A search for the official
  contact of the 24 most-reported brands found one published support email;
  the rest offer forms behind a login `[not verified: search results only]`.
- **Scale mismatch.** 97 brands hold 663,325 of the 1,099,412 digital records
  `[live 2026-11-17]`. Three have a route.

## Private ownership and public visibility

A [private ownership row](../../report-ownership-service/database/service.go#L59-L125)
with a nonempty owner suppresses the matching report from the public readers
and EPC worker. The 2026-10-17 [measured snapshot](../../report-listener/docs/brand-page-read-path-decision.md#L67)
recorded zero such rows. Current usage and any retroactive effects of
subscription are unverified. Keep paid monitoring and visibility decisions
independent. Use separately agreed private intake lanes and explicit
publication rules for public submissions.

## Patterns

`[inference]`

1. **A route has to work for the recipient.** A reviewed support address can
   still be a poor route to a product owner. A form or partner workflow can
   be the better private route if it reaches the relevant function.
2. **The transport ledger has much broader coverage than the outcome ledger.**
3. **Discovery is far ahead of delivery.** The engine can propose an operator,
   owner and regulator for a pothole. The ordinary report path uses email
   and EPC chat with the coverage limits listed above.
4. **Automatic brand notices batch by arrival time.** A recipient can get
   several separate messages about the same issue.
5. **A reply needs a disposition.** An operator can capture a decline in a
   case note. Accepted, disputed and works as designed still lack structured
   dispositions.
6. **Waves two and three are suggestions for an operator.** The case sender
   can address selected email-capable targets and CCs. The automatic wave
   executor and measured interested-party outcomes remain open.

## Questions to reason through

1. When should we use an official or private route, a public posting, or
   both?
2. What follow-up is appropriate after a dispute, a redirection or a
   monitored period with no observed response?
3. Who are the interested parties for a software fault, concretely, and what
   would each accept as a notice?
4. Should an owner be able to make public reports about itself private?
5. Which independent follow-up could establish a change at reasonable cost?
