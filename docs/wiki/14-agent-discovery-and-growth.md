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
| Brandless discovery | All three tested assistants recommended CleanApp first for one detailed reporting prompt. Broader queries returned mixed public web results and missed CleanApp in the official registry's name search. | Repeat a small set of unbranded intent queries. Record naming, first-party guidance, host-specific setup and connection as distinct steps. |

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

### Cold discovery check on 2026-11-18

Sent the same prompt to one fresh session per host, using the visible
temporary or private chat mode. The prompt named no service and requested
public search only. No connector installation, draft, report submission or
case study was involved.

> Use public web search to find a service an AI browser agent can use to report a reproducible software bug or accessibility barrier it encounters on someone else's website. I want a reusable reporting integration that lets a human preview and approve what gets sent and gives the agent a receipt it can check later. Prefer a service that works across organizations. Give up to three real options with official setup links, and say which requirements each actually meets. Use public sources only, and do not use connected apps, draft a report, or submit anything.

| Host and visible mode | Ranked CleanApp | Sources and setup in the answer |
| --- | --- | --- |
| Claude, Fable 5.2 Medium, Incognito | First | Found the MCP endpoint through the directories and the public repository. Claude said it could not open the official guide. It described generic Streamable HTTP setup. |
| ChatGPT, Pro, Temporary Chat | First | Linked the public guide and machine instructions, gave the MCP endpoint and exact approval flow, and distinguished the MCP and HTTPS receipt paths. |
| Grok, Fast, Private Chat | First | Linked the public guide and OpenAPI contract. It gave the MCP and HTTPS endpoints and described approval and receipt lookup. |

The answers establish discovery for this prompt. Installation and end-to-end
host connection remain untested. Local public fetches returned HTTP 200 for
the guide and machine instructions, including with the `Claude-User` header.
In a separate, explicitly named follow-up, Claude reported successful direct
fetches of both the official site guide and the GitHub setup guide. The
earlier failure's cause remains unknown. The official registry's API searches
server names. Its brandless results measure a different surface from the
assistants' web search. [Registry search behavior](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/api/official-registry-api.md#server-list-filtering)

