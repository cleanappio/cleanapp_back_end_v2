# Repair historical brand evidence projections

Large brand pages calculate exact visible source/family counts before paging.
Historical reports predating scalar projection migrations otherwise fall back
to parent report/media and source rows across the entire cohort. Report 1345054
reproduced an eight-second public API timeout for Microsoft and Google. A
bounded Microsoft sample had 0/1,000 report metadata rows and 0/991 external
source header rows; existing query plans already used indexed ID lookups.

`cmd/backfill-brand-evidence` explicitly warms existing derived metadata,
external source header and Wire evidence projections. It leaves public query
deadlines, exact counts, visibility, source/family grouping and paging intact.
There is no service-boot scan, migration, parent/schema rewrite, notification
queue mutation, model call, public report creation or listener restart.

## Scope and prerequisites

- Existing migrations must have created all three projection tables and all
  nine AFTER maintenance triggers. Run the normal migration verification first;
  this helper only checks readiness and never installs/changes triggers or DDL.
- Use an exact `report_analysis.brand_name`, not a frontend umbrella alias.
  Repeat for each approved underlying brand when repairing an alias group.
- Choose and retain an explicit inclusive sequence upper bound. The selection
  includes every distinct valid digital report in that brand/range, including
  unknown/coverage evidence and private rows; the public reader still applies
  current visibility. It never changes analysis, evidence or access decisions.
- Batch size is 1..200 (default 100), each operation has a 1s..1m deadline
  (default 10s), and the default pause is 250ms. One DB connection is used.
- Use the existing application or migration secret injection. Never print a DSN,
  environment, credential, raw source payload or user content into evidence.
  The DB identity needs SELECT plus writes to the three derived projections;
  do not broaden application privileges to run this one-off.

## Build and run

From `report-listener`, build from a clean committed source tree. Set
`repair_source` to its verified Git revision and retain the output binary hash:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.BuildSHA=$repair_source" \
  -o /private/operator-path/backfill-brand-evidence ./cmd/backfill-brand-evidence
shasum -a 256 /private/operator-path/backfill-brand-evidence
/private/operator-path/backfill-brand-evidence -version
```

Transfer only the binary into the existing runtime/container secret context.
No compiler or additional database instance is required on the production VM.
Below, `BOUND` means the explicitly approved numeric upper sequence bound;
replace it before execution. Audit is the default and performs no writes:

```sh
backfill-brand-evidence -brand microsoft -through-seq BOUND
backfill-brand-evidence -mode apply -brand microsoft -through-seq BOUND \
  -batch-size 100 -pause 250ms
```

For a small first production window, add `-max-batches 1`. A `paused` event is a
checkpoint, not completion. Resume with the same brand, inclusive range and
upper bound and `-after-seq` equal to the last `copied`/`paused` event's
`after_seq`. A failed batch never advances that checkpoint; replay is safe even
if an earlier projection in the batch committed. All three shared backfill
helpers hold current source locks through commit, and existing triggers retain
concurrent source updates/deletes rather than resurrecting stale rows.

Final audit always rereads the **whole** brand/range, including rows before a
resume checkpoint. It checks projection coverage and scalar fidelity (including
binary strings, NULLs, media flags, and exact Wire JSON node types), then emits
`complete` only with a nonempty cohort and zero mismatches. Counts reflect the
bounded cohort at audit time; newly analyzed/reclassified old records can enter
the brand during a long run, so repeat final audit after active intake settles
if necessary. Triggers automatically cover newly inserted source records.

## Evidence and validation

Retain source revision, binary SHA256, execution timestamps, all chosen flags,
numeric checkpoint/batch output and final audit. Record current serving
listener `/version` and image digest separately: applying derived repair data
does not change the serving listener build. Verify actual Microsoft and Google
API requests and browser pages after repair, including exact counts and recent
reports. Projection coverage is a necessary repair condition; endpoint success
and content parity are the acceptance criteria. If the endpoint still times
out, profile its next bounded phase rather than increasing deadlines or
truncating the cohort.

Unit tests cover default read-only audit, required scope, bounded selection,
pause/resume, full audit before the checkpoint and partial failure replay. The
guarded `BRAND_REPAIR_TEST_DSN` fixture only permits an empty local database
named `brand_repair_evidence_test`; CI runs it on MySQL 8.4 and 8.0.44. It
reproduces missing historical projections, repairs 205 translated reports over
multiple batches, checks exact scalar/JSON fidelity and stale-row replay, and
verifies original data, unrelated scope, notifications and future triggers.
