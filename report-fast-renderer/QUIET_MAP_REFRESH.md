# Quiet physical map refresh

Native historical and SolveSF imports commit public reports and valid physical
analyses without publishing queue messages. The renderer previously loaded those
rows only at startup. The refresh reads SQL directly and changes only the local
physical map; it publishes no messages and calls no model, email, notification,
reward, routing or source API.

The existing startup SQL and bounded refresh share the same predicates and
aggregation: valid physical analysis (all languages), active or missing status,
public or missing/empty ownership, valid nonzero coordinates, and maximum valid
physical severity. No schema or index migration is introduced. Startup's existing
full query and connection pool keep their previous timeout behavior.

## Bounded work and eventual reconciliation

One worker runs immediately and then on 30 second ticks, skipping missed ticks.
Each cycle unions and deduplicates four bounded lanes:

| Lane | Maximum IDs | Purpose |
| --- | ---: | --- |
| Policy keys greater than the discovery cursor | 4096 | New quiet SQL rows |
| Most recent policy keys | 1024 | Ordinary transactions committing out of sequence |
| Rotating policy keys | 4096 | Arbitrarily late commits below discovery cursor |
| Rotating current physical cache keys | 4096 | Deleted rows/policies, private or inactive rows, changed analysis/location |

The last lane is necessary because deleting a report may cascade-delete its
policy. Scanning only surviving policy keys would never revisit that cached
point. Cache rotation also reconciles ordinary queue-fed physical rows using the
same publication predicates.

There are at most 13,312 candidate IDs per tick and 13 point SELECTs of at most
1024 IDs each. Policy enumeration uses its primary key; targeted point lookups
force `reports.PRIMARY` and reports-first join order. This avoids repeating the
full reports/analysis join over approximately 441,000 cached points each tick.
The ceiling is about 444 candidate checks/second averaged over 30 seconds, versus
about 14,700 for a full 441,000 point join every 30 seconds. These are work bounds,
not measured MySQL CPU, query time, I/O, or financial costs. At idle, overlapping
lanes deduplicate substantially; no-new-row discovery does not fetch full reports.

At approximately 353,000 policy keys, the policy rotor takes at least 87 successful
ticks (43.5 minutes). At 441,000 cached keys, the cache rotor takes at least 108
successful ticks (54 minutes). Allow a wrap tick and growth; failures delay these
bounds. New rows are normally found by the incremental or recent lane within one
30 second tick plus query time. This is not a universal 30 second SLA: a backlog
over 4096 new policy IDs/tick or an arbitrarily late commit outside the latest
1024 keys relies on catch-up/rotation. Existing source canary checks at 0/30/60
seconds retain their exact membership assertions; under normal indexed healthy
cycles (20 second budget) a new recent source row is visible before the last
check. A failed canary is an error to investigate, not permission to relax the
membership assertion or duplicate the import.

The initial discovery cursor is captured **before** the full startup SQL snapshot,
so a transaction committing during startup is replayed. AUTO_INCREMENT is not
assumed to be commit order. Worker cursors advance only after an entire valid
cycle is committed to the map.

## Failures, concurrency, shutdown and health

A dedicated connection has 5 second TCP connect/write timeouts, a 6 second socket
read timeout, session-local `lock_wait_timeout=5`, and a 5000 ms MySQL SELECT hint.
A cycle has a 20 second soft budget, checked between statements and before map
mutation. Socket/server bounds limit the current statement; no overlapping or
detached SQL workers accumulate. The refresh never changes table data, grants,
global application configuration, or the existing callback pool. Its one
session setting only bounds that connection's metadata lock wait.

All SQL runs outside the physical map lock. A cycle records queue-touched keys;
when applying its SQL snapshot, it skips keys touched by newer queue callbacks.
This includes removal of an already absent key, preventing resurrection. Untouched
keys absent from filtered SQL are removed. Any SQL failure, deadline, stop, invalid
response, or incomplete cycle retains all existing points and cursors; the next
tick retries. The per-cycle fence is cleared on every exit path. Only bounded map
key collection and final atomic application take the lock.

Shutdown wakes the timer, sets a stop flag, and awaits the active blocking query.
Results observed after stopping are discarded before applying. It does not abort
and detach a `spawn_blocking` database operation.

`/health` keeps HTTP 200 liveness (existing OLM/SolveSF guards depend on that
contract). Its JSON `map_refresh` and `/stats`'s `physical_map_refresh` include
`state`, `last_success_unix_seconds`, `consecutive_failures`, and a sanitized fixed
`error_code`; after a failure the top-level health JSON is `degraded`. No SQL,
DSN, password, report body, or source URL is included. A normal startup reload
alone must not be accepted as proof that periodic publication works.

## Tests and deployment preflight

Run `cargo test --locked`, `cargo clippy --locked --all-targets -- -D warnings`,
and `cargo fmt --check`. Rust tests cover quiet insertions, removal including
policy cascade, late commits, rotor wrap, bounded work at 441k cached points,
concurrent queue writes/tombstones, map-lock availability during SQL, incomplete
query failure/retry, stop/deadline cleanup, and worker shutdown. CI runs the full
renderer suite and an isolated MySQL fixture for actual SQL predicates, query
plans, and bounded lookup timing. Local Rust fixture timing does not establish
MySQL performance.

Before approving a renderer release, verify on the actual renderer account:

1. `SELECT` access to `report_side_effect_policy`, `reports`, `report_analysis`,
   `report_status`, and `reports_owners`; and permission for session-local
   `lock_wait_timeout`. No write grant is needed. Do not add a grant implicitly.
2. Required existing indexes: policy/reports/status primary keys, analysis index
   beginning with `seq`, and ownership primary/index beginning with `seq`.
3. Bounded lookup `EXPLAIN` uses primary report ranges and indexed joined lookups;
   a representative complete cycle finishes comfortably inside 20 seconds.
4. After process startup, health reports an actual new background refresh success
   timestamp and zero consecutive failures. Also verify a **subsequent** quiet SQL
   batch appears in the public points and tiles without restarting the renderer.
5. Existing backend version/health, OLM progress and quiet-effect invariants remain
   healthy. This patch authorizes no production deployment, restart or grant.
