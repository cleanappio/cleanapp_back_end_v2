# Agent Context Overview

This file is a fast onboarding guide for coding agents and new engineers.

It is intentionally secondary to the canonical docs.

If anything here conflicts with the root philosophy docs or current code:

- philosophy and principles: trust the root docs
- implementation reality: trust the code

## Read this first

1. `/Users/anon16/Downloads/cleanapp_back_end_v2/README.md`
2. `/Users/anon16/Downloads/cleanapp_back_end_v2/WHY.md`
3. `/Users/anon16/Downloads/cleanapp_back_end_v2/THEORY.md`
4. `/Users/anon16/Downloads/cleanapp_back_end_v2/INVARIANTS.md`
5. `/Users/anon16/Downloads/cleanapp_back_end_v2/ARCHITECTURE.md`

Then use:

- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/architecture/current-system-map.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/architecture/domain-model.md`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/docs/decisions/decision-log.md`

## What CleanApp is optimizing for

CleanApp turns messy reports about physical, digital, and operational defects into actionable routing toward actors who can fix them.

The product is not just a reporting inbox. The core loop is:

1. ingest
2. preserve raw signal
3. enrich and structure
4. cluster and case where useful
5. route to responsible and interested parties
6. execute outreach
7. learn from outcomes

## Where the main logic lives today

### Ingest

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/cleanapp_wire_v1.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/human_ingest.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/cleanapp_wire_v1.go`

### Cases and cluster analysis

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/cases.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/cases.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/case_accumulation.go`

### Contact routing

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/case_contact_discovery.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/case_notify_routing.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/contact_routing_context.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/handlers/report_contact_strategy.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/database/subject_routing_strategy.go`

### Public read and rendering

- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener/main.go`
- `/Users/anon16/Downloads/cleanapp_back_end_v2/report-listener-v4/src/main.rs`

## Current strategic posture

- defect-general, not physical-only
- shared routing engine for cases and solo reports
- public browsing remains open
- write-plane provenance matters
- routing quality matters more than raw notification volume
- outcome learning matters more than one-off sends

## Good default questions for new work

Before changing the system, ask:

1. Does this preserve individual reports as first-class signals?
2. Does this strengthen clusters/cases rather than collapse them into tickets?
3. Does this improve routing quality?
4. Does this preserve explainability?
5. Does this help CleanApp learn from outcomes?
