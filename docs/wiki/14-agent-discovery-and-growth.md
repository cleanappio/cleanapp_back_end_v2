# Agent discovery, reporting and growth

CleanApp should be easy to find when an agent encounters a real physical or
digital problem during its work. It should help the operator send a useful
observation with clear permissions, then bring a useful decision back to the
same workflow. Repeated, independently useful contributions grow the
observation network. [Hybrid superintelligence](13-hybrid-superintelligence.md)

## Discoverability we can verify

| Path | Verified on 2026-11-18 | Next check |
| --- | --- | --- |
| Public site | The homepage links to `/agents`. The guide links to `/agents.txt`. The sitemap lists the guide and the public crawler instructions advertise it. | Check the anonymous route from homepage to guide to machine instructions. Verify records against the deployed endpoint and public package versions. |
| Tool catalogs and packages | The official MCP Registry returns the active CleanApp 1.1.0 entry by brand. The OpenClaw skill and CLI are published. The live, no-key MCP endpoint initialized and returned five tools. | Check catalog lookup, registered endpoint and live tool inventory from a fresh connection. Test where a host installs the tool and where the operator must configure it. |
| Claude setup | The repository marketplace now includes a CleanApp plugin pointed at the same public MCP endpoint. | Verify installation and live tool selection from a clean Claude session. Check official directory placement separately. |
| Brandless discovery | Sampled registry and web queries about reporting a problem or an accessibility barrier did not surface CleanApp. These are dated spot checks. | Repeat a small set of unbranded intent queries in selected search surfaces and supported hosts. Record query, surface, date, visibility and whether the result could be connected. |

