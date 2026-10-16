# Normalized source projection

Large brand reads use exact source normalization and grouping for the whole
visible cohort. Repeating historical source selection and normalization on
every request can exceed the eight-second public deadline. The derived
`report_evidence_source_projection` stores only the fifteen normalizer outputs;
analysis, visibility, ownership/status, evidence kind, cases, facets and media
remain live. It contains no report body or original media.

The explicit listener migration `0031_report_evidence_source_projection` installs
the table and additive invalidation triggers. Current source revisions and
normalizer version must match the computed result, and the complete result must
be present. A missing, dirty, malformed or obsolete entry requires the same exact
source reader and normalizer in the request's repeatable-read snapshot. It never
means the report can be omitted. The request fails if exact work cannot finish.

The refresh protocol and trigger/cascade contract are in
`go-common/evidencesourceprojection/README.md`. In particular, refreshers acquire
all generation locks before their first consistent source read, keep original
source reads nonlocking, and preserve generation tombstones. Cache writes cannot
authorize publication or substitute for live privacy and ownership guards.

## Explicit migration and bounded warming

Use the repository's selected-service canonical migration path from a clean,
reviewed, exact source archive. Select only `cleanapp_report_listener`, retain the
source SHA, and use a unique owned remote staging directory. Startup and the
refresh command verify the schema; they do not create it. Apply the migration
before starting a binary that requires it. A schema mismatch fails verification.

Build `./cmd/refresh-evidence-sources` from that same source with `BuildSHA` set to
the full commit. Record the executable SHA-256. Run it through the existing
runtime database identity; never put credential values in arguments, logs, notes
or source archives. `-version` opens no database connection.

The default mode audits an explicitly bounded exact-brand scope:

```sh
./refresh-evidence-sources -brand microsoft -through-seq 1345054
./refresh-evidence-sources -mode apply -brand microsoft -through-seq 1345054 -batch-size 200 -pause 50ms
```

`apply` selects missing or dirty results, commits at most 200 reports per batch,
and emits a checkpoint only after the entire batch commits. `-after-seq` resumes
copying; completion still audits the entire approved range. `-max-batches` emits
`paused`, which is not completion. A failed or partial batch can be replayed.
Concurrent source changes can dirty an earlier batch; rerun from the beginning
to select those entries, or let the bounded worker refresh them. Missing parents
and remaining dirty/missing results block a complete audit. The audit is a series
of bounded snapshots during live writes, not a single global snapshot.

The listener maintains an indexed dirty queue in batches of 100, with a 30-second
pause and a ten-second batch deadline. Cancellation stops work before database
shutdown. Failed batches remain dirty and the sweep proceeds fairly to later
rows; deleted-parent tombstones are retained but skipped. On a normalizer-version
upgrade, explicitly refresh historical results in bounded scopes before relying
on the new version; old-version rows always require exact fallback.

## Release verification

An audit or merged groundwork PR does not establish the performance fix. Prove
the private reader on production data for Microsoft and Google, both firsthand
and all reports, under the unchanged complete eight-second transaction budget.
Then activate the public entry point, satisfy required CI and review, and use
the canonical exact-source, immutable-digest selected listener release.

Verify the actual public failing API paths and UI on that released source and
digest. Retain before/after behavior, exact counts and public IDs, latency,
source/artifact identity, and deployment time. Complete the repair ledger only
after those checks pass. Preserve the previous listener digest and unrelated
service pins for a selected-service rollback.
