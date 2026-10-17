# Brand cohort cache (opt-in, design C″)

`GetEvidenceBrandPage` reads a brand's complete cohort in one repeatable-read
snapshot under the public eight-second deadline; for the largest brands that read
alone exceeds it (Google: 61k reports, ~6.6 s measured server-side), so
`evidence_cohort_cache.go` can hold the cohort in memory and evaluate every
publication guard live per request from small blocking sets instead.

**What is cached.** One guard-free `evidenceCohort` per canonical key (sorted
brand scope plus requested analysis language): every report of the scope with a
non-empty public identity, its chosen analysis tuple, evidence kind/version/
relationship/presence bits, original-media bit, exact source tuple (projection or
fresh fallback) and SQL-ordered case reference, plus the Go clock taken just
before the snapshot's first read. The refresh statement
(`projectedEvidenceCohortQuery`) is the direct statement minus its three live
guards and the `report_raw` join only they used (`TestEvidenceCohortRefreshQuery…`
proves it byte for byte). Nothing kind-, sort-, limit- or offset-specific is
cached; grouping stays request-local and pure. No trigger or table is added.

**Live publication guards.** Inside the request's own read-only repeatable-read
transaction and deadline, `loadPublicationBlocklists` reads four whole sets as
covering index ranges, each the exact complement of a direct-statement guard
under the same column collations:

| set | statement | index |
| --- | --- | --- |
| inactive status | `report_status.status IS NOT NULL AND status<>'active'` | `idx_rs_seq_status` |
| non-public raw | `report_raw.visibility IS NOT NULL AND visibility<>'public'` | `idx_visibility` |
| private owner | `reports_owners.is_public=FALSE AND owner IS NOT NULL AND owner<>''` | `idx_is_public` |
| privacy restricted | `report_evidence_source_projection.privacy_restricted=TRUE` | `idx_privacy_restricted` (migration `0032`, online, optional) |

Their cost is proportional to the number of blocked reports in the database, not
to the cohort: production measured 0 private owners, 1,480 shadow rows, 1,606
inactive statuses and 0 restricted projections (2026-10-17, see
`brand-page-read-path-decision.md` section 6). `applyPublicationBlocklists` then
drops blocked seqs and overwrites `PrivacyRestricted` from the live projection in
Go **before** `groupEvidenceRecords`, so `Total`, `High`/`Medium`, `KindCounts`,
`Overview`, family representatives, the first-eight-then-privacy samples and
media/excerpt redaction are all live and exact; the existing bounded hydration
runs unchanged in the same transaction. The index hints are optional: MySQL warns
and scans when an index is absent, with the same answer. A failed set read fails
the request closed.

**Bounded staleness (the only kind).** Brand membership (`is_valid`, brand
re-attribution, new reports), the non-empty `public_id` identity, analyses and
the source tuple are as old as the cohort snapshot (default ≤ 60 s plus one cohort
read). Two consequences: a report whose projection was invalidated after the
snapshot is hydrated from its last complete normalizer output rather than
failing the page (the direct path's fail-closed generation check stays in force
there), and a projection that was dirty or absent at refresh keeps its exact
fresh privacy value unless the live column adds a restriction. Un-redaction of
such a row, and any privacy change, becomes visible once the projection worker
(bounded batches every 30 s) recomputes it; a `public_id` emptied after the
snapshot fails that page closed until the next refresh (0 empties measured). A
page served this way carries `SnapshotAt` (JSON `snapshot_at`), the cohort
snapshot time; the direct path leaves it unset.

**Refresh, bounds and shutdown.** Refreshes are demand-driven: only a key that
was requested is loaded, at most once at a time per key, in a background
goroutine with its own budget and `MAX_EXECUTION_TIME`. An absent key means the
direct path (unchanged) plus one scheduled first refresh; a held cohort older than
the interval is served and refreshed. The number of held cohorts is bounded
(`EVIDENCE_BRAND_COHORT_MAX_KEYS`, LRU by request recency; refreshing cohorts are
never evicted, and a new key is not admitted while nothing idle can be). A failed
refresh keeps the previous cohort, backs off exponentially from the interval
(floored at one second) and logs the failure class only. `Service.Stop` cancels
in-flight refreshes and waits for them. One listener container runs in
production, so the cache is cold after each deploy; the warm list schedules the
first cohorts at startup.

**Configuration / rollback.** `EVIDENCE_BRAND_COHORT_CACHE=1` enables it
(default off; unset it to roll back, no schema change needed),
`EVIDENCE_BRAND_COHORT_REFRESH_SEC` (default 60), `EVIDENCE_BRAND_COHORT_BUDGET_SEC`
(default 60), `EVIDENCE_BRAND_COHORT_MAX_KEYS` (default 8; ESTIMATE ~40 MB per
61k-report cohort) and `EVIDENCE_BRAND_COHORT_WARM=microsoft,google`. Release
migration `0032_evidence_source_projection_privacy_index` adds the privacy index
online (`ALGORITHM=INPLACE, LOCK=NONE`, bounded lock wait); `Verify` accepts the
projection with or without it.

**Parity proof** (dedicated local MySQL 8 database named `*_evidence_test`, CI job
"Evidence briefing (MySQL 8)"): every projected parity sub-fixture of
`TestEvidenceMySQLFixture` also serves its matrix from a held cohort through
`cohortCacheFixture.parity` and requires `reflect.DeepEqual` with
`getEvidenceBrandPageSQL`. The cohort is dropped only after refresh-time data
changes (projection backfills, analyses, alias membership); `ProjectedPublication
BlockerParity`'s owner/status mutations, `ProjectedOptionalOwnerIndex`'s owner
flip and `ProjectedCohortCache/LiveGuardsWithoutRefresh` (one change per guard
plus a worker-recomputed privacy restriction, then their reversal) run with no
refresh at all. `ProjectedCohortCache` also covers kinds coverage/unknown, sort
recurrence, offsets 0/2/100 in en/fr, the privacy index drop/online migration, and
the disabled cache.

```
EVIDENCE_TEST_DSN='user:pass@tcp(127.0.0.1:3306)/x_evidence_test' go test ./database -run '^TestEvidenceMySQLFixture$' -count=1 -v
```
