# The problem and the thesis

**In short.** A person who sees a defect usually has no way to get that
observation to anyone who will act on it. CleanApp takes the observation,
works out who can act and who else has a stake, routes through the appropriate
channels, and preserves evidence of notice, response, and what changed.
CleanApp owns the routing and follow-up for the reporter. The idea has been
stable since 2013. The system has matured at collecting, describing, and
publishing observations. Grouping, valuation, notice, and outcome evidence
remain less developed.

## The claim

`[founder]`, 2026-11-17: superintelligence is "the ability to cut through the
spam filters and actually reach the people who are making these decisions so
[they] improve their processes faster, making EVERYONE better off, them
included". The examples given were the Google Ads bug and faults in LinkedIn,
Grok and Anthropic's products.

This wiki uses the word in that sense. It is narrower than the usual meaning
in AI research, a system that outperforms people at nearly everything. It is
more useful here, because it can be measured.

Why the claim holds `[inference]`:

1. **An organisation can only correct what it perceives.** Most knowledge of
   its defects is outside it, with the people who meet them (`THEORY.md` §1).
2. **Between those people and the decision stand filters:** support queues,
   forms behind a login, spam filters, triage that answers "intended
   behavior". They exist to protect attention from noise.
3. **More writers mean more noise, so the filters tighten.** People, and now
   machines, produce far more text than any organisation can read. True
   signals are lost more often. Feedback slows down while everything else
   speeds up.
4. **So the scarce capability is getting a true signal to the point of
   decision.** An organisation's ability to improve is bounded by how fast
   and how accurately it learns of its own faults. Whatever raises that bound
   for every organisation at once raises the intelligence of the whole
   system, whatever the ability of any single model.

Where AI comes in `[inference]`:

- **It makes the middle of the chain possible.** Reading a million reports,
  deciding which describe the same issue, working out who answers for it and
  what it is worth were not feasible at this scale before.
- **It also makes the noise.** So the hard step moves to the end of the chain:
  being believed and being attended to.
- **AI products are parties too.** A report about Claude, Grok or ChatGPT is an
  error signal for that system. A channel that carries it to the lab is part
  of how these systems get better. Machines can already file reports
  (page 08). Today a model that notices a fault in its own product has no
  channel to its maker.
- **CleanApp can be improved the same way.** A measured objective (page 11), a
  record of what was tried (this wiki), and agents that read both before each
  change: the system's own measurements steer its next change.

What "cutting through" has to mean `[inference]`:

A filter can be passed in two ways: by evading it, or by deserving to pass.
Evasion is what spam does. It is blocked harder each time, and it costs every
later notice its standing. The only durable way through is to be worth letting
through: evidence the recipient can check, a record of having been right,
independent sources honestly counted, and a posting that other people can
see. That makes noise CleanApp's own problem first. A sender whose mail is
mostly repeats and stubs is what the filters are for (pages 06, 07, 08).

What the claim predicts, and so how it can be tested:

- Comparable issues routed to an accountable role see earlier independently
  verified improvements.
- The party itself comes to value the channel: it answers, claims its
  address, pays.
- Where a public record reaches interested parties with the standing and
  ability to act, leaving a credible issue unresolved may become more costly.

Repair and reporting a repair are separate decisions. A party can fix quietly
or reply without making a change. CleanApp can recheck the underlying condition
and record what happened within the measured scope. Whether CleanApp caused
the change is a further question. [Notice and remediation](12-notice-and-remediation.md)

Page 11 defines separate measures for notice, confirmed reach, recipient
burden, and verified remediation.

## The problem in two cases

