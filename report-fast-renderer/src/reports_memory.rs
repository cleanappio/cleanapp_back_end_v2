use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
};

use crate::model::{BrandSummaryItem, ReportPoint, ReportWithAnalysis};
use cleanapp_rustlib::rabbitmq::subscriber::{Callback, Message};

pub struct InMemoryReports {
    physical_content: Arc<Mutex<BTreeMap<i64, ReportPoint>>>,
    digital_content: Arc<Mutex<BTreeMap<String, BrandSummaryItem>>>,
}

impl InMemoryReports {
    pub async fn new() -> Self {
        Self {
            physical_content: Arc::new(Mutex::new(BTreeMap::new())),
            digital_content: Arc::new(Mutex::new(BTreeMap::new())),
        }
    }
    pub fn get_digital_content(&self) -> Arc<Mutex<BTreeMap<String, BrandSummaryItem>>> {
        self.digital_content.clone()
    }
    pub fn get_physical_content(&self) -> Arc<Mutex<BTreeMap<i64, ReportPoint>>> {
        self.physical_content.clone()
    }
}

impl Callback for InMemoryReports {
    fn on_message(&self, message: &Message) -> Result<(), Box<dyn std::error::Error>> {
        let physical_content = self.physical_content.clone();
        let digital_content = self.digital_content.clone();
        // Clone body for use inside async block
        let body_bytes = message.body.clone();
        tokio::spawn(async move {
            // Parse the incoming message body into ReportWithAnalysis
            let res = serde_json::from_slice::<ReportWithAnalysis>(&body_bytes);
            if res.is_err() {
                tracing::error!(
                    "Failed to parse ReportWithAnalysis from message body: {}",
                    res.err().unwrap()
                );
                return;
            }
            // Successfully parsed; additional handling/storage will follow
            tracing::debug!("Parsed ReportWithAnalysis message successfully");
            let res = res.ok().unwrap();
            let report = &res.report;
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
                    let mut physical_lock = physical_content.lock().unwrap_or_else(|e| {
                        panic!("Failed to acquire lock on physical_content: {}", e);
                    });
                    physical_lock.insert(
                        report.seq,
                        ReportPoint {
                            severity_level: severity_level,
                            seq: report.seq,
                            latitude: report.latitude,
                            longitude: report.longitude,
                        },
                    );
                }
                "digital" => {
                    let mut digital_lock = digital_content.lock().unwrap_or_else(|e| {
                        panic!("Failed to acquire lock on digital_content: {}", e);
                    });
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
        });
        Ok(())
    }
}
