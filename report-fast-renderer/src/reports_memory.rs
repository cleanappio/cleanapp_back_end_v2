use std::{
    collections::{BTreeMap, BTreeSet},
    io,
    sync::{Arc, RwLock},
};

use mysql as my;

use crate::db;
use crate::model::{BrandSummaryItem, ReportPoint, ReportWithAnalysis};
use cleanapp_rustlib::rabbitmq::subscriber::{permanent, Callback, Message};

/// A refresh records queue writes while SQL runs outside this lock. Even a
/// removal of an absent point is a tombstone: an older SQL result must not
/// resurrect it. The fence is bounded to one nonoverlapping refresh cycle.
#[derive(Default)]
pub struct PhysicalReports {
    points: BTreeMap<i64, ReportPoint>,
    queue_writes: Option<BTreeSet<i64>>,
}
impl PhysicalReports {
    pub fn insert(&mut self, seq: i64, point: ReportPoint) {
        if let Some(writes) = &mut self.queue_writes {
            writes.insert(seq);
        }
        self.points.insert(seq, point);
    }
    pub fn remove(&mut self, seq: &i64) {
        if let Some(writes) = &mut self.queue_writes {
            writes.insert(*seq);
        }
        self.points.remove(seq);
    }
    pub fn values(&self) -> impl Iterator<Item = &ReportPoint> {
        self.points.values()
    }
    pub fn len(&self) -> usize {
        self.points.len()
    }
    pub fn keys_after(&self, after: i64, limit: usize) -> Vec<i64> {
        use std::ops::Bound::{Excluded, Unbounded};
        self.points
            .range((Excluded(after), Unbounded))
            .take(limit)
            .map(|(&seq, _)| seq)
            .collect()
    }
    pub fn begin_refresh(&mut self) -> anyhow::Result<()> {
        anyhow::ensure!(self.queue_writes.is_none(), "overlapping physical refresh");
        self.queue_writes = Some(BTreeSet::new());
        Ok(())
    }
    pub fn cancel_refresh(&mut self) {
        self.queue_writes = None;
    }
    pub fn finish_refresh(&mut self, ids: &[i64], rows: Vec<ReportPoint>) -> anyhow::Result<usize> {
        let requested: BTreeSet<_> = ids.iter().copied().collect();
        let mut selected = BTreeMap::new();
        for point in rows {
            anyhow::ensure!(requested.contains(&point.seq), "unexpected refresh point");
            anyhow::ensure!(
                !selected.contains_key(&point.seq),
                "duplicate refresh point"
            );
            anyhow::ensure!(
                has_map_location(point.latitude, point.longitude),
                "invalid refresh location"
            );
            selected.insert(point.seq, point);
        }
        let writes = self
            .queue_writes
            .take()
            .ok_or_else(|| anyhow::anyhow!("refresh fence missing"))?;
        let mut applied = 0;
        for id in ids {
            if writes.contains(id) {
                continue;
            }
            if let Some(point) = selected.remove(id) {
                self.points.insert(*id, point);
            } else {
                self.points.remove(id);
            }
            applied += 1;
        }
        Ok(applied)
    }
}

pub struct InMemoryReports {
    physical_content: Arc<RwLock<PhysicalReports>>,
    pub refresh_status: Arc<RwLock<crate::quiet_refresh::RefreshStatus>>,
    digital_content: Arc<RwLock<BTreeMap<String, BrandSummaryItem>>>,
    pool: my::Pool,
}

impl InMemoryReports {
    pub async fn new(pool: my::Pool) -> Self {
        Self {
            physical_content: Arc::new(RwLock::new(PhysicalReports::default())),
            refresh_status: Arc::new(RwLock::new(crate::quiet_refresh::RefreshStatus::default())),
            digital_content: Arc::new(RwLock::new(BTreeMap::new())),
            pool,
        }
    }
    pub fn get_digital_content(&self) -> Arc<RwLock<BTreeMap<String, BrandSummaryItem>>> {
        self.digital_content.clone()
    }
    pub fn get_physical_content(&self) -> Arc<RwLock<PhysicalReports>> {
        self.physical_content.clone()
    }
}

impl Callback for InMemoryReports {
    fn on_message(&self, message: &Message) -> Result<(), Box<dyn std::error::Error>> {
        let started_at = std::time::Instant::now();

        let res = self
            .decode_report_message(&message.body)
            .map_err(|e| permanent(io::Error::other(e.to_string())))?;

        tracing::debug!("Parsed ReportWithAnalysis message successfully");
        let report = &res.report;
        tracing::info!("Got a new report: seq={}", report.seq);

        let (classification, severity_level, brand_name, brand_display_name) = res
            .analysis
            .iter()
            .filter(|a| a.language == "en")
            .map(|a| {
                (
                    a.classification.as_str(),
                    a.severity_level,
                    a.brand_name.as_str(),
                    a.brand_display_name.as_str(),
                )
            })
            .next_back()
            .unwrap_or(("", 0f64, "", ""));

        match classification {
            "physical" => {
                let mut physical_lock = self.physical_content.write().unwrap_or_else(|e| {
                    panic!("Failed to acquire lock on physical_content: {}", e)
                });
                if !has_map_location(report.latitude, report.longitude) {
                    physical_lock.remove(&report.seq);
                    return Ok(());
                }
                physical_lock.insert(
                    report.seq,
                    ReportPoint {
                        severity_level,
                        seq: report.seq,
                        latitude: report.latitude,
                        longitude: report.longitude,
                    },
                );
            }
            "digital" => {
                let mut digital_lock = self
                    .digital_content
                    .write()
                    .unwrap_or_else(|e| panic!("Failed to acquire lock on digital_content: {}", e));
                match digital_lock.get_mut(brand_name) {
                    Some(item) => {
                        item.total += 1;
                    }
                    None => {
                        digital_lock.insert(
                            brand_name.to_string(),
                            BrandSummaryItem {
                                brand_name: brand_name.to_string(),
                                brand_display_name: brand_display_name.to_string(),
                                total: 1,
                            },
                        );
                    }
                }
            }
            other => {
                tracing::warn!("Unknown classification type: {}", other);
            }
        }

        tracing::debug!(
            "report_fast_renderer on_message finished seq={} duration_ms={}",
            report.seq,
            started_at.elapsed().as_millis()
        );
        Ok(())
    }
}

fn has_map_location(lat: f64, lon: f64) -> bool {
    lat.is_finite()
        && lon.is_finite()
        && (-90.0..=90.0).contains(&lat)
        && (-180.0..=180.0).contains(&lon)
        && (lat != 0.0 || lon != 0.0)
}

impl InMemoryReports {
    fn decode_report_message(&self, body: &[u8]) -> anyhow::Result<ReportWithAnalysis> {
        if let Ok(report_with_analysis) = serde_json::from_slice::<ReportWithAnalysis>(body) {
            if report_with_analysis.report.seq > 0 {
                return Ok(report_with_analysis);
            }
        }

        #[derive(serde::Deserialize)]
        struct SeqEnvelope {
            seq: i64,
        }

        if let Ok(seq) = serde_json::from_slice::<i64>(body) {
            if seq > 0 {
                return db::fetch_report_with_analysis(&self.pool, seq);
            }
        }

        if let Ok(envelope) = serde_json::from_slice::<SeqEnvelope>(body) {
            if envelope.seq > 0 {
                return db::fetch_report_with_analysis(&self.pool, envelope.seq);
            }
        }

        Err(anyhow::anyhow!(
            "failed to decode report-fast-renderer message body: {}",
            String::from_utf8_lossy(body)
        ))
    }
}
