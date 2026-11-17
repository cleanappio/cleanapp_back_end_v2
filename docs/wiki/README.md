# CleanApp wiki: what has been tried, and what is still unsolved

**The problem this wiki tracks.** Someone notices a defect: a hazard in a
street, a fault in a product. CleanApp's job is to take that observation off
their hands, get it to the parties responsible for it and to the parties with
a stake in it, make the notice impossible to deny, and find out what they did.
This wiki records each approach CleanApp has taken to that problem, what it
produced, and what is still open.

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
| [Glossary](glossary.md) | Terms, including the 2018 vocabulary. |

Short on time: read 01, then 10.

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
| `[live 2026-11-17]` | Read from the public API or the public site on that date. |
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
- When a number is re-measured, replace it and change the date in its tag.
- Opinions go under "Questions" or in page 10, never in an inventory.

## What this wiki does not know

- **Anything before December 2022 beyond the 2018 wiki.** The white paper, the
  patent file, the blog and the Medium posts it links are not in the
  repositories.
- **Customers and revenue.** The code has plans and prices. It has no record
  of who pays.
- **The current state of the production database.** Numbers tagged `[db]` are
  six days old.
- **The source of the case-studies page** (`cleanapp.io/clustersamples`). It
  is not in the repositories I could read.
- **What happened outside the system:** calls, meetings, disclosures, replies
  sent to people rather than to `info@cleanapp.io`. The Google Ads disclosure
  in page 01 is the example.
