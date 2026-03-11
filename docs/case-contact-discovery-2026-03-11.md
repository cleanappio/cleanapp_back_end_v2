# Case Contact Discovery

Date: March 11, 2026

## Goal

For serious physical hazards, especially structural failures with public exposure, CleanApp should discover:

- actual operator contacts from official sites
- relevant public-authority stakeholders
- project/design/build stakeholders when the evidence supports it
- non-email channels that still matter operationally:
  - phone
  - official contact pages
  - social

The system should stay general-purpose:

- school buildings
- metro stations
- hospitals
- civic buildings
- commercial sites
- any other place with a structurally significant public hazard

## Implemented In This Slice

### Discovery model

- Case escalation targets now carry:
  - `source_url`
  - `evidence_text`
  - `verification_level`
- The backend now prefers:
  - official site pages
  - official municipal pages
  - directory-backed place records
- Low-signal inferred emails remain fallback-only.

### Official-site extraction

- Website crawling now follows more localized and role-bearing links, not just `/contact`:
  - `Kontakt`
  - `Impressum`
  - `Team`
  - `Hausdienst`
  - `Verwaltung`
  - `Hochbau`
  - `Bauamt`
  - similar operational/contact surfaces
- Official pages using lightweight Alpine-style obfuscated email rendering are decoded.

### Hazard-aware stakeholder expansion

- Structural cases trigger broader stakeholder search.
- Severe / urgent / immediate-danger structural cases additionally search for:
  - building authorities
  - fire/building-safety authorities
  - public-safety contacts
- Architect / contractor / engineer discovery remains tied to structural cases.

### Search order

- If `GOOGLE_SEARCH_API_KEY` and `GOOGLE_SEARCH_CX` are configured, case discovery now prefers Google Custom Search for stakeholder page discovery.
- DuckDuckGo HTML search remains the fallback.

## Current Flow

1. Seed the case from the highest-severity report coordinates.
2. Pull nearby official location context from Nominatim / Overpass / Google Places.
3. Crawl the discovered official domains for direct contacts.
4. Build hazard-aware stakeholder search queries.
5. Search for:
   - operator/site contacts
   - facilities/operations contacts
   - municipal authority contacts
   - project parties where appropriate
6. Persist the enriched targets onto the case.
7. Show the resulting targets with role, channel, source, evidence, and verification level.

## What This Still Does Not Solve Perfectly

- Many official websites still require rendered-browser execution to expose all contact data.
- Project-party discovery is still heuristic unless tied to a clearly attributable project/reference page.
- Webforms and phone trees are discoverable, but not yet actionable through a first-class workflow.
- International public-authority search still depends on general heuristics rather than jurisdiction-specific registries.

## Better Tools To Explore Next

### 1. Dedicated rendered contact fetcher

Best next step.

Run a separate rendered-page service using:

- Playwright
- Cloudflare Browser Rendering / scraping tools
- or another browser worker

Use it only for high-value official domains so the core backend stays lightweight.

Why:

- solves client-rendered and obfuscated contact blocks more reliably
- lets us inspect footer-only and accordion-hidden content
- improves evidence snippets and role extraction

### 2. Better web search backends

Worth adding behind config:

- Brave Search API
- additional Google Custom Search tuning
- domain-restricted search for official/public sites

Why:

- better relevance for municipal and project-party discovery
- lower dependence on HTML search scraping
- easier regional tuning

### 3. Jurisdiction-specific registries

For construction/structural cases, high-value future sources include:

- municipal permit portals
- official project/reference pages
- procurement registries
- professional registries for architects/engineers

Why:

- architect / engineer / contractor attribution becomes evidence-based rather than purely search-based

### 4. First-class multimodal escalation workflows

Current sending is email-first. The next upgrade should treat channels separately:

- email: auto-send when verified
- phone: queue human call tasks
- webform: queue semi-automated submission
- public safety: higher-priority escalation workflow

## Operational Principle

The system should be:

- over-inclusive for severe public hazards
- strict about typed channels
- strict about evidence
- willing to surface non-email contacts even when they are not yet auto-sendable

That balance is what makes the result both actionable and defensible.
