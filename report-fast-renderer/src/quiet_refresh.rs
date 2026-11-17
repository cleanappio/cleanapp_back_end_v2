//! Quiet, SELECT-only repair of physical map points written without RabbitMQ.
//! Discovery and reconciliation use bounded keyset lanes, not a periodic full
//! reports/analysis join. See QUIET_MAP_REFRESH.md for latency and cost bounds.
use crate::{db::PhysicalRefreshDb, model::ReportPoint, reports_memory::PhysicalReports};
use anyhow::{ensure, Result};
use serde::Serialize;
use std::{
    collections::BTreeSet,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, RwLock,
    },
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use tokio::{sync::watch, task::JoinHandle};

pub const QUERY_CHUNK: usize = 1024;
const NEW_IDS: usize = 4096;
const RECENT_IDS: usize = 1024;
const ROTOR_IDS: usize = 4096;
const INTERVAL: Duration = Duration::from_secs(30);
const CYCLE_BUDGET: Duration = Duration::from_secs(20);

#[derive(Clone, Debug, Serialize)]
pub struct RefreshStatus {
    pub state: &'static str,
    pub last_success_unix_seconds: Option<u64>,
    pub consecutive_failures: u64,
    pub error_code: Option<&'static str>,
}
impl Default for RefreshStatus {
    fn default() -> Self {
        Self {
            state: "starting",
            last_success_unix_seconds: None,
            consecutive_failures: 0,
            error_code: None,
        }
    }
}
impl RefreshStatus {
    fn record(&mut self, success: bool) {
        if success {
            self.state = "healthy";
            self.last_success_unix_seconds = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .ok()
                .map(|v| v.as_secs());
            self.consecutive_failures = 0;
            self.error_code = None;
        } else {
            self.state = "degraded";
            self.consecutive_failures = self.consecutive_failures.saturating_add(1);
            // Never surface SQL/DSN/credential-bearing error messages in health.
            self.error_code = Some("quiet_map_sql_refresh_failed");
        }
    }
}

