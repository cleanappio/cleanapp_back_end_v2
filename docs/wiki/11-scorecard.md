# Scorecard: measuring whether CleanApp works

**In short.** CleanApp works when a real defect that someone reported reaches
a person who can decide to fix it, gets an answer, and is fixed sooner than it
would have been. The scorecard follows every first-hand report through five
steps of that chain and counts the noise sent along the way. Each number is
one read-only query that anyone can read. As of 2026-11-17 no report is known
to have got past the second step, and the steps after it cannot yet be
measured in production. Making them measurable is the first job.

The tool is in [`tools/scorecard/`](../../tools/scorecard/README.md). The
measurements are in [`scorecard-ledger.csv`](scorecard-ledger.csv).

## What is measured, and why these four things

Page 01 states the claim: the capability that matters is getting a true signal
through the filters to the people who decide, so that they improve faster and
everyone gains. That turns into four quantities.

| Quantity | The question | Where it is measured |
| --- | --- | --- |
| **Loss** | Of the real defects people reported, how many never reached a person at the party responsible? | chain steps 1 to 3 |
| **Latency** | How long did it take? | the two `hours_` metrics |
| **Noise** | How much of what was sent was not a real, first-hand defect, or went to the wrong place? | the noise group |
| **Effect** | Was it answered? Was it fixed? | chain steps 4 and 5, the effect group |

Reaching the people who decide means low loss and low latency. The right to
pass a filter is earned by low noise. Everyone being better off shows up as
effect. A change that improves one of the first three by spoiling another is
not an improvement, which is why they are read together.

## The chain

The unit is the **eligible first-hand report**: the evidence label says the
author experienced the problem, the analysis is valid, and the report is
public (not quarantined, not an import barred from notices, not private to an
owner). The definition lives in one file,
`tools/scorecard/metrics/_eligible_firsthand.sql`, and every chain metric
includes it.

The cohort is every such report received in the **matured window**: from 37
days ago to 7 days ago. Each report has had at least a week for something to
come back.

| Step | Metric | A report counts when |
| --- | --- | --- |
| 1. Received | `chain_1_received` | it is in the cohort |
| 2. Sent | `chain_2_notified` | it appears in at least one email delivery, to anyone |
| 3. Seen | `chain_3_seen` | a person followed a tracked link in a notice that carried it, or a person replied to that notice |
| 4. Answered | `chain_4_answered` | a person replied to a notice that carried it |
| 5. Fixed | `chain_5_fixed` | a fix is on record |

Every step is reported per hundred received, so the steps can be read down as
one column: 100 received, so many sent, so many seen, and so on. The largest
drop between two steps is the bottleneck.

Rules that keep it honest:

- **A step counts only on evidence that came back.** "Sent" means the mail
  relay accepted the message. An automatic acknowledgement is not a person. A
  fix is never inferred from a send, from silence, or from a source closing.
- **One cohort for all five steps.** A share is never taken over a different
  set of reports from the step before it.
- **First-hand only.** Coverage and unknown records are not in the chain at
  all. Sending them is counted as noise.
- **"None to measure" is not zero.** When the cohort is empty the tool says
  so.

What the chain does not capture yet:

- **The unit should be the issue.** It is the report because the system has no
  automatic notion of an issue (page 06). Six copies of one post count six
  times.
- **"Seen by a person at the party" is weaker than "seen by a person who
  decides".** A support agent who opens a link counts. All three verified
  routes are support addresses.
- **Only email produces evidence.** An EPC notice cannot yet show that anyone
  read it. `firsthand_epc_notified` reports that path's coverage on the same
  cohort, and stops there.
- **Dashboards are not observed.** A customer reading its dashboard leaves no
  outcome.
- **Whether the reporter was told** is not recorded in a form that can be
  counted.
- **Cause.** "Fixed after a notice" is a fact. "Fixed because of a notice" is
  not measured (see the last section).

## Noise

| Metric | Reads as |
| --- | --- |
| `signal_share_of_sends` | Of everything sent in the last 30 days, the share that is first-hand. The rest is what a recipient can fairly call noise. |
| `notices_bounced` | Notices that could not be delivered. A high share means the address list is wrong. |
| `opt_outs_30d` | Recipients who asked not to be written to again. |
| `misrouted_30d` | Notices recorded as sent to the wrong party. |
| `audit_precision` | Kept by hand, below. |

Until there is a first full measurement the rule is: none of these may get
worse from one measurement to the next. Fixed limits are set after that, in
this section, with the date.

## Every metric

Generated by `scorecard.py catalogue`. A test fails if a metric is missing
here. "Matured" is 37 to 7 days before the measurement.

