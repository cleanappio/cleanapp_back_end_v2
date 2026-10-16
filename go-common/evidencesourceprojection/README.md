# Normalized evidence source projection

This is a derived cache of the 15 source-normalizer outputs. Analysis, report kind,
visibility, media, case links and publication permission remain live inputs. A
missing or dirty row requires exact source normalization; it never removes a
report from the cohort. Nullable dates are authoritative, including SQL NULL and
historical zero-date literals. Strings are unbounded to preserve the normalizer's
output without truncation.

Run `Migrate` only through the explicit report-listener migration flow (step
`0031_evidence_source_projection`). `Verify` is read-only and checks the table,
indexes, named invalidators and known source cascade contract. Existing report,
EII and Wire projection triggers are not replaced. The canonical selected-service
source deployment already runs this Go migration with the staged `go-common`.

Refresh in a fresh repeatable-read transaction:

1. Obtain the sorted, unique IDs before starting the transaction (at most 200).
2. Call `EnsureRowsAndLock` before **any consistent read** in that transaction.
3. Read and normalize sources without acquiring parent-table share/update locks.
4. Call `Store` with the returned revision and each complete result; commit only
   after all intended stores succeed. Keep the generation locks until commit.

A writer changes its source and generation in one transaction. If it waits behind
refresh, the refresher sees the previous source snapshot and the writer dirties
that result after the refresh commits. If it commits first, the refresher locks
the new generation before establishing its snapshot. Establishing a snapshot
before locking generations violates this guarantee. Do not invoke locking source
projection backfills while holding generation locks; their parent-to-cache lock
order can deadlock against refresh.

The `(dirty,seq)` index supports a bounded warmer. Join it to current `reports`
parents so deleted generations do not monopolize the queue. Every supported
source/projection I/U/D invalidates both moved identities; fetcher deletion first
invalidates Wire reports because InnoDB cascades do not activate child triggers.
Generation rows persist after report deletion and reuse. Never delete them as
orphan cleanup: that permits revision ABA. Overflow fails closed through SQL.

Administrative `TRUNCATE`, trigger removal, foreign-key-check bypasses, and bulk
schema replacement are outside the writer contract. They require a maintenance
plan that prevents serving the cache and invalidates affected generations before
reads resume. An algorithm/collation change must increase `NormalizerVersion`
and explicitly mark/backfill affected rows in bounded batches; the dirty queue
alone cannot discover a version bump. Verify the new version before enabling its
reader. Never serve a previous generation or version as a fallback.
