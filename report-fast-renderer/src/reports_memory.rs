use std::{
    collections::BTreeMap,
    sync::{Arc, RwLock},
};

use crate::model::{BrandSummaryItem, ReportPoint, ReportWithAnalysis};
use cleanapp_rustlib::rabbitmq::subscriber::{permanent, Callback, Message};

pub struct InMemoryReports {
    physical_content: Arc<RwLock<BTreeMap<i64, ReportPoint>>>,
    digital_content: Arc<RwLock<BTreeMap<String, BrandSummaryItem>>>,
}

impl InMemoryReports {
    pub async fn new() -> Self {
        Self {
            physical_content: Arc::new(RwLock::new(BTreeMap::new())),
            digital_content: Arc::new(RwLock::new(BTreeMap::new())),
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

        // Parse inline: ack/nack decision depends on *this* returning success/failure.
        let res = serde_json::from_slice::<ReportWithAnalysis>(&message.body)
            .map_err(|e| permanent(e))?;

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
                let mut physical_lock = self
                    .physical_content
                    .write()
                    .unwrap_or_else(|e| panic!("Failed to acquire lock on physical_content: {}", e));
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
