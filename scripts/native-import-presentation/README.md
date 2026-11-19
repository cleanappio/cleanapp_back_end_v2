# Improve source tracing

Display the issue described by a source report as its title, with a compact
source label stored separately. Preserve original attribution, observation
timestamps, verification status, raw metadata, canonical identity and quiet
import policies in detailed evidence. Titles use source text or public taxonomy,
never contributor identity or paid model generation.

The native writer keeps immutable candidate/material hashes unchanged. Replay
and rollback accept only the entire original presentation or the entire
reviewed projection. Mixed or independently curated records remain protected.
The existing receiver, guard and finite budget controls remain independently
digest-bound by the live policy.

`epoch.py` and its unchanged host-local SQL transport provide the reviewed native
writer. Credentials stay in the existing database container. This package does
not replace the receiver or its source acquisition and privacy controls.

## Validate

```sh
python3 -m unittest discover -s scripts/native-import-presentation -p 'test_*.py'
python3 -m unittest discover -s scripts/solvesf-import -p 'test_*.py'
```

Runtime source and test-receipt digests must match an actual isolated MySQL test
run before installation. Pause the existing imports under the shared deployment
lock, retain private rollback copies, and update only affected source/test
bindings. Preserve all source limits, deadlines, images and immutable staging.

## Repair existing records

`backfill.py` defaults to a bounded read-only dry run. Select an adapter and an
explicit inclusive sequence fence. Keyset batches avoid image reads and global
aggregation. Apply requires stopped importers, the existing deployment boundary
held by an external operator, and a fresh private write-ahead rollback journal.
It modifies only the analysis title, summary and display source after a complete
exact legacy match. Both retained description fields, source ledgers, privacy
flags, media and canonical IDs remain unchanged. Avoiding updates to image-bearing
report rows also avoids copying their BLOBs into full-row database logs.

A partial run reports its last sequence; resume within the same fence with a new
private journal. A prepared journal entry does not prove a transaction committed.
Fresh public detail and discovery reads verify the resulting display directly;
no queue publication or model calls are needed.
