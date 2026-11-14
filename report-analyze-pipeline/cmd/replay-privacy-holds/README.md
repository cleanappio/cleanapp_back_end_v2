# Replay a corrected payment-privacy hold

Finite operator maintenance for explicitly reviewed existing machine reports.
Deploy the reviewed privacy fix to the analyzer before applying. This command
has no model client, creates no reports/source IDs, and makes no SQL writes,
permission changes or outcome edits. The normal worker rechecks live policy and
hydrates canonical source data before its ordinary analysis/evidence pass.

Default execution is read-only: supply one to ten explicit sequences. It emits
only receipt identity and source/state hashes as a review manifest. It does not
construct a broker publisher or create a journal. For today's reviewed cohort,
only sequence1592439 is intended; the other four reports do not need reanalysis.

Build from the reviewed source in the analyzer module. Run in the approved
machine-worker configuration environment; no credentials go on the command line.
The exchange, routing key and queue must be `cleanapp-exchange`, `report.raw` and
`report-analysis-queue`. This never publishes to the human routing key.

```sh
go build -o /approved/private/replay-privacy-holds ./cmd/replay-privacy-holds
umask 077
/approved/private/replay-privacy-holds --seqs 1592439 > /approved/private/review.json
```

Review the exact receipt and hashes, the deployed analyzer revision, and the
ordinary analysis/retry cost before applying. The current receipt must still
have `held/payment_information_detected`, processing `not_required`, no image,
no saved analysis in any language and no publication marker. Consent must already
be version1 automatic publication/notices. The new shared policy must find no
privacy or policy hold, permit analysis, and retain adequate original/context.
Every check and both hashes are reloaded under the existing per-sequence lock.

```sh
/approved/private/replay-privacy-holds --seqs 1592439 --apply \
  --manifest /approved/private/review.json \
  --journal /approved/private/replay-journal.json
```

The manifest must be a regular private file; the journal requires an existing
0700 parent directory and is atomically stored as0600. Always reuse this manifest
and journal for the same maintenance action. An attempted phase is persisted
before one persistent, mandatory, broker-confirmed publication of only `{seq}`.
The stable message ID binds the sequence and source hash. Confirmed entries skip
on rerun. Any uncertain attempt refuses an automatic resend, including the
broker-ACK/journal-write gap; inspect actual worker/receipt state before an
operator chooses another action. This is not broker-level deduplication.

The first failed recheck, journal write or publication stops the bounded run and
returns an opaque error plus any confirmed sequences. Raw text, source URLs,
capabilities and upstream errors are never printed. Enqueue confirmation is not
analysis completion, public publication or recipient delivery; use the original
receipt to follow those normal pipeline outcomes. No sweep or recurring job is
installed, and no model/provider calls are made by the command itself.
