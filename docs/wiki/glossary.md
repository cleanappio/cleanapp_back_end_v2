# Glossary

Terms as this wiki uses them. Where the code uses a different word, it is
given in `code font`.

## Parties

| Term | Meaning |
| --- | --- |
| Reporter | The person or program that made the observation. |
| Responsible party | Whoever can fix the defect: a company, an owner, an operator, a public body. |
| Interested party | Whoever has a stake in the defect or in the answer without being able to fix it: a regulator, an insurer, a journalist, a researcher, a competitor, an investor, other users. `THEORY.md` §4. |
| Brand | The code's word for the subject of a digital report: a company or a product (`brand_name`). |
| Area | A polygon on the map with registered contacts (`areas`, `contact_emails`). |
| Owner | A customer that has registered a brand or an area (`customer_brands`, `customer_areas`). Can make matching reports private. |

## Records

| Term | Meaning |
| --- | --- |
| Report | One submission or one imported item. Numbered by `seq` inside the system and by an opaque `public_id` outside it. |
| Physical / digital | A report about a place, or about a product or service. One pipeline handles both. |
| First-hand, coverage, unknown | `report_kind`. Whether the author experienced the problem, described someone else's, or cannot be told. |
| Source key | The identity of the item a report was imported from: a post, a URL, an ID. Two reports with one source key are one import seen twice. |
| Issue | One underlying problem, however many reports describe it. The system has no automatic notion of this. `issue_id` today identifies a case family or, outside a case, a single source. |
| Case | A group of reports that a person has put together as one issue (`cases`, `case_reports`). |
| Cluster | Used loosely in the documents for any group of similar reports. In the code: a pairwise match between photos (`report_clusters`), a saved map selection (`saved_clusters`), or a hypothesis computed on request. |
| Independence | Whether two reports come from people who observed separately. Always "unassessed" today. |
| Lane, quarantine | Where machine input is held until its source is trusted. Quarantined reports are stored and not shown. |

## Notice

| Term | Meaning |
| --- | --- |
| Notice | A statement to a party that a report or an issue exists. |
| Private notice | One delivered to an address the party controls: email, a form, a dashboard. |
| Public notice | One posted where anyone can see that it was posted, about what, and when. |
| Route | A known way to reach a party. A verified route has been reviewed by a person (three exist). |
| Contact observation | A contact found by discovery. Not yet a recipient. |
| Escalation target | A contact that has been ranked for a particular case. |
| Notify plan, wave | The ranked list of whom to tell, in rounds: those who can act, then authorities and oversight, then other stakeholders. |
| Outcome | What is known to have happened after a notice: sent, bounced, viewed, acknowledged, replied, out of office, no response, misrouted, fixed (`notify_outcomes`). |
| Endpoint memory | What the system remembers about an address: how often it bounced, acknowledged, fixed (`contact_endpoint_memory`). |

## EPC

| Term | Meaning |
| --- | --- |
| EPC | Ethereum Plus Code (the public page) or Ethereum Place Code (`OVERVIEW.md`). A public address for a brand or a place. |
| Key | The text an address is derived from: `cleanapp/<brand>`. |
| Campaign | A batch of notices sharing a key prefix and a template (`epc_campaigns`). |
| CREATE3 | A method that fixes a contract's address from a key alone, so the address is known before the contract exists. |
| Factory | The contract that maps keys to addresses and could later deploy a contract at each. |
| Outbox | The durable queue of notices and their receipts (`epc_outbox`). |
| Blockscan Chat | The messaging service that carries today's notices to an address. |
| Board, noticeboard | The public list of notices. Planned at `/epc`; sample data today. |

## Rewards

| Term | Meaning |
| --- | --- |
| KITN | CleanApp's token. Earned per report, match, vote and referral. |
| Sort game | A mobile game in which people rate a report's urgency and flag spam. |

## Intake

| Term | Meaning |
| --- | --- |
| Wire | The single intake interface for machines (`cleanapp_wire_v1`). |
| Fetcher | A registered machine source with a key. |
| Indexer | A program CleanApp runs to collect posts from a platform. |

## Measurement

| Term | Meaning |
| --- | --- |
| Scorecard | The shipped diagnostic queries and the proposed outcome measures described in [page 11](11-scorecard.md). |
| Eligible first-hand report | The unit of the shipped report diagnostics. Eligibility uses an evidence label, valid analysis, public visibility and notification rules. |
| Chain | The shipped `chain_*` queries are parallel diagnostics on one report cohort. Later outcomes can exist without earlier milestones or a common notice. |
| Matured window | The legacy SQL cohort window from 37 to 7 days before measurement. The follow-up window for proposed issue cohorts is specified at enrollment. |
| Confirmed decision-owner reach | Issue-specific acknowledgment or verified assignment to a role with authority to decide, prioritize, assign or fix the issue. |
| Verified remediation | A documented independent check that the reported condition changed. Recipient participation is optional. |
| Ledger | The public `scorecard-ledger.csv` holds dated aggregate measurements. The private operating record holds case evidence and restricted observations. |
| Experiment | A documented hypothesis, cohort, comparator, prediction, evidence and outcome with a steward and review date. |

## The 2018 vocabulary

From the earlier wiki. Most of these ideas are still open.

| 2018 term | Meaning then | Nearest thing today |
| --- | --- | --- |
| Aggregate CleanApp Report | "the aggregate of multiple CleanApp Reports from multiple CleanApp Reporters" | a case |
| Composite CleanApp Report | several reports of one condition from one reporter | nothing |
| Chrono CleanApp Report | progression of a condition over time | nothing; photo matching is the closest |
| Enterprise CleanApp Report | "not published … directed to CleanApp Responders within a particular organization" | owner-private reports |
| Anon CleanApp Report | scrubbed of the reporter's identity | face blurring; opaque IDs |
| CleanAppTrack | "tracking CleanApp Responders' responsiveness" | outcomes |
| CleanApperRating | "peer-reviewed accuracy, effectiveness, contribution score" | nothing live; `wire_reputation` is an empty table |
| CleanApp Responder | whoever responds, person or machine | responsible party |
| CleanAppMap | public map of reports and their metadata | the globe |
