use std::{
    collections::BTreeMap,
    io,
    sync::{Arc, RwLock},
};

use mysql as my;

use crate::db;
use crate::model::{BrandSummaryItem, ReportPoint, ReportWithAnalysis};
use cleanapp_rustlib::rabbitmq::subscriber::{permanent, Callback, Message};

pub struct InMemoryReports {
    physical_content: Arc<RwLock<BTreeMap<i64, ReportPoint>>>,
    digital_content: Arc<RwLock<BTreeMap<String, BrandSummaryItem>>>,
    pool: my::Pool,
}

impl InMemoryReports {
    pub async fn new(pool: my::Pool) -> Self {
        Self {
            physical_content: Arc::new(RwLock::new(BTreeMap::new())),
            digital_content: Arc::new(RwLock::new(BTreeMap::new())),
            pool,
        }
    }
    pub fn get_digital_content(&self) -> Arc<RwLock<BTreeMap<String, BrandSummaryItem>>> {
        self.digital_content.clone()
    }
    pub fn get_physical_content(&self) -> Arc<RwLock<BTreeMap<i64, ReportPoint>>> {
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
            .last()
            .unwrap_or(("", 0f64, "", ""));

        match classification {
            "physical" => {
                let mut physical_lock = self.physical_content.write().unwrap_or_else(|e| {
                    panic!("Failed to acquire lock on physical_content: {}", e)
                });
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