pub trait PointSource {
    fn policy_ids_after(&mut self, after: i64, limit: usize) -> Result<Vec<i64>>;
    fn recent_policy_ids(&mut self, limit: usize) -> Result<Vec<i64>>;
    fn points(&mut self, ids: &[i64]) -> Result<Vec<ReportPoint>>;
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Cursors {
    new_after: i64,
    policy_after: i64,
    cache_after: i64,
}
impl Cursors {
    fn new(startup_policy_seq: i64) -> Self {
        Self {
            new_after: startup_policy_seq,
            policy_after: 0,
            cache_after: 0,
        }
    }
}

type PhysicalMap = Arc<RwLock<PhysicalReports>>;

/// The Drop path also clears the per-cycle queue fence on SQL failure, stop,
/// deadline expiry or unwinding. It never changes the points on failure.
struct InFlight {
    map: PhysicalMap,
    active: bool,
}
impl InFlight {
    fn new(map: PhysicalMap) -> Result<Self> {
        map.write()
            .map_err(|_| anyhow::anyhow!("physical map lock"))?
            .begin_refresh()?;
        Ok(Self { map, active: true })
    }
    fn apply(
        mut self,
        ids: &[i64],
        points: Vec<ReportPoint>,
        stop: &AtomicBool,
        deadline: Instant,
    ) -> Result<usize> {
        let mut map = self
            .map
            .write()
            .map_err(|_| anyhow::anyhow!("physical map lock"))?;
        check_budget(stop, deadline)?;
        let applied = map.finish_refresh(ids, points)?;
        self.active = false;
        Ok(applied)
    }
}
impl Drop for InFlight {
    fn drop(&mut self) {
        if self.active {
            if let Ok(mut map) = self.map.write() {
                map.cancel_refresh();
            }
        }
    }
}

fn check_budget(stop: &AtomicBool, deadline: Instant) -> Result<()> {
    ensure!(!stop.load(Ordering::Acquire), "quiet refresh stopped");
    ensure!(Instant::now() < deadline, "quiet refresh deadline");
    Ok(())
}

fn checked_ids(ids: &[i64], limit: usize, after: Option<i64>) -> Result<()> {
    ensure!(ids.len() <= limit, "policy ID bound");
    ensure!(ids.iter().all(|&id| id > 0), "invalid policy ID");
    if let Some(after) = after {
        ensure!(
            ids.first().is_none_or(|id| *id > after),
            "policy keyset cursor"
        );
        ensure!(ids.windows(2).all(|v| v[0] < v[1]), "policy keyset order");
    } else {
        ensure!(ids.windows(2).all(|v| v[0] > v[1]), "recent policy order");
    }
    Ok(())
}

fn cycle<S: PointSource>(
    source: &mut S,
    map: &PhysicalMap,
    cursors: &mut Cursors,
    stop: &AtomicBool,
    deadline: Instant,
) -> Result<usize> {
    check_budget(stop, deadline)?;
    let flight = InFlight::new(map.clone())?;
    let mut next = cursors.clone();

    let new = source.policy_ids_after(cursors.new_after, NEW_IDS)?;
    checked_ids(&new, NEW_IDS, Some(cursors.new_after))?;
    next.new_after = new.last().copied().unwrap_or(cursors.new_after);
    check_budget(stop, deadline)?;
    // AUTO_INCREMENT allocation is not commit order. A recent overlap repairs
    // ordinary late commits quickly, independently of the discovery cursor.
    let recent = source.recent_policy_ids(RECENT_IDS)?;
    checked_ids(&recent, RECENT_IDS, None)?;
    check_budget(stop, deadline)?;
    let mut policy = source.policy_ids_after(cursors.policy_after, ROTOR_IDS)?;
    checked_ids(&policy, ROTOR_IDS, Some(cursors.policy_after))?;
    if policy.is_empty() && cursors.policy_after != 0 {
        check_budget(stop, deadline)?;
        policy = source.policy_ids_after(0, ROTOR_IDS)?;
        checked_ids(&policy, ROTOR_IDS, Some(0))?;
    }
    next.policy_after = policy.last().copied().unwrap_or(0);
    // Rotate the cache too: deleting a report can cascade-delete its policy,
    // so a policy-only rotor would never remove that cached point.
    let cached = {
        let locked = map
            .read()
            .map_err(|_| anyhow::anyhow!("physical map lock"))?;
        let mut ids = locked.keys_after(cursors.cache_after, ROTOR_IDS);
        if ids.is_empty() && cursors.cache_after != 0 {
            ids = locked.keys_after(0, ROTOR_IDS);
        }
        ids
    };
    next.cache_after = cached.last().copied().unwrap_or(0);
    let ids: Vec<_> = new
        .into_iter()
        .chain(recent)
        .chain(policy)
        .chain(cached)
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect();
    ensure!(
        ids.len() <= NEW_IDS + RECENT_IDS + 2 * ROTOR_IDS,
        "cycle point bound"
    );
    let mut points = Vec::new();
    for chunk in ids.chunks(QUERY_CHUNK) {
        check_budget(stop, deadline)?;
        let rows = source.points(chunk)?;
        ensure!(rows.len() <= chunk.len(), "point response bound");
        ensure!(
            rows.iter().all(|p| chunk.binary_search(&p.seq).is_ok()),
            "point outside query chunk"
        );
        points.extend(rows);
    }
    check_budget(stop, deadline)?;
    let applied = flight.apply(&ids, points, stop, deadline)?;
    *cursors = next; // Commit cursors only after the complete, validated map update.
    Ok(applied)
}

pub struct QuietRefresh {
    stop: Arc<AtomicBool>,
    wake: watch::Sender<bool>,
    task: JoinHandle<()>,
}
impl QuietRefresh {
    pub fn start(
        map: PhysicalMap,
        status: Arc<RwLock<RefreshStatus>>,
        startup_policy_seq: i64,
    ) -> Self {
        Self::start_with(
            map,
            status,
            startup_policy_seq,
            INTERVAL,
            PhysicalRefreshDb::connect,
        )
    }
    fn start_with<S, F>(
        map: PhysicalMap,
        status: Arc<RwLock<RefreshStatus>>,
        startup_policy_seq: i64,
        interval: Duration,
        connect: F,
    ) -> Self
    where
        S: PointSource + Send + 'static,
        F: Fn() -> Result<S> + Send + Sync + 'static,
    {
        let connect = Arc::new(connect);
        let stop = Arc::new(AtomicBool::new(false));
        let worker_stop = stop.clone();
        let (wake, mut stopped) = watch::channel(false);
        let task = tokio::spawn(async move {
            let mut cursors = Cursors::new(startup_policy_seq);
            let mut ticks = tokio::time::interval(interval);
            ticks.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
            loop {
                tokio::select! {
                    biased;
                    _ = stopped.changed() => break,
                    _ = ticks.tick() => {},
                }
                if worker_stop.load(Ordering::Acquire) {
                    break;
                }
                // Exactly one blocking SELECT worker. No DB work or network wait
                // runs on the async runtime or under a physical-map lock.
                let worker_map = map.clone();
                let cycle_stop = worker_stop.clone();
                let connect = connect.clone();
                let outcome = tokio::task::spawn_blocking(move || {
                    let deadline = Instant::now() + CYCLE_BUDGET;
                    let result = connect().and_then(|mut source| {
                        cycle(
                            &mut source,
                            &worker_map,
                            &mut cursors,
                            &cycle_stop,
                            deadline,
                        )
                    });
                    (cursors, result)
                })
                .await;
                match outcome {
                    Ok((next, result)) => {
                        cursors = next;
                        if worker_stop.load(Ordering::Acquire) {
                            break;
                        }
                        if let Ok(mut status) = status.write() {
                            status.record(result.is_ok());
                        }
                        match result {
                            Ok(applied) => tracing::info!(applied, "quiet SQL physical map refresh complete"),
                            Err(_) => tracing::warn!("quiet SQL physical map refresh failed; retaining current map; see map_refresh health"),
                        }
                    }
                    Err(_) => {
                        if let Ok(mut status) = status.write() {
                            status.record(false);
                        }
                        tracing::error!(
                            "quiet SQL physical map refresh worker failed; current map retained"
                        );
                        break;
                    }
                }
            }
        });
        Self { stop, wake, task }
    }
    pub async fn stop(self) {
        self.stop.store(true, Ordering::Release);
        let _ = self.wake.send(true);
        // A running SELECT has server-side 5s and socket 6s limits. Let it drain
        // before process shutdown; aborting spawn_blocking would detach SQL.
        let _ = self.task.await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        collections::BTreeMap,
        sync::{atomic::AtomicUsize, Barrier},
    };

