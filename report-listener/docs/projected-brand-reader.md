# Revision-checked evidence source reader

`GetEvidenceBrandPage` uses `getEvidenceBrandPageProjected` for public brand
briefings. Revision-checked normalized source values avoid repeated source
discovery, while exact misses and every live publication guard remain in the
same read-only snapshot and unchanged eight-second deadline.

One explicit query selects preferred analysis, report metadata with authoritative
projection NULLs, live visibility/status/ownership, live evidence presence and
original-media status, and the per-report normalized source projection. It never
filters by kind or pages the input universe. Clean source entries require the
shared version, generation, dirty flag, completeness, and ingestion-NULL checks.
Cache SQL date literals preserve NULL, non-NULL zero dates and microseconds.
Malformed date syntax or invalid dates become exact misses.

Only visible missing/dirty/invalid entries call
`loadFreshEvidenceSourceRecords(ctx, tx, ids)`. That shared helper accepts any
existing report parent independently of analysis or publication and returns only
source-normalizer outputs. It uses native EII source/ID ordering and original raw
Wire `submitted_at DESC` ordering; header presence controls the chosen tuple's
payload fields, including authoritative NULLs. Exact timestamp ties retain the
reference's unspecified selection. The established archive/SQL normalizer is
reused unchanged. Missing parents are omitted for refreshers; a missing visible
fallback is an error for the reader. No partial counts are returned.

All source-cache misses merge only the fifteen source outputs. Analysis, evidence,
media and publication remain live. Existing Go overview, SQL-collated case links,
source deduplication, kind/family grouping, first-eight-then-privacy samples, exact
counts and bounded chosen-analysis display/image hydration run in the same
read-only repeatable-read transaction and eight-second context. The reader never
refreshes or publishes cache generations. Refreshers lock persistent generation
rows before their first consistent source read and use the common guarded store.

The executable SQL reference remains unchanged. The guarded dual-MySQL fixture
retains cold/mixed/warm source projections, malformed public IDs and mixed
collations, native source winners, privacy, nullable analysis and large-family
sample/count parity. Unit checks cover the clean path's absence of source reads,
live publication guards, exact nullable hydration, dirty/malformed misses, source
parent scope, date grammar and fail-closed partial input/fallback handling.
