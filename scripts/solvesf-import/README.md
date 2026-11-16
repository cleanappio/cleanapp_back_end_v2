# SolveSF public-feed import

**Production canary verified on November 16, 2026:** four reports and four photos
passed native SQL, public metadata, JPEG and map checks at 20:31 UTC. Runtime
polling state is recorded separately in the root-owned AWS deployment receipt
and systemd; installing the source or unit files does not establish activation.

This importer reads the public feed selected by the user at `solvesf.com`. It
does not enumerate all SF311 records, submit cases to the city, contact reporters,
or make model calls. CLI collection is finite. The optional controller and
systemd units implement a bounded hourly job; the supplied units are disabled
until root commissions and enables them after a verified canary.

## Scope and identity

The public UI supports citywide `3d`, `24h`, and `today` windows. Collection
defaults to `3d`; the API requires an area filter for `7d`, so citywide `7d` is
rejected. During November 16, 2026 checks, citywide `3d` returned HTTP 503 while
explicit `today` returned HTTP 200. A five-case canary can therefore explicitly
use `--window today`. The selected window is recorded in `stats.json`; the
collector never silently falls back or claims whole-history completeness.

SolveSF publishes both city-API records and open-data imports. A record's
`source=open311` or `source=socrata` identifies its underlying feed, not the app
or person that submitted it. Imported records retain:

- canonical identity `source_namespace=sf311`, `source_id=<original city case ID>`;
- publisher `SolveSF` and the original `https://solvesf.com/report/<case ID>` link;
- allowlisted public description, city status, public photos, and exact decimal
  coordinates;
- the original timestamp strings, including offsets, alongside normalized UTC;
- `submitter_origin=unknown`, `description_authorship=unverified_may_be_ai`, and
  `resolution_verified=false`.

The description may have been generated or assisted by AI. City closure/status
is retained as a source statement, not evidence that CleanApp verified a fix.
The `sf311` identity prevents a future direct city-feed import from creating a
second report for the same city case.

## Finite collection and planning

Use Python 3.12 or newer. Collection itself uses the standard library. Media
verification additionally requires the exact Pillow version in
`requirements.txt`.

```sh
umask 077
python3 -B collect.py --state-dir /tmp/cleanapp-solvesf-canary --window today --max-reports 5
```

The collector makes unauthenticated public GETs, at most one request start per
second. It limits feed/detail response sizes, rejects incomplete/non-JSON
responses, and follows no API redirects. HTTP 403, 429, server failures and
network errors stop the run. There are no automatic retries.

The private state directory contains the SQLite checkpoint, `candidates.jsonl`,
`held-revisions.jsonl`, and `stats.json`. Output files are replaced atomically;
the manifest is replaced last. Verify its file digests before consuming a
snapshot. A network interruption can preserve completed per-case checkpoints
without publishing a partial new snapshot; rerunning the same command resumes.
Unchanged discovery records avoid detail requests. `--max-reports` defaults to
100 and bounds work per run; the eligible snapshot can contain earlier retained
candidates, so explicitly stage at most five cases for this writer.

The read-only plan requires the SHA256 of that exact staged candidate file:

```sh
python3 -B apply.py --mode plan --candidates /path/to/candidates.jsonl --candidate-sha256 <sha256>
python3 -B media.py --candidates /path/to/candidates.jsonl --candidate-sha256 <sha256>
```

These commands neither connect the plan to production MySQL nor archive media.
The native writer accepts at most five cases per invocation. Production apply
is a separate root-commissioned operation; candidate input cannot grant access
or select a different guard.

## Evidence, privacy, and replay

Only publication fields are retained. Operational session URLs, user identifiers,
tokens, and nested operational payloads are excluded. Explicit private/deleted
records, invalid individual records, and missing detail records receive durable
metadata-only holds containing city ID, static reason, and first/last-seen times.
They do not block later valid cases. Access failures are not converted into
record holds.

Missing descriptions and cases with more than four photos are retained and held
as whole cases; photos are not silently discarded to fit the current reader.
Changed source material is retained as a held revision. All held IDs are excluded
from eligible snapshots and remain held on later observations. The first
candidate and prior versions are not overwritten.

Before apply, `media.py --execute` archives allowlisted source originals and
separate reproducible display images in the existing private AWS backup bucket,
with byte/digest and complete readback verification. It writes a private media
manifest bound to the candidate-file digest. Apply rechecks the current source,
archive contents, display derivation, and privacy/mutation evidence. A source or
media change holds the case.

