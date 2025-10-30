use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, utoipa::ToSchema)]
pub struct Report {
    #[serde(rename = "seq")]
    pub seq: i64,
    #[serde(rename = "timestamp")]
    pub timestamp: DateTime<Utc>,
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "team")]
    pub team: i32,
    #[serde(rename = "latitude")]
    pub latitude: f64,
    #[serde(rename = "longitude")]
    pub longitude: f64,
    #[serde(rename = "x")]
    pub x: f64,
    #[serde(rename = "y")]
    pub y: f64,
    #[serde(rename = "image", skip_serializing_if = "Option::is_none")]
    pub image: Option<Vec<u8>>,
    #[serde(rename = "action_id")]
    pub action_id: String,
    #[serde(rename = "description")]
    pub description: String,
}

/// ReportAnalysis represents an analysis result
#[derive(Debug, Clone, Default, Serialize, Deserialize, utoipa::ToSchema)]
pub struct ReportAnalysis {
    #[serde(rename = "seq")]
    pub seq: i64,
    #[serde(rename = "source")]
    pub source: String,
    #[serde(rename = "analysis_text")]
    pub analysis_text: String,
    #[serde(rename = "analysis_image", skip_serializing_if = "Option::is_none")]
    pub analysis_image: Option<Vec<u8>>,
    #[serde(rename = "title")]
    pub title: String,
    #[serde(rename = "description")]
    pub description: String,
    #[serde(rename = "brand_name")]
    pub brand_name: String,
    #[serde(rename = "brand_display_name")]
    pub brand_display_name: String,
    #[serde(rename = "litter_probability")]
    pub litter_probability: f64,
    #[serde(rename = "hazard_probability")]
    pub hazard_probability: f64,
    #[serde(rename = "digital_bug_probability")]
    pub digital_bug_probability: f64,
    #[serde(rename = "severity_level")]
    pub severity_level: f64,
    #[serde(rename = "summary")]
    pub summary: String,
    #[serde(rename = "language")]
    pub language: String,
    #[serde(rename = "classification")]
    pub classification: String,
    #[serde(rename = "is_valid")]
    pub is_valid: bool,
    #[serde(rename = "inferred_contact_emails")]
    pub inferred_contact_emails: String,
    #[serde(rename = "created_at")]
    pub created_at: DateTime<Utc>,
    #[serde(rename = "updated_at")]
    pub updated_at: DateTime<Utc>,
}

/// ReportWithAnalysis represents a report with its corresponding analysis
#[derive(Debug, Clone, Serialize, Deserialize, utoipa::ToSchema)]
pub struct ReportWithAnalysis {
    #[serde(rename = "report")]
    pub report: Report,
    #[serde(rename = "analysis")]
    pub analysis: Vec<ReportAnalysis>,
}

/// ReportPoint represents physical report points with severity levels and coordinates
#[derive(Clone, Debug, Serialize, Deserialize, utoipa::ToSchema)]
pub struct ReportPoint {
    pub seq: i64,
    pub severity_level: f64,
    pub latitude: f64,
    pub longitude: f64,
}

/// BrandSummaryItem represents reports grouped by brand
#[derive(Clone, Debug, Serialize, Deserialize, utoipa::ToSchema)]
pub struct BrandSummaryItem {
    pub brand_name: String,
    pub brand_display_name: String,
    pub total: u64,
}