    fn point(seq: i64, lat: f64) -> ReportPoint {
        ReportPoint {
            seq,
            latitude: lat,
            longitude: -122.4,
            severity_level: 0.5,
        }
    }
    fn map(rows: &[ReportPoint]) -> PhysicalMap {
        let mut map = PhysicalReports::default();
        for p in rows {
            map.insert(p.seq, p.clone());
        }
        Arc::new(RwLock::new(map))
    }
    fn rows(map: &PhysicalMap) -> Vec<(i64, f64)> {
        map.read()
            .unwrap()
            .values()
            .map(|p| (p.seq, p.latitude))
            .collect()
    }
    #[derive(Default)]
    struct Source {
        policy: BTreeSet<i64>,
        visible: BTreeMap<i64, ReportPoint>,
        lookups: Vec<Vec<i64>>,
        fail_on_lookup: Option<usize>,
    }
    impl Source {
        fn new(policy: &[i64], visible: &[ReportPoint]) -> Self {
            Self {
                policy: policy.iter().copied().collect(),
                visible: visible.iter().map(|p| (p.seq, p.clone())).collect(),
                ..Self::default()
            }
        }
    }
    impl PointSource for Source {
        fn policy_ids_after(&mut self, after: i64, limit: usize) -> Result<Vec<i64>> {
            use std::ops::Bound::{Excluded, Unbounded};
            Ok(self
                .policy
                .range((Excluded(after), Unbounded))
                .take(limit)
                .copied()
                .collect())
        }
        fn recent_policy_ids(&mut self, limit: usize) -> Result<Vec<i64>> {
            Ok(self.policy.iter().rev().take(limit).copied().collect())
        }
        fn points(&mut self, ids: &[i64]) -> Result<Vec<ReportPoint>> {
            self.lookups.push(ids.to_vec());
            ensure!(
                self.fail_on_lookup != Some(self.lookups.len()),
                "fixture DB failure"
            );
            Ok(ids
                .iter()
                .filter_map(|id| self.visible.get(id).cloned())
                .collect())
        }
    }
    fn run<S: PointSource>(
        source: &mut S,
        map: &PhysicalMap,
        cursors: &mut Cursors,
    ) -> Result<usize> {
        cycle(
            source,
            map,
            cursors,
            &AtomicBool::new(false),
            Instant::now() + CYCLE_BUDGET,
        )
    }