Native case identity is not a photo hash. Cross-source or deleted-media matches
require reconciliation. The dedicated city-case ledger survives report deletion;
replay cannot resurrect a removed report. The separate media ledger retains
indexed original/display hashes for this boundary.

Every inserted report has all five side-effect policy flags enabled:
`suppress_email`, `suppress_notifications`, `suppress_rewards`,
`suppress_routing`, and `suppress_analysis`. The source import does not enqueue
normal processing to produce these effects.

## Root commissioning checklist

This is an operational checklist, not a script that self-approves deployment.
Preserve unrelated runtime configuration and use the existing shared deployment
and backup lock.

1. Bind the exact source revision/bundle and SQL helper. Apply the canonical
   additive migration in `go-common/sf311import/schema.sql` through the normal
   migration path; verify both case and media ledgers, identity indexes, delete
   behavior, and the actual live schema. The importer performs no DDL.
2. Run offline guard/collector/media tests and the disposable actual-MySQL
   atomicity, replay, revision, privacy-drift, schema, and no-resurrection tests.
   Actual-MySQL fixtures execute real schema/projection/privacy SQL, while their
   host, cloud, stopped-OLM and journal controls are mocked and do not commission
   the production guard. Produce the root-owned
   `actual-tests.json` receipt bound to the exact code, helper, and schema digests.
3. Quiesce `olm-native-receiver.service`, prove its process is stopped, and bind
   the unchanged receiver/policy/cursor files. Copy mutation state at the safe
   cursor; do not advance or reset OLM state to accommodate this source.
4. Commission fresh complete indexed privacy/media coverage, live guard evidence,
   mutation/suffix fences, current host/server identity, verified recovery
   evidence, capacity reserve, and the private source/media archives. A hash-only
   manifest or an old backup claim is insufficient.
5. Install reviewed root-owned code and private evidence under
   `/srv/cleanapp-solvesf-import`. Bind the exact candidates, media manifest,
   current guards, source files, mutation cursor, and a short expiry in the
   root-owned `evidence/controls.json`. Use the fixed guard and deployed OLM
   library locations; source input cannot relax these controls.
6. Apply no more than five cases under both the shared deployment/backup lock
   and native import advisory lock. Verify the report, raw provenance, analysis,
   external identity, side-effect flags, media, and durable case mappings together.
   Re-run the identical batch to prove replay skips rather than duplicates.
7. Seed/protect the imported asset hashes in mutation state without rewriting
   prior protected history or moving the safe cursor. Verify OLM evidence remains
   unchanged, then resume the receiver through its established controls. Keep
   changed, deleted, conflicting, or privacy-held cases protected.
8. Before enabling recurrence, bind and verify the controller source/test
   receipt and explicit time, case, cadence, and archive-byte budgets described
   below. Verify `service.py --check`, then the first finite controller cycle,
   guarded replay, receiver resumption, and actual timer state. Record the exact
   configuration expiry and observed lifecycle.

Record actual counts, receipts, and runtime state below before claiming the
canary complete. Configure future polling only after verified canary/replay and
explicitly record that lifecycle; shipping unit files does not enable a timer.

| Production result | Verified value |
| --- | --- |
| Core source revision and initial bundle | `38038c5c250a2706064375cf153524adfbed0680`; SHA256 `7ad2af2d5f0a7c94d90381dd3199430f98fe188ae5c98befd8d78fae0c56e766` |
| Collection window and evolving eligible/held counts | Explicit `today`; current counts in `state/canary/stats.json` |
| First cases inserted and public verification | Four cases and four JPEG previews; `evidence/live-canary.json` passed November 16 at 20:31 UTC |
| Identical replay skipped / duplicates | Read `evidence/replay.json` for the checked production replay result; absence is not verification |
| All five side-effect flags | Verified on all four initial cases |
| OLM receiver resumed and cursor preserved | Verified after the first cycle; receiver/configuration/policy files preserved across its owned pause |
| Polling activation | Read `evidence/deployment.json` and actual `cleanapp-solvesf-import.timer` state; source installation alone is insufficient |
| Initial configuration expiry and limits | November 23, 2026 at 20:08:58 UTC; at most five/hour, 500 total cases, 40 GiB archived evidence |