[Agent guide](https://www.cleanapp.io/agents) ·
[Machine instructions](https://www.cleanapp.io/agents.txt) ·
[Official MCP Registry lookup](https://registry.modelcontextprotocol.io/v0.1/servers?search=cleanapp&version=latest) ·
[Published packages](https://github.com/cleanappio/cleanapp-agent001/blob/main/distribution/README.md) ·
[Claude marketplace plugin](https://github.com/cleanappio/cleanapp_back_end_v2/pull/235) ·
[Glama listing](https://glama.ai/mcp/connectors/io.github.cleanappio/cleanapp) ·
[ProtocolProbe listing](https://protocolprobe.com/mcp/servers/io.github.cleanappio/cleanapp)

Keep the site guide and machine-readable instructions canonical. Match
category terms to real tasks such as reporting a reproducible application
failure, an accessibility barrier or damaged infrastructure. Keep titles,
descriptions, setup steps, and permission semantics consistent wherever they
are published. Start with tested install paths for hosts used by participating
workflows. Reviewed host listings can extend distribution after activation is
reliable. A registry entry establishes a lookup path. Cold discovery,
connection, tool selection, and repeat use each need their own evidence.
For example, start a fresh session and ask where a browser agent can report
a reproducible blocker with a receipt. Record whether CleanApp appeared
without being named, the path it offered, and which steps the host supports.
Use a bounded set of agreed prompts so changes can be compared over time.

## The first useful report and the reason to return

1. **Offer help when the workflow encounters a real blocker.** Start with
   opt-in browser QA and accessibility workflows. On 2026-11-18 the public
   guide invited agents to report what they or their users observe, and the
   live preparation tool described its input as user-provided details.
   Align those descriptions so an agent can offer a factual draft based on
   an actual observation made during authorized work.
2. **Make the first report easy to approve.** Capture only the relevant
   steps, result, scope, source, and uncertainty. Show the exact draft and
   the permissions for sending, public visibility, and notices. Keep
   retries tied to the same draft or intent. Preserve fresh corroboration
   and suppress repeat outreach for evidence already received.
3. **Return the decision to the original task.** Keep the receipt private.
   Bring back the recorded decision and a useful next step, including why
   something was held. Keep intake, publication, transport, accountable
   reach, and verified remediation distinct.
4. **Shorten installation for the hosts people actually use.** Add tested
   setup links and a no-submission connection check to `/agents`. Use the
   existing MCP, HTTPS, and portable skill paths. Record where operators
   drop out before adding more listings or building new receipt delivery.

[Current permissions and receipt contract](../agent-onboarding.md) ·
[Live agent instructions](https://www.cleanapp.io/agents.txt)

## Measures for agent growth

**Primary measure: 30-day repeat contribution.** The denominator is opted-in
workflows whose first qualifying submission has completed a 30-day
observation window and the declared review lag. The numerator is those that
submitted a second qualifying contribution within 30 days of the first.
Count each workflow once. A human reviewer must confirm a distinct issue
or materially new corroboration. Exact replays, repeated evidence and
synthetic traffic do not qualify. Report the raw numerator and denominator,
cohort dates, review coverage, and pending judgments by host when the slice
is large enough. Report separately how often a second eligible reporting
opportunity was observed, including unknown opportunity coverage.

Use an opted-in cohort and a scoped workflow key in the internal operating
record. The public intake uses a shared anonymous actor. Its `agent_name`
is self-declared and its `source_id` identifies an individual submission
and its retry. Request counts, IP addresses, installs and catalog views
cannot establish unique or repeat reporting workflows.
[Public intake](../../report-listener/handlers/public_agent_ingest.go)

| Question | Measure |
| --- | --- |
| Can a fresh agent find and connect? | Cold intent checks that identify CleanApp over checks attempted, successful connections over attempted connections in enrolled hosts, and first valid previews over eligible observed blockers. |
| Does the work survive approval and review? | Exact approvals per preview, retained receipts per approval, human-qualified contributions per unique submitted receipt, and outcomes returned to the operator per recorded disposition. |
| Can it reach someone and improve the condition? | Confirmed issue-specific decision-owner reach and independently verified remediation, with observation coverage. These are separate from receipt retrieval. |
| Is more activity worth the burden? | Duplicate receipts per reviewed receipt, repeat outreach, misroutes per audited notice with audit coverage, permission incidents, reviewer minutes, and incremental cash and credits per useful contribution. |

The 30-day rate is a metric definition. Publish a baseline only after an
opted-in cohort matures. Add measures with their denominator, method,
evidence source and version. [Scorecard](11-scorecard.md)

## Workstreams and recurring work

| Team | Owns | Evidence it leaves for the next team |
| --- | --- | --- |
| Discovery and installation | Public guide, registry, host setup, tool inventory, version drift and cold-intent checks. | Dated pass, failure or unknown by surface, plus the smallest actionable fix. |
| Activation and retention | A consented reporting cohort and the path from observed blocker through preview, approval, receipt and repeat contribution. | Stage counts, review coverage, cost, and the first measurable drop. |
| Reach and remediation | Evidence from authorized notices and independent verification coverage. | Which role could act, where the handoff stopped, and what a comparable recheck showed. |
| Learning and quality | Human corrections, small scoped lessons and held-out handoff replay. | What a later agent should retrieve, how it used it, and whether it performed better. |

Assign each queued task a steward, a source, one expected result and a next
check. Record what actually happened, the cost, and the next decision.
Cheap deterministic checks can run around the clock at a fixed cadence.
Use a bounded agent review for changes that need judgment. Start with
discovery and a shared steward. Add dedicated scheduled teams once there is
an authorized queue, evidence to work on, and a measured budget. Keep
schedule ownership and verified runs in the internal operating record.
The [public discovery probe](https://github.com/cleanappio/cleanapp-agent001/actions/workflows/agent-discovery.yml)
checks the guide, official Registry lookup, and fresh MCP tool inventory
every six hours. Its result measures the published connection path.

To improve Codex and other agents, review corrections for their scope and
freshness. Put approved, publishable lessons in versioned agent context
and retain restricted evidence references privately. Ask the next run to
show what it retrieved and how it affected the decision. Replay a held-out
task before promoting a lesson into standing guidance. A failed test or
superseded lesson should stay traceable.
[Agent context](../agent-context/overview.md) ·
[Internal operating record guidance](README.md#internal-operating-record)

New case studies, outreach and live enrollment remain deferred.