| Group | Metric | What it measures | Kind | Window |
| --- | --- | --- | --- | --- |
| chain | `chain_1_received` | Eligible first-hand reports received in the matured window. The denominator of every later step. | count | matured |
| chain | `chain_2_notified` | Share of those reports that appear in at least one email delivery, to any recipient, at any time. | ratio | matured |
| chain | `chain_3_seen` | Share with a viewed outcome (a tracked link in a notice was followed by a person) or a replied outcome (a person wrote back). An automatic acknowledgement does not count. | ratio | matured |
| chain | `chain_4_answered` | Share with a replied outcome: a message with no automatic markers came back to a notice that carried the report. | ratio | matured |
| chain | `chain_5_fixed` | Share with a fixed outcome on record. A fix is never inferred from a send or from silence. | ratio | matured |
| reach | `notices_answered` | Share of notices sent in the matured window where a person wrote back. | ratio | matured |
| reach | `notices_auto_acknowledged` | Share of notices sent in the matured window where an automatic acknowledgement came back, such as a help-desk ticket number. | ratio | matured |
| reach | `notices_bounced` | Share of notices sent in the matured window where a delivery failure came back. | ratio | matured |
| reach | `notices_seen` | Share of notices sent in the matured window where a person followed a tracked link or wrote back. | ratio | matured |
| reach | `notices_sent` | Notification messages sent in the matured window. Only messages sent with reply tracking on are counted, because only those can be tied to what came back. | count | matured |
| reach | `notices_silent` | Share of notices sent in the matured window where nothing came back within the waiting period, recorded as no_response. | ratio | matured |
| reach | `parties_notified` | Distinct recipient mail domains that were sent at least one report in the matured window. | count | matured |
| reach | `parties_seen_ever` | Distinct recipient mail domains with at least one viewed or replied outcome since records began. This number can only go up. | count | all time |
| latency | `hours_notice_to_first_response` | Median, over notices sent in the matured window that got a view, an automatic acknowledgement or a reply, of the time from sending to the first of those. A bounce is not a response. The second number is how many notices the median is taken over. | median | matured |
| latency | `hours_received_to_notice` | Median, over eligible first-hand reports received in the last 30 days that were sent, of the time from receipt to the first email delivery. The second number is how many reports the median is taken over. | median | last 30 days |
| noise | `misrouted_30d` | Misrouted outcomes recorded in the last 30 days. | count | last 30 days |
| noise | `opt_outs_30d` | Addresses added to the opt-out list in the last 30 days. | count | last 30 days |
| noise | `signal_share_of_sends` | Of report-recipient pairs sent in the last 30 days, the share whose report is labelled first-hand. The rest is coverage, unknown or unlabelled: material a recipient can fairly call noise. | ratio | last 30 days |
| coverage | `firsthand_received_30d` | Eligible first-hand reports received in the last 30 days. | count | last 30 days |
| coverage | `firsthand_total` | Reports whose evidence label is first-hand, before any eligibility filter. | count | all time |
| coverage | `parties_with_unsent_evidence` | Brands with at least @backlog_min eligible first-hand reports (5 unless set otherwise) of which none has ever been sent to anyone. The size of the "no door" problem. | count | all time |
| coverage | `reports_total` | Rows in the reports table, of every kind and visibility. | count | all time |
| public_notice | `epc_addresses_notified` | Distinct EPC addresses with at least one sent notice. | count | all time |
| public_notice | `epc_notices_held` | EPC notices with status held: queued, and waiting because the chat provider does not yet accept the recipient. | count | now |
| public_notice | `epc_notices_sent_30d` | EPC notices with status sent whose send time (or, for imported rows without one, creation time) falls in the last 30 days. | count | last 30 days |
| public_notice | `epc_notices_sent_total` | Rows in the EPC outbox with status sent, including notices imported from the earlier development system. | count | all time |
| public_notice | `firsthand_epc_notified` | Share of the eligible first-hand reports received in the matured window (the chain's cohort) for which a production EPC notice has status sent. The public-notice counterpart of chain_2_notified. | ratio | matured |
| identity | `cases_total` | Cases on record. A case is the only grouping of reports by issue that the system keeps. | count | all time |
| identity | `reports_in_cases` | Distinct reports attached to at least one case. | count | all time |
| effect | `fixed_total` | Distinct reports with a fixed outcome since records began. | count | all time |
| effect | `reports_resolved_30d` | Reports whose status became resolved in the last 30 days. For physical reports this is set when a later photo of the same place shows the problem gone, or by hand. | count | last 30 days |

## Numbers kept by hand

These cannot be read from the database. They go in the ledger with the status
`manual` and a note saying where they came from.

| Metric | Meaning | How to get it |
| --- | --- | --- |
| `verified_routes` | Parties with a reviewed way in. | Count the entries in `email-service/service/company_routing.go`. |
| `routes_to_named_role` | Verified routes that reach a named function that decides (a product owner, a security team, a trust and safety team) and not a general support desk. | Review each route. |
| `public_board_real` | 1 when the public noticeboard lists real notices, 0 when it shows sample rows. | Read `cleanapp-frontend/src/hooks/epc/useBrandData.ts`. |
| `epc_addresses_claimed` | EPC addresses taken over by the party they name. | 0 until claiming exists (page 04). |
| `audit_precision` | Of 50 notices sampled at random from the last 30 days, the share for which a person answers yes to all three: is it a real defect, was it sent to the right party, is it new to that party? | Monthly. Record the three counts in the note. |
| `same_issue_agreement` | Of 200 pairs of reports about one brand, the share on which two people independently agree that the pair is, or is not, the same issue. | Once, then whenever a matcher changes (page 06). |

## Where things stand

No full measurement has been taken. The tool needs read access to the
production database, which the session that wrote it did not have. What is
known comes from an earlier measurement and from the code.

| Metric | Value | Source |
| --- | --- | --- |
| `chain_1_received`, `chain_2_notified` | not yet measured | |
| `chain_3_seen` | 0 known | No viewed or replied outcome existed `[db 2026-11-11]`. Tracking was not live. |
| `chain_4_answered` | 0 known | same |
| `chain_5_fixed`, `fixed_total` | 0 | No fixed outcome existed `[db 2026-11-11]`. |
| `parties_seen_ever` | 0 | same |
| `notices_*`, both `hours_` metrics | not measurable | `notification_messages` is filled only once reply tracking is live (release `bc799bd`, staged). |
| `signal_share_of_sends` | not yet measured | For scale: first-hand records are 0.4% of five large scopes `[live 2026-11-17]`. |
| `cases_total`, `reports_in_cases` | 31, 760 | `[db 2026-11-11]` |
| `verified_routes`, `routes_to_named_role` | 3, 0 | code at `bc799bd` |
| `public_board_real`, `epc_addresses_claimed` | 0, 0 | code |
| EPC metrics, `firsthand_total`, `parties_with_unsent_evidence` | not yet measured | |

So the known position is: out of every hundred first-hand reports, none is
known to have been seen by a person at the party responsible. That is the
number to move.

## The loop

This is how the scorecard is used to improve CleanApp, by a person or by an
agent. Once a week, and after any release that is meant to move a number:

1. **Measure.** Run the tool with `--ledger docs/wiki/scorecard-ledger.csv`
   and commit the new rows.
2. **Check the noise group first.** If a noise number got worse, that is the
   next piece of work, whatever the chain says.
3. **Find the bottleneck.** Read the chain down. The step with the largest
   drop is where the next change goes. A step that cannot be measured counts
   as the bottleneck until it can.
4. **Predict before changing anything.** Add a row to the experiments table
   below: the change, the one metric it should move, the value now, the value
   expected, and the date it will be checked.
5. **Ship the change.**
6. **Check on the date.** Record what the metric did and mark the row
   confirmed, refuted or unclear. A refuted row stays. Update the inventory
   page for the mechanism that changed (pages 03 to 09).

Three rules:

- One metric per experiment. A change that is supposed to move everything
  tests nothing.
- No change to routing, notice, grouping or valuation without a row here
  first.
- Do not redefine a metric to make a number move. Add a new one and retire the
  old one by name.

Every session that works on those areas starts by reading the last
measurement and the open rows of this table. That is the whole mechanism: what
was measured decides what is built next, and what is built next is judged by
what is measured after.

## Experiments

| # | Opened | Change | Hypothesis | Metric | Before | Predicted | Check on | Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 2026-11-17 | Activate release `bc799bd`: signed reply addresses and tracked links on company notices. | Page 10, H1 and H7 | `notices_seen` | not measurable | Measurable. Fewer than 5 per 100, because all three routes are general support addresses `[inference]`. | 14 days after activation | open |

## Not built yet

- **A public, live scorecard.** Today a person runs the tool. The numbers
  should be served by the API and shown on the site, so that CleanApp's own
  record is on the board next to everyone else's.
- **The same numbers per party.** Grouped by recipient, the chain becomes a
  record of how each company responds: what the 2018 wiki called
  CleanAppTrack. That is a product for interested parties (page 09).
- **Cause.** To measure whether a notice makes a fix come sooner, hold back a
  random share of non-urgent notices for a fixed number of days and compare
  time to fix. Hazards to people are never held back. This needs fix
  detection first.
- **The issue as the unit**, once reports are grouped automatically.
