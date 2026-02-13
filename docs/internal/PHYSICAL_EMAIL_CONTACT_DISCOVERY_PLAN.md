# Physical Location Contact Discovery (Email Pipeline) — Implementation Plan

## Goal

When a **physical** report is submitted (office park, museum, campus, train station, etc), CleanApp should:

1. Determine the most likely responsible entity for that location (operator/owner/department).
2. Discover *plausible, actionable* contact mailboxes (prefer role-based: `facilities@`, `security@`, `support@`, etc).
3. Send a notification via the email pipeline **only when we have a reasonable target** and can do so safely (throttled, opt-out capable).

This plan is incremental: each phase can ship independently and improves coverage without destabilizing core ingestion/analysis.

## Current Deployed Building Blocks (as of 2026-02-13)

### A) `report_analysis.inferred_contact_emails` (per report)
This is our "best guess" contact list for a given report (comma-separated emails).

Populated by:
- **Report analyze pipeline** (physical): OSM + website scraping + optional Google CSE + optional LLM re-analysis with location context.
- **Email fetcher daemon** (physical): consented `contact_emails` by `area_index` point-in-area match.

### B) Area-based contacts (consent gated)
Tables:
- `area_index` (spatial index polygons)
- `areas` (geojson)
- `contact_emails` (`consent_report=true` gate)

### C) Email sending service
`email-service` polls analyzed reports and sends via SendGrid, with:
- per brand+email throttling
- opt-out enforcement
- dry-run mode available

## Problem We Must Solve

Physical contact discovery is frequently **asynchronous** (network calls, caching, enrichment jobs). If email-service processes a report immediately and marks it as processed with "no recipients", then even if contacts are discovered minutes later, we will never send.

So we need:
- A way to **defer** physical reports briefly when recipients are not yet known.
- A clear, safe hierarchy for "who should we email?"

## Phase 1 (Shipped / In Progress): Make Physical Reports Eligible for Contact Discovery

### 1) Populate inferred emails for physical reports
Mechanisms:
- OSM enrichment during analysis:
  - Nominatim reverse-geocode -> domain/operator/org
  - Overpass nearby POIs -> contact emails + websites
  - Website scrape -> mailto + plain emails
  - Optional Google Custom Search -> location contact emails (if configured)
  - Optional LLM pass with location context (Gemini) if still sparse
- Consent-first enrichment:
  - `email-fetcher` updates `inferred_contact_emails` for physical reports from `area_index + contact_emails`

### 2) Defer processing in email-service when recipients are not yet known
If a report is physical and has:
- no usable inferred emails
- no area-consented emails

Then:
- schedule a retry in `email_report_retry` with a short exponential backoff
- stop after N tries and mark processed

This avoids "giving up" before enrichment finishes.

## Phase 2: Improve Coverage for Large Facilities (Campus/Stations/Malls)

This phase is about *unit/department/facility* targeting (UCLA, Stanford, etc).

### 1) Better org hierarchy extraction (OSM/Wikidata)
Input:
- Nominatim fields: `operator`, `brand`, `name`, `website`, `contact:*`
- Overpass results for buildings and amenities around the point

Output:
- A ranked set of candidate "entities" at the location:
  - campus/university
  - department or facility
  - station authority / property manager

### 2) Contact discovery strategy per entity
For each candidate entity:
- Find official domain (OSM website, known domain patterns for .edu/.gov, etc)
- Scrape: `/contact`, `/about`, `directory`, `facilities`, `security`, `help`
- Prefer role mailboxes:
  - facilities/maintenance
  - security/safety
  - accessibility
  - sustainability/cleanup
  - support/help

### 3) Cache + dedupe
Cache discovered mailboxes by:
- `location_hash` (rounded lat/lon or geohash)
- `domain`
- `primary_name`

So repeated reports from the same campus don’t re-scrape.

## Phase 3: Area Coverage and Consent Workflows (Professionalization)

To be "enterprise-grade", we want a consent/verification lane for physical contacts.

### 1) Expand `areas/area_index`
Import polygons for:
- major campuses
- major transit hubs
- business parks / malls

### 2) Two-tier contacts
Introduce:
- `contact_emails` (verified/consented for sending)
- `contact_emails_candidates` (discovered but not yet verified)

Operationally:
- discovery writes candidates
- a lightweight review step flips to consented

This gives maximum safety while improving coverage over time.

## Phase 4: Observability (Make It Not a Black Box)

We should be able to answer, at any time:
- How many emails were sent last 24h? last 7d?
- Send success rate (SendGrid accepted vs failed)
- Reasons for "no send"
- Physical coverage: % of physical reports that resolve recipients

Deliverables:
- A "trace one seq" script (DB + service logs) that outputs:
  - analysis row
  - inferred emails before/after enrichment
  - email-service decision path and send/no-send reason
  - SendGrid response (redacted)