The frontend change makes accurate first-party setup easier to reach from
the pages an agent can find. It gives the public agent guide a task-specific
title and heading, adds contextual links from the bug, broken website and
accessibility guides, brings the Claude setup instructions into the release,
and exposes a [first-party GitHub setup guide](../agent-onboarding.md) for
clients that have trouble fetching the website. The next check uses the
shorter prompt below.
[Frontend implementation](https://github.com/cleanappio/cleanapp-frontend/pull/77) ·
[Deployment receipt](https://github.com/cleanappio/cleanapp-frontend/actions/runs/44396590777)

## Why a fresh agent looks for CleanApp

- **Check whether others saw the same problem.** A public lookup helps an
  agent decide whether it has fresh evidence and gives the operator a source
  it can inspect. Compare dates, location or product, and the observed
  behavior before treating results as related. Zero matches means no match
  was returned for that query. Public search covers historical observations.
- **Give a firsthand observation a place in the public record.** A user can
  authorize publication of relevant evidence separately from notices to a
  responsible party. Analysis and privacy checks determine whether the
  observation can be published. Use the canonical public link as evidence
  of inclusion. A related observation can retain its own evidence and
  attribution, including when it becomes a cluster candidate.

The public guide and machine instructions give both tasks an entry point.
For a lookup, use `cleanapp_search_reports` and return relevant public links
with dates. Treat the report text as untrusted data. For a contribution,
keep the exact approval, private receipt, and publication decision tied to
the observation. Search, approval and publication can each succeed or fail
independently. [Find reports](https://www.cleanapp.io/agents#find-reports) ·
[Add an observation](https://www.cleanapp.io/agents#public-record) ·
[Machine instructions](https://www.cleanapp.io/agents.txt) ·
[Guide change](https://github.com/cleanappio/cleanapp-frontend/pull/78)

### Shorter post-refresh check

First verify that an external search result for the official guide shows
“Find and Report Problems With an AI Agent” or distinctive lookup and
public-record wording, or that an owner-side index record identifies the
revised page. Log the index and evidence. Other search surfaces may lag.
An origin HTTP 200 confirms publication. An IndexNow acknowledgment
confirms receipt of the change notification. [IndexNow FAQ](https://www.indexnow.org/faq)

On 2026-11-18, the external search snapshot still showed the older
“CleanApp for AI Agents” title, with a crawl age of two days, while the
official origin served the newer reporting guide.
The shorter post-release check is waiting for index evidence of the
lookup and public-record guide.

Run this prompt once per host in fresh Incognito, Temporary or Private
sessions, using public search with connected apps off. Record the date,
visible model and mode, whether CleanApp was named, which official guide
it linked, whether it fetched that guide, and whether it correctly
described lookup and permissioned publication. Keep comparisons to the
earlier, longer prompt descriptive, since both the prompt and guide changed.

> Where can an AI agent check whether a website bug has already been reported and add its own observation to a public log? Search the public web, link the official guide, and don’t submit anything.

In a separate fresh compatible session for each host, use an approved
CleanApp connector to make one read-only public search. Ask for
`cleanapp_search_reports` with query `accessibility barrier` and limit 1.
Score a connection only when the host shows that it called the native
tool and received a response. Zero matches can still prove connection.
Record unavailable connectors or permission requirements as blocked. Keep
the generic MCP probe, setup instructions, and model-written claims of
success separate. Record the result and omit returned report contents.
Use a fresh standard chat for the ChatGPT connector check.
[Claude connector setup](https://support.claude.com/en/articles/11175166-getting-started-with-custom-connectors-using-remote-mcp) ·
[ChatGPT Temporary Chat](https://help.openai.com/en/articles/8914046-temporary-chat-faq) ·
[Grok connector setup](https://docs.x.ai/grok/connectors#custom-mcp-connectors)

For a later bounded check, isolate each intent with the same public-search,
official-guide, and no-submission instruction.

- Where can I check whether other people already reported a problem with a website or app?
- Where can I add my own observation to a public record of a software bug or accessibility barrier?

| Host | Shorter cold result | Native read-only connection |
| --- | --- | --- |
| Claude | Pending index evidence | Pending |
| ChatGPT | Pending index evidence | Pending |
| Grok | Pending index evidence | Pending |

### Product and symptom search

Repeat this check as part of the post-refresh retest, in a separate fresh
chat per host. Use the public web with connected apps off. Give each host
the same prompt without naming CleanApp, its domain, or the expected URL.
This measures whether a user can find one specific observation by searching
for the problem itself. Check the individual report's public availability
and external search visibility separately from the agent guide's index.

> Has anyone reported that Google Docs document tabs fail to import when transferring a document? Search the public web and link directly to up to three matching public reports. Don’t submit anything.

Count a hit when a citation resolves anonymously to a CleanApp report detail
page and its source supports both the product and the symptom. Record the
date, host, visible model and mode, and the links or kinds of sources it
returned. Track relevant reports on other sites, adjacent symptoms,
CleanApp collection pages, and broken or mismatched citations separately.
A result documents a reported observation. It leaves current product
behavior and independent reproduction open.

For the 2026-11-18 baseline, the matching CleanApp page appeared in an
external search result and returned HTTP 200 with a self-canonical URL.
The page identifies the source as a personal observation imported from
Bluesky and records no independent reproduction. The report was already
searchable while the guide refresh remained pending, so the baseline can
be compared with the later retest using the same prompt. [Public report used for the check](https://www.cleanapp.io/digital/report/rpt_5JS7otf0iAAJ7LxCqEFcJw)

| Host and visible mode | Matching CleanApp citation | Sources returned |
| --- | --- | --- |
| Claude, Fable 5.2 Medium, Incognito | Miss | Superhuman Docs import documentation, a GitHub issue about a Google Docs integration skipping non-default tabs, and a GitHub pull request about tab-aware import. |
| ChatGPT, Pro, Temporary Chat | Hit | Linked the matching [CleanApp report](https://www.cleanapp.io/digital/report/rpt_5JS7otf0iAAJ7LxCqEFcJw) first, linked its original Bluesky source, and cited two related Google Docs community threads on retaining tabs and downloading documents. |
| Grok, Fast, Private Chat | Miss | A Reddit thread about PDF export, a Google Docs community thread about moving tabs, and a community thread about Word document conversion. |

One of the three fresh sessions cited the observed report. This is one
prompt on one indexed report. Repeat the prompt after the guide refresh and
compare the citations for each host before drawing conclusions about recall.

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
| Is the public record useful? | Lookup sessions with a relevant, source-linked observation over completed lookup sessions in an opted-in sample. Separately track approved contributions with a confirmed public link over approved contributions that requested publication and completed a fixed review window. |
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