    #[test]
    fn quiet_insert_and_cached_deletion_or_visibility_removal() {
        let map = map(&[point(10, 1.), point(11, 2.)]);
        // 10 was deleted including its policy; 11 became private/invalid/inactive
        // and is therefore absent from the authoritative filtered SQL result.
        let mut source = Source::new(&[11, 12], &[point(12, 3.)]);
        run(&mut source, &map, &mut Cursors::new(11)).unwrap();
        assert_eq!(rows(&map), vec![(12, 3.)]);
        assert!(source.lookups.concat().contains(&10));
    }

    #[test]
    fn late_commit_below_high_water_is_repaired() {
        let map = map(&[]);
        let mut source = Source::new(&[50, 100], &[point(50, 1.), point(100, 2.)]);
        let mut cursors = Cursors::new(100);
        run(&mut source, &map, &mut cursors).unwrap();
        assert_eq!(rows(&map), vec![(50, 1.), (100, 2.)]);
        assert_eq!(cursors.new_after, 100);
        // Far older late commits also repair when the bounded policy rotor wraps.
        source.policy.extend(101..=(RECENT_IDS as i64 + 200));
        source.policy.insert(1);
        source.visible.insert(1, point(1, 4.));
        cursors.policy_after = i64::MAX;
        run(&mut source, &map, &mut cursors).unwrap();
        assert!(rows(&map).contains(&(1, 4.)));
    }

    #[test]
    fn partial_database_failure_preserves_every_point_and_cursor_then_retries() {
        let map = map(&[point(1, 10.)]);
        let ids: Vec<_> = (1..=1500).collect();
        let points: Vec<_> = ids.iter().map(|&id| point(id, 2.)).collect();
        let mut source = Source::new(&ids, &points);
        source.fail_on_lookup = Some(2); // first chunk succeeded, second fails
        let mut cursors = Cursors::new(0);
        let before = cursors.clone();
        assert!(run(&mut source, &map, &mut cursors).is_err());
        assert_eq!(rows(&map), vec![(1, 10.)]);
        assert_eq!(cursors, before);
        source.fail_on_lookup = None;
        run(&mut source, &map, &mut cursors).unwrap();
        assert_eq!(map.read().unwrap().len(), 1500);
        assert_eq!(cursors.new_after, 1500);
    }

    #[test]
    fn queue_updates_and_absent_removal_tombstones_win_during_sql() {
        struct ConcurrentSource {
            inner: Source,
            ready: Arc<Barrier>,
            done: Arc<Barrier>,
        }
        impl PointSource for ConcurrentSource {
            fn policy_ids_after(&mut self, a: i64, n: usize) -> Result<Vec<i64>> {
                self.inner.policy_ids_after(a, n)
            }
            fn recent_policy_ids(&mut self, n: usize) -> Result<Vec<i64>> {
                self.inner.recent_policy_ids(n)
            }
            fn points(&mut self, ids: &[i64]) -> Result<Vec<ReportPoint>> {
                self.ready.wait();
                self.done.wait();
                self.inner.points(ids)
            }
        }
        let map = map(&[point(1, 1.)]);
        let ready = Arc::new(Barrier::new(2));
        let done = Arc::new(Barrier::new(2));
        let mut source = ConcurrentSource {
            inner: Source::new(&[1, 2, 3], &[point(1, 2.), point(2, 2.), point(3, 2.)]),
            ready: ready.clone(),
            done: done.clone(),
        };
        let queued = map.clone();
        let callback = std::thread::spawn(move || {
            ready.wait();
            // This succeeds while the SQL fixture is blocked: SQL holds no map lock.
            let mut guard = queued.try_write().expect("SQL held physical map lock");
            guard.insert(1, point(1, 9.));
            guard.remove(&2); // absent-key tombstone must prevent resurrection
            drop(guard);
            done.wait();
        });
        run(&mut source, &map, &mut Cursors::new(0)).unwrap();
        callback.join().unwrap();
        assert_eq!(rows(&map), vec![(1, 9.), (3, 2.)]);
    }

