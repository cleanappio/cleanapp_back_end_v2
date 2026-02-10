use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct TagAddedEvent {
    pub report_seq: i32,
    pub tags: Vec<String>,
    pub timestamp: DateTime<Utc>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct ReportMessage {
    pub seq: i32,
    pub id: String,
    // Add other report fields as needed for future auto-tagging
}
