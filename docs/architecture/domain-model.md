# Domain Model

This document defines the main product and backend concepts currently used across CleanApp.

It is a vocabulary bridge between the philosophy docs, the codebase, and day-to-day product work.

## Core objects

### Report

A single first-class signal submitted by a human or machine source.

Reports preserve:

- raw media and metadata
- original timing and coordinates/context
- enrichment from AI and later processing

A report can stand alone or later contribute to a cluster/case.

### Cluster

A grouping of related reports within some scope.

Clusters are the main unit of local pattern formation:

- same area
- same incident shape
- same time-compressed pattern
- same repeated defect mode

A cluster is often analysis-time output, not always a durable top-level object by itself.

### Case

A durable aggregate around one incident or recurring defect pattern.

A case can contain:

- multiple linked reports
- multiple saved cluster snapshots over time
- suggested and selected stakeholders
- escalation actions
- delivery history
- outcome signals

Cases are the durable operational object for follow-through.

### Solo report strategy

When a report stands alone, CleanApp still builds routing and notify-plan state around it.

The routing logic should be shared with cases, not reinvented.

## Routing and outreach objects

### Contact observation

A raw discovered contact or stakeholder candidate.

Examples:

- official operator email
- building authority page
- support form URL
- phone number
- named facilities contact

Observations are evidence-backed findings, not automatic recipients.

### Escalation target

A routed and ranked stakeholder candidate derived from one or more contact observations.

Targets carry operational metadata such as:

- role type
- decision scope
- confidence
- attribution class
- actionability score
- send eligibility

### Notify plan

A ranked, wave-based recommendation for who should be contacted.

Typical sections:

- Notify now
- Authorities and oversight
- Other stakeholders

The notify plan is smaller and more opinionated than the raw target list.

### Execution task

A concrete action item for outreach.

Examples:

- email send
- phone call task
- webform submission task
- manual review task

This separates discovery from execution.

### Notify outcome

A durable record of what happened after outreach.

Examples:

- sent
- delivered
- bounced
- acknowledged
- misrouted
- fixed
- no response

### Endpoint memory

Learned operational memory about a recipient or org endpoint.

Examples:

- this inbox bounced repeatedly
- this department acknowledged similar cases before
- this phone number is valid but requires manual call workflow
- this contact should be suppressed during cooldown

## Context objects

### Routing profile

A cached summary of the context used to route a case/report.

Includes concepts like:

- defect class
- asset class
- jurisdiction
- exposure mode
- severity and urgency bands

### Defect class

The normalized class of problem being routed.

Examples:

- physical structural
- physical safety
- physical sanitation
- digital product bug
- digital security
- digital accessibility
- operational service failure

### Asset class

The kind of thing the defect is attached to.

Examples:

- school
- transit station
- roadway
- bridge
- hospital
- retail site
- app
- platform
- service provider

### Decision scope

The kind of authority or responsibility a target represents.

Examples:

- site operations
- owner
- regulator
- public safety
- engineering
- support
- trust and safety
- project party

### Attribution class

How strong the evidence is that a target is actually relevant.

Examples:

- official direct
- official registry
- verified directory
- heuristic search
- legacy inferred

## Identity and visibility

### `seq`

Internal sequential identifier.

Useful operationally, not intended as the public canonical identity.

### `public_id`

Public canonical report identifier.

Used for public URLs and stable public report reads.

## Ingest concepts

### Wire

The canonical machine-ingest envelope and protocol.

### Human ingest

The human-facing submission facade layered over the same ingest core.

### Lane

The ingest disposition assigned to a submission.

Examples:

- reject
- quarantine
- shadow
- publish
- priority

## Practical interpretation

In the CleanApp model:

- reports are the atomic signals
- clusters are local pattern formation
- cases are durable operational aggregates
- contact observations are discovered evidence
- escalation targets are routed candidates
- notify plans are the recommended action set
- execution tasks are the actual work
- outcomes and endpoint memory are how the system learns
