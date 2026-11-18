# Scorecard for notice, reach and remediation

CleanApp can document a credible notice, seek evidence that it reached an
accountable role, and independently check whether the condition changed.
A recipient may act without sending an update through CleanApp. Measure each
of these events on its own evidence. See
[Notice and remediation](12-notice-and-remediation.md).

The tool is in [`tools/scorecard/`](../../tools/scorecard/README.md). The
public historical measurements are in
[`scorecard-ledger.csv`](scorecard-ledger.csv).

Use the separate [handoff replay design](13-hybrid-superintelligence.md#test-whether-the-next-handoff-improves)
to test whether prior human judgments improve a fresh team's decisions.
Reach and remediation remain field outcomes.

## Measures for a future issue cohort

Use one human-validated issue for the outcome denominator. Agree on the
issue, enrollment date, intended accountable role, and follow-up window
before routing. For multi-party notice, retain each issue–role pair so
additional recipients cannot multiply the issue count. The working proposal
uses 14 days for confirmed reach and a predeclared, issue-appropriate window
for remediation checks.

| Measure | Evidence and denominator |
| --- | --- |
| **Public notice** | Issues with a verified public posting among enrolled issues eligible for publication. Keep the stable reference, date, published evidence and any corrections. Show publication eligibility and coverage. |
| **Private notice** | Share of enrolled issues with a documented attempt via a reviewed route. Record the issue, target role, time, channel and transport state. A relay acceptance proves acceptance by the relay. |
| **Confirmed decision-owner reach** | Enrolled issues with issue-specific acknowledgment or verified assignment to a role that can decide, prioritize, assign or fix the issue within the window. Keep issues with no route in the denominator. Show pending follow-up and unknown observation coverage alongside the rate. |
| **Disposition** | A documented response such as forwarded, accepted, disputed, works as designed or declined. A forwarding acknowledgment records progress. An accepted handoff by an accountable role records reach. |
| **Verified remediation** | Enrolled issues with a dated, independent check confirming the original condition improved within the checked scope. Record full repair and partial mitigation separately. Report verification coverage and inconclusive checks alongside the count. A verified repair can occur without a recorded acknowledgment or attribution to CleanApp. |

For a digital issue, a check might reproduce the original steps on the
relevant version. For a physical issue, it might compare dated observations
of the same site and condition. A recipient's claim, an automatic ticket
closure, and a successful check each have different evidence value. Store the
source, check date and result. Record a recurrence or correction when new
evidence warrants it.

Track time from enrollment to notice and confirmed reach. Include unresolved
and unobservable issues in the cohort view and label any latency summary that
uses only observed events. Compare changes using one primary outcome plus
recipient burden and cost. Use operator minutes and incremental cash or credit
consumption per confirmed handoff or verified remedy. Report the totals and
denominators as well as the unit costs.

## What the shipped queries can tell us

The report diagnostics use a shared
[`_eligible_firsthand.sql`](../../tools/scorecard/metrics/_eligible_firsthand.sql)
definition. It selects public reports with a first-hand evidence label and a
valid English analysis and excludes reports subject to suppression or private
ownership. The matured window selects reports received 37 to 7 days before
the measurement. Several reports can describe one issue. The evidence label
alone establishes neither accuracy nor independent corroboration.

- `chain_2_notified` finds an email delivery row. The row reflects transport
  acceptance.
- `chain_3_seen` finds a `viewed` or `replied` outcome.
  `chain_4_answered` finds a `replied` outcome. Tracked fetches and classified
  replies are engagement signals. The link handler uses a browser heuristic.
  The inbox worker can record one reply against each report in a digest,
  regardless of which issue the reply concerns. These outcomes carry no
  verified decision-role or issue-specific attribution. See the
  [link handler](../../report-listener/handlers/notification_links.go) and
  [inbox outcome writer](../../report-listener/database/inbox.go).
- `chain_5_fixed` finds a `fixed` outcome on record. It does not require an
  earlier send or answer, or independently validate the evidence.
- EPC outbox status, public posting and recipient attention are distinct
  events. The EPC diagnostics describe outbox activity. See
  [EPC and public notice](04-epc-public-notice.md).

Treat the existing `chain_*` metrics as parallel diagnostics on a report
cohort. Their outcome queries do not consistently enforce `--as-of` or link
successive events to the same notice or role. A historical run can count a
later send or reply. Each query also runs in a separate read-only database
session. A valid reach funnel needs a common snapshot, ordered and linked
events, and a bounded outcome cutoff. Remediation stays independently
measurable. See the [scorecard runner](../../tools/scorecard/scorecard.py) and
[queries](../../tools/scorecard/metrics/).

## Noise

| Metric | Reads as |
| --- | --- |
| `signal_share_of_sends` | Share of counted report–recipient delivery rows with a first-hand label. The delivery table retains the latest row per pair, so the share misses repeat sends. A label does not measure relevance or recipient burden. |
| `notices_bounced` | Recorded bounces among tracked notices. Interpret against the population with bounce observation available. |
| `opt_outs_30d` | Recorded opt-out addresses. Compare against distinct exposed recipients in the same period. |
| `misrouted_30d` | Recorded misroutes. Compare with notices audited for routing and report audit coverage. |
| `audit_precision` | Kept by hand, below. |

Use append-only delivery events when measuring duplicate or repeat outreach.
Compare rates with their denominators and eligible observation coverage.
Changing the volume or turning on telemetry can raise recorded incident
counts even when the underlying rate is steady. Treat disabled or unobserved
routes as unknown. In particular, `notices_silent` divides recorded
`no_response` outcomes by tracked notices. A disabled silence worker can
leave the numerator empty.

## Every metric

The IDs are from `scorecard.py catalogue`. A test checks that every metric ID
appears here. Read legacy labels such as “seen” and “answered” with the query
and telemetry limits above. “Matured” is 37 to 7 days before the measurement.

| Group | Metric | What it measures | Kind | Window |
| --- | --- | --- | --- | --- |
| chain | `chain_1_received` | Eligible first-hand reports received in the matured window. The shared denominator for these report diagnostics. | count | matured |
| chain | `chain_2_notified` | Share of those reports that appear in at least one email delivery, to any recipient, at any time. | ratio | matured |
| chain | `chain_3_seen` | Share with a recorded viewed or replied outcome. | ratio | matured |
| chain | `chain_4_answered` | Share with a recorded replied outcome, including digest-level replies associated with each included report. | ratio | matured |
| chain | `chain_5_fixed` | Share with a recorded fixed outcome, regardless of previous notice or verified reach. | ratio | matured |
| reach | `notices_answered` | Share of tracked notices sent in the matured window with a linked replied outcome. | ratio | matured |
| reach | `notices_auto_acknowledged` | Share of notices sent in the matured window where an automatic acknowledgement came back, such as a help-desk ticket number. | ratio | matured |
| reach | `notices_bounced` | Share of notices sent in the matured window where a delivery failure came back. | ratio | matured |
| reach | `notices_seen` | Share of tracked notices sent in the matured window with a linked view or reply event. | ratio | matured |
| reach | `notices_sent` | Notification messages sent in the matured window. Only messages sent with reply tracking on are counted, because only those can be tied to what came back. | count | matured |
| reach | `notices_silent` | Share of tracked notices sent in the matured window with a recorded `no_response` outcome. Interpret with silence-worker coverage. | ratio | matured |
| reach | `parties_notified` | Distinct recipient mail domains that were sent at least one report in the matured window. | count | matured |
| reach | `parties_seen_ever` | Distinct recipient mail domains associated with at least one recorded viewed or replied outcome. | count | all time |
| latency | `hours_notice_to_first_response` | Median, over notices sent in the matured window that got a view, an automatic acknowledgement or a reply, of the time from sending to the first of those. A bounce is not a response. The second number is how many notices the median is taken over. | median | matured |
| latency | `hours_received_to_notice` | Median, over eligible first-hand reports received in the last 30 days that were sent, of the time from receipt to the first email delivery. The second number is how many reports the median is taken over. | median | last 30 days |
| noise | `misrouted_30d` | Misrouted outcomes recorded in the last 30 days. | count | last 30 days |
| noise | `opt_outs_30d` | Addresses added to the opt-out list in the last 30 days. | count | last 30 days |
| noise | `signal_share_of_sends` | Share of latest report–recipient delivery rows in the window whose report is labeled first-hand. | ratio | last 30 days |
| coverage | `firsthand_received_30d` | Eligible first-hand reports received in the last 30 days. | count | last 30 days |
| coverage | `firsthand_total` | Reports whose evidence label is first-hand, before any eligibility filter. | count | all time |
| coverage | `parties_with_unsent_evidence` | Brands with at least `@backlog_min` eligible first-hand reports (5 by default) and no email delivery row for those reports. | count | all time |
| coverage | `reports_total` | Rows in the reports table, of every kind and visibility. | count | all time |
| public_notice | `epc_addresses_notified` | Distinct EPC addresses with at least one sent notice. | count | all time |
| public_notice | `epc_notices_held` | EPC notices with status held: queued, and waiting because the chat provider does not yet accept the recipient. | count | now |
| public_notice | `epc_notices_sent_30d` | EPC notices with status sent whose send time (or, for imported rows without one, creation time) falls in the last 30 days. | count | last 30 days |
| public_notice | `epc_notices_sent_total` | Rows in the EPC outbox with status sent, including notices imported from the earlier development system. | count | all time |
| public_notice | `firsthand_epc_notified` | Share of eligible first-hand reports received in the matured window with a production EPC outbox row marked sent. | ratio | matured |
| identity | `cases_total` | Cases on record. A case is the only grouping of reports by issue that the system keeps. | count | all time |
| identity | `reports_in_cases` | Distinct reports attached to at least one case. | count | all time |
| effect | `fixed_total` | Distinct reports with a fixed outcome since records began. | count | all time |
| effect | `reports_resolved_30d` | Reports marked resolved in the last 30 days, including model-assisted photo comparisons or manual decisions. Verify the condition separately. | count | last 30 days |

## Numbers kept by hand

Record approved aggregate measurements in the public ledger with status
`manual` and a source note. Keep case-level observations, private evidence
and recipient information in the access-controlled operating record.

| Metric | Meaning | How to get it |
| --- | --- | --- |
| `verified_routes` | Parties with a reviewed way in. | Count the entries in `email-service/service/company_routing.go`. |
| `routes_to_named_role` | Reviewed routes to an identified decision function such as a product, security, trust and safety, or facilities team. | Verify the role, scope and route. A reviewed route can exist before any issue-specific evidence of reach. |
| `public_board_real` | 1 when the public noticeboard lists real notices, 0 when it shows sample rows. | Verify the rendered production board and record the date and source. |
| `epc_addresses_claimed` | EPC addresses taken over by the party they name. | 0 until claiming exists (page 04). |
| `audit_precision` | Of 50 notices sampled at random from the last 30 days, the share for which a person answers yes to all three: is it a real defect, was it sent to the right party, is it new to that party? | Monthly. Record the three counts in the note. |
| `same_issue_agreement` | Legacy pooled reviewer agreement on pairs labeled as the same or a different issue. | Report agreement by pair class and false merges separately when testing a matcher (pages 06 and 10). |

## Where things stand

The ledger contains a historical snapshot from November 2026. The first-hand
cohort has no measured reach denominator. The recorded counts describe
available evidence for the dates shown.

The 2026-11-17 release receipt records deployment of `bc799bd` to the report
listener and email service, including the inbox schema migration. On
2026-11-18 both public version endpoints reported that commit. This confirms
the deployed build. Active inbox polling, tracked sends, full observation
coverage and verified decision-owner reach remain unverified in this review.
[Listener version](https://live.cleanapp.io/version) ·
[Email version](https://email.cleanapp.io/version)

| Metric | Recorded value | Scope and date |
| --- | --- | --- |
| `chain_1_received`, `chain_2_notified` | Unmeasured | No cohort run in the ledger. |
| `chain_3_seen`, `chain_4_answered`, `chain_5_fixed` | Unknown rates | Zero relevant outcomes recorded on 2026-11-11. Cohort denominators absent. |
| `parties_seen_ever`, `fixed_total` | 0, 0 | Recorded outcome counts on 2026-11-11. |
| `notices_*`, both `hours_` metrics | Unmeasured | Verify telemetry for each channel before interpreting ratios or elapsed time. |
| `signal_share_of_sends` | Unmeasured | Public evidence API counts describe grouped briefing entries in selected company scopes. They do not provide the outbound denominator. |
| `cases_total`, `reports_in_cases` | 31, 760 | Database snapshot on 2026-11-11. |
| `verified_routes`, `routes_to_named_role` | 3, 0 | Code at `bc799bd`. All three reviewed routes are general support destinations. |
| `public_board_real`, `epc_addresses_claimed` | 0, 0 | Sample EPC board and implementation review on 2026-11-17. |
| EPC metrics, `firsthand_total`, `parties_with_unsent_evidence` | Unmeasured | No results in the ledger. |

## The loop

1. **Measure coverage.** Establish which routes and dates have functioning
   telemetry. Record incomplete channels, unavailable observations, empty
   cohorts and observed outcomes separately.
2. **Identify the loss.** Review the evidence on intake, notice, reach and
   remediation. A gap in measurement calls for a coverage check. A
   documented misroute calls for a routing change.
3. **Write the prediction.** Give each experiment a steward, a fixed cohort,
   an outcome window, one primary measure, baseline or comparator, delivery
   burden, estimated cost and a review date.
4. **Preserve the result.** Record the outcome, supporting evidence and
   remaining uncertainty. Append dated measurements and corrections. Record
   the cohort, cutoff, source and metric version with each run. Use a new
   metric name or version when definitions change.
5. **Publish reviewed aggregates.** Keep historical rows. Keep restricted
   evidence and operational details in the private record and share only
   approved aggregate results.

## Experiments

| # | Opened | Change | Hypothesis | Metric | Before | Predicted | Check on | Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 2026-11-17 | Ship signed reply addresses and tracked links for company notices in `bc799bd`. | Engagement telemetry for H1 and H7 in the original plan | `notices_seen` | Unmeasured | Original prediction was fewer than 5 recorded engagements per 100 tracked notices. | Originally 14 days after activation | Build deployed. Engagement unmeasured. A diagnostic follow-up would use post-activation notices with verified telemetry and a fixed follow-up window. |

New case studies and outreach experiments are deferred pending design
discussion. The proposed issue cohort above is a measurement design.

## Open measurement work

- **Reliable historical runs.** Apply event cutoffs, link outreach milestones
  to the same issue and route, and use one snapshot. Add fixtures for events
  after the cutoff, unlinked fixes, digest replies and disabled telemetry.
- **Issue-level observations.** Validate issue membership and distinguish
  accountable handoff from engagement. Record reporter updates and
  independent remediation checks.
- **Incremental impact.** Establish whether plausible routing or publication
  changes improve reach or time to remediation through a controlled
  comparison. Continue ordinary notice and urgent handling. A repair
  observed after a notice establishes timing. Attribution requires evidence
  of the mechanism or a credible comparator.
- **Public reporting.** Review definitions, coverage, uncertainty and
  aggregation before serving live rates or comparing organizations.