The fixed evidence paths above are under `/srv/cleanapp-solvesf-import` on AWS.
An example verified public report is
[SF311 case via SolveSF](https://www.cleanapp.io/physical/report/rpt_1wz6bprjyh8RAh_PNgSS1Q).
The first schema installation encountered metadata locks held by a background
email-summary read. Installation retained the canonical foreign keys and
verified the deployed schema before publishing any source reports. Its private
migration and recovery receipts retain the operational evidence.

## Optional finite hourly controller

`service.py --check` is the default read-only configuration, source/test receipt,
host/library, schema, and native-inventory check. `service.py --run` performs one
bounded cycle. Both use the fixed root-owned
`/srv/cleanapp-solvesf-import/evidence/controller.json`; candidate input cannot
provide another controller configuration.

Configuration must have kind `cleanapp_solvesf_controller.v1`, explicit
`starts_at`/`expires_at`, and the following enforced envelope:

| Setting | Allowed bound |
| --- | --- |
| Lifetime | At most seven days |
| Window | Explicit `today` only |
| `cases_per_run` | At most five |
| `interval_seconds` | At least 3,600 |
| `maximum_native_cases` | At most 500 total canonical `sf311` cases |
| `maximum_archive_bytes` | At most 40 GiB cumulative staging reservations |

Root can select smaller limits. The archive-byte limit includes conservative
reservations for failed or uncertain uploads; capacity is released only after
verified object sizes. It is a data-volume limit, not a currency billing cap.
The controller binds the exact hashes of `service.py`, `collect.py`, `media.py`,
`apply.py`, and `guard.py`, the actual-test receipt, the deployed OLM base policy,
and a controller-test receipt bound to the service source.

Each cycle resumes only its own previously verified pause, honors expiry and
cadence, inventories already-recorded city IDs, and collects up to five detail
records using the durable collector state. It stages eligible new cases and
private originals/previews **before** pausing OLM. A source outage during this
stage never pauses the receiver. Only after staging does it own a short OLM
pause, refresh coverage and guard evidence, copy mutation state, commission
digest-bound short-lived apply controls, and invoke the same guarded writer.

Resuming OLM requires an owned pause marker, unchanged receiver/configuration/
policy files, fresh live guard validation, all five quiet flags on imported
reports, and protected original/display asset hashes. An uncertain commit or
missing protection leaves the owned pause in place for operator attention. A
later invocation must verify recovery before doing new collection; it never
blindly starts a receiver paused by another operator. Expiry or the native-case
limit disables this controller's timer. Archive exhaustion refuses new staging.

The supplied `cleanapp-solvesf-import.service` runs on the existing AWS host with
`MemoryMax=1G`, `CPUQuota=25%`, `Nice=15`, `UMask=0077`, and a 30-minute start
timeout. The reviewed root OLM transport invokes `sudo`, so the service uses
`NoNewPrivileges=false`. The supplied disabled timer has its first activation after one hour, subsequent
hourly activations, and up to three minutes of jitter; it does not catch up missed
runs. Root must install and explicitly enable it after commissioning. Today-only
discovery can miss older or unavailable cases and does not claim historic coverage.

## Tests and CI

```sh
python3 -m pip install --requirement requirements.txt
python3 -B -W error::ResourceWarning -m unittest discover -p 'test_*.py' -v
```

The narrow `solvesf-import.yml` workflow runs Python 3.12 tests with pinned
Pillow, including offline controller limits/recovery tests, compiles/tests the
new Go migration package, and runs all three opt-in
actual-MySQL methods against official `mysql:8.0.44`. These exercise atomicity,
replay, no-resurrection, live guard SQL projection/privacy drift, and schema
tombstone drift. Host/cloud and stopped-OLM/journal controls remain mocked in
these fixtures. Its disposable fixture is named
`ca-import-test-mysql`, labelled `cleanapp.solvesf.fixture=true`, limited to 1 GiB,
has `--network none`, and publishes no ports. The fixture database is
`cleanapp_dataunion_import_test`; `SOLVESF_TEST_MYSQL=1` only opts into that test.
Cleanup runs even after a failed test. These synthetic tests do not supply
production guard, source-freshness, privacy-coverage, or deployment approval.

The workflow also reruns when any of its three borrowed helpers changes:
`candidate_data.py`, `mysql_client.py`, or `integration_test.py` under
`scripts/historical-imports/dataunion_0_1_0/`.

CI has no production credentials, public-feed polling, S3 writes, or deployment
steps. Existing repository workflows remain unchanged.
