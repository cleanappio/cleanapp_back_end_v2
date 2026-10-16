# Graceful shutdown

`index_bluesky`, `analyzer_bluesky`, `submitter_bluesky`, `bluesky_now`, and
`news-indexer`'s `analyzer_twitter` register persistent SIGTERM and SIGINT
listeners before startup. A pending shutdown prevents another batch or message.
The first batch still runs immediately; subsequent batch delays are unchanged.

Batch workers finish their current `run_once`, including network responses and
database receipts, before disconnecting the database pool. The stream finishes
the current `process_message` before checking shutdown again. This preserves its
existing cursor-before-store sequence; shutdown does not cancel work between
those writes. Idle stream reads, connection attempts and reconnect delays can
be interrupted without starting another message. The existing `--once` limit
of100 messages remains unchanged.

Shutdown has no forced work timeout. A current batch can take longer than a
Docker client wait; an unknown stop must settle before restarting the container.
This change does not make an older deployed binary handle signals. Release and
old-runtime retirement require their own reviewed procedure.

The process tests send both real signals during startup, active batch receipts,
the stream's cursor-to-store section, and idle waits. They run owned local
fixtures without database, ingestion, model or outbound-message calls.

## Preserve the deployed source before replacement

The repository can lag a custom production build. Before this maintenance change,
we recovered the generation-pinned source archives behind successful Cloud Builds
`85dabdea-6867-4eb4-899d-6446ec56549f` (the deployed indexer, analyzer and stream)
and `c72dcd1b-31ee-4a9c-9715-cfdb00050280` (the deployed submitter). The first
archive contained freshness, physical/photo selection and media handling that
were missing from the repository. Those business rules were preserved while
adding shutdown; the submitter's existing business code already matched.

Activation must bind the full scanned build context and its hashes to the
resulting image digest and selected runtime configuration. A version label or
new build from main alone does not establish that live behavior was preserved.
These shutdown changes do not establish that the separate report-loading issue
has been resolved.