**Google Ads install-count displays.** `[live 2026-11-18]` The case studies
link twelve records from 2025-08-14 to 2026-10-10. Eight contain standalone
captures and four share one promotional composite. The captures show
displayed totals such as 6 billion, 1.43 billion, and 807 million. The contact
log shows relay acceptance for three outreach rounds on 2026-09-20, 10-09,
and 10-16. Original message contents and recipient attention are unknown.
`[founder]` Google's disclosure programme returned "intended behavior".
The account provides a reported disposition from that channel. Decision-owner
reach, the basis for the display, and any fix remain unverified in this record.
[Google Ads case and contact log](https://www.cleanapp.io/clustersamples#google-ads)

**Claude.** `[live 2026-11-17]` Anthropic's public briefing showed 7,144
grouped entries, including 120 first-hand entries. The verified company email
path uses one reviewed support address. The `[db 2026-11-11]` snapshot
contained no recorded acknowledgment from that route.

The Google Ads record supports a reported answer from an official programme.
The Anthropic record supports a transport route to support. Each leaves open
whether the issue reached the role able to decide on it.

## What CleanApp has said it is

| When | Source | Statement |
| --- | --- | --- |
| 2013-04 | `[2018 wiki]` | Started as "a wiki community around waste/hazard mapping" and "a defensive US patent project to make sure that BigTech wouldn't privatize citizen-city interactions". |
| 2018–2019 | `[2018 wiki]` | Three stages: reporting, analytics, response. "Waste/hazard/pollution data is also liability data." "To access CleanApp Analytics, interested parties pay a small subscription fee." A glossary defines the aggregate report (many reporters, one condition) and CleanAppTrack ("tracking CleanApp Responders' responsiveness"). |
| 2018–2019 | `[2018 wiki]` | Blockchain, researched "since 2015", is wanted for three things: rewards, storage, and being "more secure against censorship and regulatory capture". |
| 2025-12-18 | `WHY.md` | "Problems are everywhere, but signals don't travel." Brands are where "responsibility concentrates". Success is when "ignoring signals becomes reputationally and economically costly". "If CleanApp ever becomes 'just another inbox,' it has failed." |
| 2025-12-18 | `THEORY.md` | Clusters are "the fundamental unit of systemic meaning". Routing matters more than reporting. The world is "multi-party addressable … a network of responsible and interested parties". Incentives are "part of the system's physics". |
| 2026-03 | `docs/decisions/decision-log.md` | Defect-general, not hazard-specific. Notify plans in ranked waves to several parties. "CleanApp should learn who actually acts." |
| 2026-10 | `docs/evidence-first-release.md` | Lead with original observations. Never present source counts as affected-user totals. |
| 2026-11-17 | `[founder]` | See the next section. |

`[inference]` Five aims recur across this history. Preserve observations
from outside the institution. Publish suitable evidence. Route to parties
with a legitimate stake. Help those parties make decisions about risk and
repair. Reward useful contributions.

## The founder's positions, 2026-11-17

These correct or sharpen the documents above. They are recorded here because
they are not in the code.

1. **CleanApp does the escalating.** Sending the reporter to the company's own
   form is the friction CleanApp exists to remove. No design may hand work
   back to the reporter.
2. **Customers are both responsible parties and interested parties.** What is
   sold is valuable feedback that improves their results, with the cost of
   ignoring it and the benefit of acting on it made concrete. It is not
   complaints.
3. **A company that cannot be reached is the product, not an obstacle.** That
   nobody, including an AI model, can get feedback to a large company is the
   pain being solved.
4. **A "no" is not a resolution.** "Intended behavior" from an official
   disclosure programme is part of the problem.
5. **CleanApp is a public bulletin board.** Ethereum Plus Codes were
   introduced to give public notice.
6. **This is a research-grade problem**, worked on since 2013, and should be
   treated as one.
7. **Superintelligence, in this field, is reach.** See "The claim" above.

## Six problems inside the one problem

| # | Problem | The question | Page |
| --- | --- | --- | --- |
| 1 | Identity | When are two reports about the same thing? | 06 |
| 2 | Truth | What does a record establish, and how many independent people stand behind it? | 07 |
| 3 | Value | What is it worth, to whom, to know this and to fix it? | 05 |
| 4 | Responsibility | Who can fix it, and who else has a stake? | 03 |
| 5 | Notice | What was published, where was the evidence sent, and which decision roles can we show it reached? | 03, 04, 12 |
| 6 | Incentive | Why would any of them act, or pay? | 09 |

They depend on each other in a loop:

```
identity ──► truth ──► value ──► notice ──► incentive ──► outcome
    ▲                                                        │
    └────────────────────────────────────────────────────────┘
```

- Independent observers cannot be counted until reports are grouped by issue.
- A credible severe observation can warrant action with prevalence unknown.
  Estimate prevalence separately where the evidence supports it.
- A notice is only as strong as the claim it carries.
- A party is more likely to act when the expected benefit of repair exceeds
  its cost. Appropriate notice may change that calculation.
- An owner decision, an independent recheck, or a corrected source can each
  teach CleanApp something different about the earlier steps.

`[inference]` Each step is weak today, and the weakness passes down the chain:
see the table at the end of this page.

## Why this is research and not only engineering

`[inference]` None of the six has a known, general solution.

- **Identity.** There is no ground truth for "the same problem". Reports
  differ in language, medium and vantage point. The thing reported changes
  version, owner and location. New kinds of thing appear daily.
- **Truth.** Reporters are unverified. Once a report carries consequences,
  some people will file false ones, and some parties will want true ones
  discredited.
- **Value.** Worth depends on how many are affected, how exposed they are and
  what would have happened otherwise. None of the three is observed.
- **Responsibility.** Who answers for a defect is a legal and organisational
  fact that is often disputed, hidden or out of date.
- **Notice.** There is no agreed standard for when a third party's notice
  counts, in law or in public opinion.
- **Incentive.** The party best placed to fix a defect is often the one best
  served by silence.

What follows from treating it as research:

- State what is believed as a hypothesis that could be shown false (page 10).
- Measure before and after. Keep the failed attempts on record (this wiki).
- Calibrate every threshold against examples a person has judged.
- Prefer one issue carried all the way round the loop over a million records
  that stop at "stored".

## The idea against the system, today

| The idea says | Where | The system today | Page |
| --- | --- | --- | --- |
| Clusters are the unit of meaning. | `THEORY.md` §3, `INVARIANTS.md` §2 | Grouping by issue is done by hand: 31 cases `[db 2026-11-11]`. Notifications are batched by clock. | 06, 03 |
| Independent reports converge. | `THEORY.md` §3 | `independence` is always the word "unassessed". | 06, 07 |
| Routing is to many parties. | `THEORY.md` §4, decision 2026-03-12 | One wave, one address, three companies. | 03 |
| Not an inbox. | `WHY.md`, `INVARIANTS.md` §14 | The main live channel is email to support inboxes. | 03 |
| A public board gives notice. | `[founder]`, the `/epc` page | Addresses and delivery are built. The board shows sample rows. | 04 |
| The data is liability data. | `[2018 wiki]` | One sentence of dollar text per report since 2025-12. Never added up. | 05 |
| Credible notice makes a persistent defect harder to ignore. | `WHY.md` | Effect on remediation is unmeasured. The responsible party can repair without replying. | 09, 12 |
| The system learns who acts. | decision 2026-03-13 | 7 outcomes in the `[db 2026-11-11]` snapshot. The tracking build is deployed `[live 2026-11-18]`. Worker activity and recipient outcomes require separate verification. | 03, 11 |