    #[test]
    fn stop_or_deadline_after_select_discards_cycle_and_releases_fence() {
        struct StopSource<'a> {
            inner: Source,
            stop: &'a AtomicBool,
            stop_in_query: bool,
        }
        impl PointSource for StopSource<'_> {
            fn policy_ids_after(&mut self, a: i64, n: usize) -> Result<Vec<i64>> {
                self.inner.policy_ids_after(a, n)
            }
            fn recent_policy_ids(&mut self, n: usize) -> Result<Vec<i64>> {
                self.inner.recent_policy_ids(n)
            }
            fn points(&mut self, ids: &[i64]) -> Result<Vec<ReportPoint>> {
                if self.stop_in_query {
                    self.stop.store(true, Ordering::Release);
                } else {
                    std::thread::sleep(Duration::from_millis(5));
                }
                self.inner.points(ids)
            }
        }
        for stop_in_query in [true, false] {
            let map = map(&[point(1, 1.)]);
            let stop = AtomicBool::new(false);
            let mut source = StopSource {
                inner: Source::new(&[1], &[point(1, 2.)]),
                stop: &stop,
                stop_in_query,
            };
            let mut cursors = Cursors::new(0);
            let deadline = Instant::now()
                + if stop_in_query {
                    CYCLE_BUDGET
                } else {
                    Duration::from_millis(1)
                };
            assert!(cycle(&mut source, &map, &mut cursors, &stop, deadline).is_err());
            assert_eq!(rows(&map), vec![(1, 1.)]);
            assert_eq!(cursors, Cursors::new(0));
            map.write().unwrap().begin_refresh().unwrap();
            map.write().unwrap().cancel_refresh();
        }
    }

    #[test]
    fn invalid_snapshot_never_partially_mutates_map_or_cursor() {
        let map = map(&[point(1, 1.)]);
        let mut lock = map.write().unwrap();
        lock.begin_refresh().unwrap();
        assert!(lock
            .finish_refresh(&[1, 2], vec![point(1, 2.), point(2, 100.)])
            .is_err());
        assert_eq!(
            lock.values().map(|p| p.latitude).collect::<Vec<_>>(),
            vec![1.]
        );
        lock.cancel_refresh();
        lock.begin_refresh().unwrap();
        assert!(lock.begin_refresh().is_err());
        assert!(lock
            .finish_refresh(&[1], vec![point(1, 2.), point(1, 3.)])
            .is_err());
    }

    #[test]
    fn representative_cache_441k_has_bounded_work_and_rotors_wrap() {
        let cache: Vec<_> = (1..=441_000).map(|id| point(id, 37.7)).collect();
        let map = map(&cache);
        let ids: Vec<_> = (1..=353_000).collect();
        let mut source = Source::new(&ids, &cache);
        let mut cursors = Cursors::new(353_000);
        let started = Instant::now();
        run(&mut source, &map, &mut cursors).unwrap();
        let targets: usize = source.lookups.iter().map(Vec::len).sum();
        assert!(targets <= NEW_IDS + RECENT_IDS + 2 * ROTOR_IDS);
        assert!(source.lookups.iter().all(|v| v.len() <= QUERY_CHUNK));
        assert!(source.lookups.len() <= 13);
        assert_eq!(map.read().unwrap().len(), 441_000);
        // Prove the final cached key gets revisited (including a policy-cascade
        // deletion), rather than relying on only the live policy-key iterator.
        source.policy.remove(&441_000);
        source.visible.remove(&441_000);
        cursors.cache_after = 440_999;
        cursors.policy_after = 353_000;
        run(&mut source, &map, &mut cursors).unwrap();
        assert!(!rows(&map).iter().any(|&(id, _)| id == 441_000));
        assert_eq!(cursors.cache_after, 441_000);
        run(&mut source, &map, &mut cursors).unwrap();
        assert_eq!(cursors.cache_after, ROTOR_IDS as i64);
        eprintln!("441k cached-points fixture: first bounded cycle and two rotor steps {:?}; maximum targets/cycle {} (SQL performance unmeasured)",started.elapsed(),NEW_IDS+RECENT_IDS+2*ROTOR_IDS);
    }

    #[test]
    fn refresh_failure_is_visible_without_database_error_details() {
        let mut status = RefreshStatus::default();
        status.record(true);
        let last = status.last_success_unix_seconds;
        status.record(false);
        assert_eq!(status.state, "degraded");
        assert_eq!(status.last_success_unix_seconds, last);
        assert_eq!(status.consecutive_failures, 1);
        assert_eq!(status.error_code, Some("quiet_map_sql_refresh_failed"));
        status.record(true);
        assert_eq!(status.state, "healthy");
        assert_eq!(status.consecutive_failures, 0);
    }

    #[tokio::test]
    async fn failed_worker_is_observable_and_idle_shutdown_wakes_immediately() {
        let map = map(&[point(1, 1.)]);
        let status = Arc::new(RwLock::new(RefreshStatus::default()));
        let worker = QuietRefresh::start_with::<Source, _>(
            map.clone(),
            status.clone(),
            0,
            Duration::from_secs(3600),
            || anyhow::bail!("fixture-private-database-detail"),
        );
        tokio::time::timeout(Duration::from_secs(2), async {
            while status.read().unwrap().state != "degraded" {
                tokio::task::yield_now().await;
            }
        })
        .await
        .unwrap();
        let value = serde_json::to_string(&*status.read().unwrap()).unwrap();
        assert!(!value.contains("fixture-private-database-detail"));
        assert!(value.contains("quiet_map_sql_refresh_failed"));
        assert_eq!(rows(&map), vec![(1, 1.)]);
        tokio::time::timeout(Duration::from_secs(1), worker.stop())
            .await
            .unwrap();
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn worker_is_sequential_and_shutdown_drains_without_late_apply() {
        struct SlowSource {
            active: Arc<AtomicUsize>,
            max: Arc<AtomicUsize>,
            calls: Arc<AtomicUsize>,
        }
        impl PointSource for SlowSource {
            fn policy_ids_after(&mut self, a: i64, _: usize) -> Result<Vec<i64>> {
                Ok(if a == 0 { vec![1] } else { vec![] })
            }
            fn recent_policy_ids(&mut self, _: usize) -> Result<Vec<i64>> {
                Ok(vec![1])
            }
            fn points(&mut self, _: &[i64]) -> Result<Vec<ReportPoint>> {
                let active = self.active.fetch_add(1, Ordering::AcqRel) + 1;
                self.max.fetch_max(active, Ordering::AcqRel);
                self.calls.fetch_add(1, Ordering::AcqRel);
                std::thread::sleep(Duration::from_millis(50));
                self.active.fetch_sub(1, Ordering::AcqRel);
                Ok(vec![point(1, 2.)])
            }
        }
        let map = map(&[point(1, 1.)]);
        let active = Arc::new(AtomicUsize::new(0));
        let max = Arc::new(AtomicUsize::new(0));
        let calls = Arc::new(AtomicUsize::new(0));
        let factory = {
            let (active, max, calls) = (active.clone(), max.clone(), calls.clone());
            move || {
                Ok(SlowSource {
                    active: active.clone(),
                    max: max.clone(),
                    calls: calls.clone(),
                })
            }
        };
        let worker = QuietRefresh::start_with(
            map.clone(),
            Arc::new(RwLock::new(RefreshStatus::default())),
            0,
            Duration::from_millis(1),
            factory,
        );
        tokio::time::timeout(Duration::from_secs(2), async {
            while active.load(Ordering::Acquire) == 0 {
                tokio::task::yield_now().await;
            }
        })
        .await
        .unwrap();
        // Stop during an actual blocking lookup. Its later successful result is
        // discarded; stop awaits it rather than detaching the blocking worker.
        tokio::time::timeout(Duration::from_secs(2), worker.stop())
            .await
            .unwrap();
        assert_eq!(active.load(Ordering::Acquire), 0);
        assert_eq!(max.load(Ordering::Acquire), 1);
        assert_eq!(calls.load(Ordering::Acquire), 1);
        assert_eq!(rows(&map), vec![(1, 1.)]);
    }
}
