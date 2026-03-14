# Decision Log

This file records short, durable statements of major product and architecture decisions.

It is not a full ADR system. It is a compact memory layer for decisions that would otherwise be repeatedly reconstructed from chat, code, and commit history.

## 2026-03-08: CleanApp Wire is the canonical machine-ingest core

Decision:

- machine-originated submissions should flow through CleanApp Wire rather than separate bespoke ingest paths

Why:

- consistent provenance
- lane assignment
- idempotency
- receipts and status

Consequence:

- legacy machine-ingest surfaces should become compatibility facades, not parallel cores

## 2026-03-11: Human ingest should reuse the same ingest core, but remain a separate facade

Decision:

- mobile/web human submissions should not masquerade as Wire externally
- they should still land in the same canonical ingest core internally

Why:

- preserve low-friction human UX
- keep provenance and lane policy unified

Consequence:

- `human-reports` facade exists on top of the shared ingest machinery

## 2026-03-11: Public report identity should be opaque, not sequential

Decision:

- public report URLs and public detail fetches should use `public_id`

Why:

- remove trivial sequence walking
- preserve stable canonical public links

Consequence:

- internal `seq` remains useful operationally
- public surfaces should migrate off `seq`

## 2026-03-11: The globe should remain dense and public

Decision:

- keep the full-history map experience visually dense and public

Why:

- the globe is a core product surface, not just a browse list

Consequence:

- visualization and stable identifier exposure should be separated where possible
- hardening should not destroy the globe experience

## 2026-03-11: Cases are durable aggregates, not one-off saved polygons

Decision:

- cases should accumulate multiple cluster snapshots over time

Why:

- repeated analysis of the same area should not create endless duplicate umbrella cases

Consequence:

- cluster-to-case matching, attach-or-create logic, and merge tooling are core behaviors

## 2026-03-11: Contact discovery and notify routing are distinct steps

Decision:

- raw discovered contacts should be separated from the recommended notify set

Why:

- broad discovery is useful
- indiscriminate sending is harmful

Consequence:

- contact observations, routed targets, and notify plans should remain distinct concepts

## 2026-03-12: Cases and solo reports should share one routing engine

Decision:

- contact discovery and notify routing should be shared across cases and solo reports

Why:

- the logic is defect-general
- separate engines would drift and duplicate effort

Consequence:

- cases and reports should remain thin wrappers over shared routing context and scoring

## 2026-03-12: CleanApp is defect-general, not hazard-specific

Decision:

- the action system must support physical, digital, and operational defects under one conceptual model

Why:

- physical/digital separation is artificial at the system level
- the mission is routing actionable defects to the right actors

Consequence:

- routing must adapt by defect class and asset class, not by hard product silo

## 2026-03-12: Notify plans should be wave-based and multi-party

Decision:

- outreach should happen in ranked waves rather than flat blasts

Why:

- improves relevance
- reduces institutional fatigue
- preserves escalation headroom

Consequence:

- same-org caps, backup contacts, and widening logic are part of the model

## 2026-03-13: Outcome learning is part of the product, not a reporting afterthought

Decision:

- delivery, acknowledgment, bounce, misroute, and fix signals should influence future routing

Why:

- CleanApp should learn who actually acts

Consequence:

- endpoint memory and notify outcomes are first-class data
