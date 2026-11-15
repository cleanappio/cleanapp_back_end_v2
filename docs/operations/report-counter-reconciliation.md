# Legacy report counter

The map's physical/digital badge polls the frontend `/api/reports-count` route every 30 seconds. That route proxies the legacy backend `/valid-reports-count` endpoint. It is separate from the renderer's `/stats` and map-point count.

The existing counter definition is preserved: a distinct `report_analysis.seq` contributes to the total if any analysis row is valid. It contributes to physical or digital when any valid row has that classification, across all languages. A sequence can count in both categories. These are analysis totals, without public-visibility, status, coordinate or source-policy filters; they are not the count of public map points or completed imports.

The former additive `MAX(seq)` cursor permanently missed analysis arriving later for an older report. It also failed to remove invalidated, deleted or reclassified contributions. The updater now reconciles from the beginning on every pass, using keyset pages of at most 10,000 sequence groups. The pass's maximum sequence is only a finite upper bound. Invalid groups advance the keyset; missing sequence ranges do not require empty-page scans.

Resource bounds:

- Existing covering index `idx_ra_seq_class_valid(seq, classification, is_valid)` is required; no new schema or infrastructure is installed.
- Each query has a five-second timeout and reads the covering index without report text or image payloads. Pages are sequential, with a one-second pause between pages.
- A completed pass is followed by a ten-minute wait. A long pass cannot queue overlapping passes. At roughly 1.6 million sequences, the pause budget is about 160 seconds plus query time; this is an estimate, not a measured production duration.
- One complete source traversal is required per pass. Verify the covering plan and observe the first pass before changing the cadence or batch size. No model or provider calls occur.

The last complete durable snapshot remains available during reconciliation. A page, timeout, cancellation or persistence error retains its totals and original freshness timestamp. Only a successful complete pass atomically replaces `report_counts_total` and then the in-memory response. Reloading a persisted snapshot does not make its timestamp current. A cold service without a complete snapshot returns HTTP 503 with `Retry-After: 30`, rather than reporting an unknown count as zero.

There is no synchronous count refresh in a public request. Source changes after their page was read can appear on the following pass; this remains a periodically reconciled aggregate, not a transactional real-time snapshot. `last_updated` identifies the last completed reconciliation, not the last ingested report.

Validation:

```sh
go test ./backend/server -run TestReportCounter -count=1
```

The real MySQL fixture is opt-in with `REPORT_COUNTER_TEST_DSN`, restricted to local TCP and a fresh dedicated `report_counter_test` schema. It proves late low-sequence analysis, multi-language distinctness, reclassification, invalidation, deletion, durable/cache parity and an ordered covering-index plan. It refuses existing fixture tables and never touches production data.
