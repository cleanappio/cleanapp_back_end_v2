# CleanApp wiki: what has been tried, and what is still unsolved

**The problem this wiki tracks.** Someone notices a defect: a hazard in a
street, a fault in a product. CleanApp's job is to preserve that observation,
route it to people who can act, keep evidence of notice and response, and
check what changed for the reporter and others affected.
This wiki records each approach CleanApp has taken to that problem, what it
produced, and what is still open.

**Why it matters.** An organisation can only fix what it learns about. The
founder's claim is that the capability worth calling superintelligence is the
one that gets a true signal through the filters to the people who decide, so
that they improve faster and everyone gains (page 01). Page 11 measures how
close CleanApp is to that. Page 12 describes how a public record and direct
notice might change incentives, and how to observe remediation independently.
Page 13 defines how human–agent handoffs compound knowledge and how to test
whether they improve the next decision.

**Why it exists.** `WHY.md` and `THEORY.md` say what CleanApp believes. The
decision log says what was decided since March 2026. The architecture
documents say how the system is built. None of them says what was attempted,
what was switched off, and what the results were.

**Who wrote it.** Claude, on 2026-11-17, at the founder's request, from the
code, the commit history, the documents in the repositories, the 2018 wiki and
the live public API. It is one reader's understanding and is meant to be
corrected. Where the code is silent, the page says so.

## Pages

| Page | Question it answers |
| --- | --- |
| [01 Problem and thesis](01-problem-and-thesis.md) | What is CleanApp trying to do, and how far is the system from its own stated idea? |
| [02 Timeline](02-timeline.md) | What was built when, from 2013 to today? |
| [03 Notice and routing](03-notice-and-routing.md) | Every way CleanApp has tried to tell someone about a report, and what came back. |
| [04 EPC and public notice](04-epc-public-notice.md) | The public bulletin board: the idea, what is built, what is missing. |
| [05 Quantification](05-quantification.md) | Every way CleanApp has put a number or a price on a report. |
| [06 Identity and clustering](06-identity-and-clustering.md) | Every way CleanApp decides that two reports are the same thing. |
| [07 Evidence and truth](07-evidence-and-truth.md) | What a record claims, what it does not, and who gets to answer. |
| [08 Intake and corpus](08-intake-and-corpus.md) | Where reports come from and what the database actually holds. |
| [09 Incentives and buyers](09-incentives-and-buyers.md) | Who gains from reporting, from fixing and from knowing. |
| [10 Open problems](10-open-problems.md) | Where the code and the idea disagree, working hypotheses, and what to measure next. |
| [11 Scorecard](11-scorecard.md) | The numbers that say whether CleanApp works, where they stand, and the loop for improving them. |
| [12 Notice, response and remediation](12-notice-and-remediation.md) | What notice can establish, why a party might fix silently, and how CleanApp can verify a change. |
| [13 Hybrid superintelligence](13-hybrid-superintelligence.md) | How people, agents, and institutions learn from each handoff and how to test that learning. |
| [Glossary](glossary.md) | Terms, including the 2018 vocabulary. |

Start with pages 01 and 13 for the thesis and learning loop. Pages 12 and 11
cover incentives and measurement.

## Conventions

**Status words.** Every mechanism carries one.

| Word | Meaning |
| --- | --- |
| live | Runs in production today. |
| on demand | Runs only when a person asks for it. |
| built, off | Code exists and is switched off or not deployed. |
| write-only | Results are stored and nothing reads them. |
| offline | A tool someone runs by hand, not a service. |
| retired | Was live, no longer called. |
| sample data | A public page that shows invented rows. |
| plan | Written down, not built. |

**Source tags.** A claim with a file path or a commit hash comes from the code.
Other claims carry a tag.

| Tag | Meaning |
| --- | --- |
| `[live YYYY-MM-DD]` | Read from the public API, a public version endpoint, or the deployed site on that date. |
| `[db 2026-11-11]` | Measured on the production database and recorded in `docs/architecture/notification-outcomes.md`. Not re-measured here. |
| `[2018 wiki]` | From the GitHub wiki of `cleanappio/wiki` (81 edits, 2018-07-09 to 2019-12-21). |
| `[founder]` | Said by the founder on 2026-11-17. Not in the code. |
| `[inference]` | My conclusion. Could be wrong. |
| `[not verified]` | I could not check it from here. |

Commit hashes are from `cleanapp_back_end_v2` unless marked `(frontend)`.
Dates are year-month-day.

## Keeping it current

- A change to a mechanism listed here updates its entry in the same pull
  request: status word, date, and one line on what changed.
- When something is switched off or abandoned, keep the entry and write down
  why. The failures are the most useful part.
- Preserve dated measurements in the ledger. Record the cohort, window,
  metric definition or revision, and evidence source with new runs. Link
  the current result from prose and keep corrections traceable.
- For a live claim, record the check date and what was verified. A build
  revision, a worker processing traffic, and a real recipient outcome need
  different evidence.
- Write hypotheses in page 10. Record the owner, next check, expected result,
  and observed outcome for an experiment that changes routing or measurement.

## Internal operating record

This wiki is in a public repository. Use the private **CleanApp Internal
Operating Record** in ChatGPT Pages for route details, contacts, correspondence,
case decisions, rechecks, release receipts, and commercial evidence. Authorized
operators can find the record by title or get access from its owner. Keep
restricted originals in approved storage and link to them inside the record.
Publish reviewed summaries and appropriate aggregate measurements here.

For each issue, record who will follow up and when. Keep notice receipts,
decision-owner acknowledgment, reported remediation, and independent
observations distinct. The public record should carry corrections and
uncertainty in the same place as the original observation.

## What this wiki does not know

- **Anything before December 2022 beyond the 2018 wiki.** The white paper, the
  patent file, the blog and the Medium posts it links are not in the
  repositories.
- **Customers and revenue.** The code has plans and prices. It has no record
  of who pays.
- **The current state of the production database.** Use the date on each
  `[db]` observation. The November 11 figures are a historical snapshot.
- **Case-study source packages.** The public [case studies](https://www.cleanapp.io/clustersamples)
  and their [published data](https://www.cleanapp.io/clustersamples/samples.json)
  are available. The October 18 release materials and offline matching
  experiments live outside the product repositories.
- **What happened outside the system:** calls, meetings, disclosures, replies
  sent to people rather than to `info@cleanapp.io`. The Google Ads disclosure
  in page 01 is the example.
