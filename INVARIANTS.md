# INVARIANTS.md

## CleanApp System Invariants

This document defines the **non-negotiable invariants** of the CleanApp system.

These are not preferences.  
They are not implementation details.  
They are **laws of the system**.

Any change — human or AI-generated — that violates an invariant is incorrect, even if it appears functional or efficient.

---

## 1. Individual Reports Always Matter

Every good-faith report is a first-class signal.

- No report is “too small” to ingest
- No report is ignored by default
- No report is dismissed because it stands alone

CleanApp exists to honor individual signals **and** allow them to compound.

---

## 2. Clusters Are the Unit of Systemic Meaning

While individual reports matter, **clusters are the unit of systemic insight**.

- Decisions are informed by clusters, not anecdotes
- Severity, urgency, and priority emerge through aggregation
- Time-compressed clusters indicate urgency

The system must always preserve the ability to form and re-form clusters as new data arrives.

---

## 3. Routing Is Multi-Party by Default

No issue has a single “owner” by assumption.

CleanApp routes signals to:
- responsible parties
- interested parties
- stakeholders with economic, regulatory, or strategic incentive

Any design that assumes a single recipient by default violates this invariant.

---

## 4. Recall Is Favored Over Precision at Ingestion

At the point of ingestion:

- false negatives are more dangerous than false positives
- missing a signal is worse than ingesting noise

Filtering, validation, and precision are **downstream concerns**.

The system must never prematurely discard potential signals.

---

## 5. Raw Data Is Never Irreversibly Lost

CleanApp must always preserve:

- raw reports
- original text
- original media
- original timestamps
- source metadata

Enrichment must be:
- additive
- reproducible
- re-runnable as models improve

Any pipeline that destroys raw data breaks the system.

---

## 6. AI Is Assistive, Not Authoritative

AI may:
- summarize
- cluster
- classify
- infer stakeholders
- reduce cognitive load

AI may not:
- declare truth
- suppress signals permanently
- replace human judgment
- create irreversible outcomes

Human accountability is preserved at all times.

---

## 7. History Must Be Preserved

CleanApp is not a real-time alerting system only.

- Historical data is a core asset
- Trends matter more than moments
- Past failures must remain visible

No design may privilege immediacy at the expense of memory.

---

## 8. Visibility Is a Feature, Not a Side Effect

CleanApp is designed to make patterns visible.

- Repetition must be observable
- Persistence must be measurable
- Improvement (or lack thereof) must be detectable

Any feature that hides systemic patterns violates this invariant.

---

## 9. Incentives Must Align With Signal Quality

The system must reinforce:

- meaningful reporting
- honest aggregation
- robust validation
- responsible action

Designs that reward:
- spam
- noise
- performative reporting
- superficial engagement

are incompatible with CleanApp.

---

## 10. The Pipeline Must Remain Modular

No single component may assume it is the system.

- ingestion sources are replaceable
- enrichment models will change
- routing strategies will evolve
- dashboards are views, not truth

Tight coupling that prevents evolution violates this invariant.

---

## 11. Physical and Digital Signals Are Treated Uniformly

The system must not privilege:
- physical-world reports over digital reports
- or vice versa

Both flow through the same conceptual pipeline:
- ingest
- enrich
- cluster
- route
- observe over time

Artificial separation is a violation.

---

## 12. Optimization Must Not Destroy Meaning

Local optimizations must not:
- reduce recall
- erase history
- collapse multi-party routing
- turn clusters back into tickets
- convert signals into inbox items

Efficiency that destroys meaning is not efficiency.

---

## 13. The System Must Remain Interpretable

CleanApp must remain legible to humans.

- why a cluster exists must be explainable
- why a signal was routed must be traceable
- why an action was suggested must be inspectable

Opaque behavior that cannot be reasoned about violates trust.

---

## 14. CleanApp Is Not an Inbox

At no point may CleanApp devolve into:
- a ticketing system
- a complaints queue
- a PR buffer
- a support deflection tool

If this happens, the system has failed.

---

### Enforcement

These invariants apply to:
- code changes
- schema changes
- AI prompt changes
- data retention policies
- routing logic
- incentive design

They are binding on:
- human contributors
- AI systems
- automation tools
- future refactors

---

### End of INVARIANTS.md
