# Hybrid superintelligence

CleanApp's thesis is that people, agents, and institutions can collectively
notice and correct problems that each would struggle to address alone. The
test is whether more credible observations reach roles with authority to act,
and whether comparable problems improve sooner. Each handoff should improve
the decision at hand and leave evidence that improves the next team's
decision. [The problem and the thesis](01-problem-and-thesis.md)

People contribute context, judgment, permission, authority, and independent
verification. Agents can be observers too. They can preserve what happened in
a tool or browser, compare observations, assemble evidence, and find a route
at a scale people cannot maintain by hand. Institutions decide and act in
their own workflows. A comparable recheck can show what changed even when
the institution stays silent. [Notice, response and remediation](12-notice-and-remediation.md)

## What each handoff teaches

| Handoff | Evidence passed forward | What the next team learns |
| --- | --- | --- |
| Observer to agent | Original observation, source and author type, date, place or product scope, reproduction conditions, uncertainty, and handling permission. | Which details made the observation reproducible and what is still missing. Related reports can contribute fresh evidence and keep their own provenance. |
| Agent to human reviewer | A concise packet, candidate matches and routes with supporting sources, a proposed action, and unresolved questions. The reviewer records what they accepted, changed, or rejected and why. | Which matching and routing judgments held up, how to distinguish a duplicate from corroboration, and when to ask a human or leave the issue unresolved. |
| Reviewer and agent to decision owner | A reviewed packet with a specific ask, the basis for the target role's authority, and a dated record of the approved route and payload. Record transport, any verified assignment or acknowledgment, and the disposition separately. | Which role can act on this kind of issue, what information it used, and where a seemingly valid channel stalled or needed redirection. A relay receipt carries less evidence than a verified assignment. |
| Owner or independent observer to verifier | The owner's claim or disposition when available, plus a comparable recheck with its date, method, scope, result, and evidence. | Which checks establish repair, mitigation, recurrence, or continued uncertainty, and how to update the reporter in each situation. Record who acted and CleanApp's contribution only when supported. |
| Verifier and steward to the next team | A dated, reviewed lesson linked to evidence, with an applicable scope, exceptions, and a freshness check. | What to reuse, what to verify again, and when to disregard an earlier conclusion. The next team records which lesson it used and what it changed. |

The record must work across sessions and agents. For a consequential
handoff, record the decision, source, reviewer, and next action. Keep an
approved lesson small enough to retrieve with its supporting evidence.
Record a correction as a new event that supersedes an earlier judgment.
Promote repeated evidence to a matching, routing, or verification rule only
after a human reviews its scope. Access and permission to publish or contact
a party are specific to each issue. The internal operating record holds
restricted evidence and the wiki holds reviewed, publishable findings.
[Operating record guidance](README.md#internal-operating-record)

A passed ownership check can improve a future route without proving the issue
was fixed. An unsuccessful route can teach the next agent to verify a
different function. A recent correction should survive a fresh session and
stay within its supported scope. More retrieved text, more submissions, and
more replies provide activity counts. The compounding claim depends on
better decisions and independently observed outcomes.

## Test whether the next handoff improves

**Offline design.** Run each held-out synthetic scenario twice in fresh
sessions. Give both runs the same model, tools, reviewer access, time budget,
applicable policies, and evidence available at the moment of decision. Give
one run the reviewed handoff lessons. Keep outcome labels and close variants
of the evaluation scenarios out of the lesson set. Include new issue
families, stale and conflicting routes, duplicate and corroborating
observations, unavailable owners, and ambiguous rechecks. Grade outputs
against a fixed rubric in random order, with graders unaware of which run
had the lessons.

**Primary measure.** Across all eligible scenarios, compare how often the
team makes the correct next decision within the time budget. A ready-to-route
issue needs a source-grounded packet and an appropriate role. An issue with
insufficient evidence or no verified route needs a justified hold or next
check. Report these results by category and include routing coverage so a
model that always holds cannot look successful. Track evidence fidelity,
false merges, misroutes, unsupported claims, permission errors, reviewer
minutes, and cost per correct handoff alongside the primary measure.

Require evidence of transfer. A known correction should improve a fresh
handoff within its scope. An expired, conflicting, or out-of-scope lesson
should trigger verification or be set aside. If the gain disappears on
unseen issue families, takes more reviewer work than it saves, or comes
with more unsafe or unsupported decisions, revise the learning process.

**Later outcome test.** Once case work is authorized, compare the ordinary
workflow with the same workflow plus one reviewed learning step. Use similar
eligible issues and the same follow-up effort. Measure issue-specific
confirmed reach within the agreed window, time to independently verified
remediation, and coverage of both measures. Keep issues without a route in
the reach denominator. Record also whether the reporter got a useful update.
Compare recipient burden, operator time and cost. Keep normal and urgent
notice in place. This tests whether improved
handoffs move the real-world outcomes, beyond what offline replay can show.
[Scorecard](11-scorecard.md) · [Open problems](10-open-problems.md)

Both tests are proposals. Case studies and new outreach remain deferred.

The [agent discovery and growth workstreams](14-agent-discovery-and-growth.md)
put this learning loop into practice. Each team records what the next run
can use and checks whether it made a better decision.
