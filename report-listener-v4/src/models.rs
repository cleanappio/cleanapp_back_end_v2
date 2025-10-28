use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize, utoipa::ToSchema)]
pub struct BrandSummaryItem {
    pub brand_name: String,
    pub brand_display_name: String,
    pub total: u64,
}

#[derive(Debug, Serialize, Deserialize, utoipa::ToSchema)]
pub struct Report {
    pub seq: i64,
    pub timestamp: String,
    pub id: String,
    pub latitude: f64,
    pub longitude: f64,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub image: Vec<u8>,
}

#[derive(Debug, Serialize, Deserialize, utoipa::ToSchema, Clone)]
pub struct ReportAnalysis {
    pub seq: i64,
    pub source: String,
    pub analysis_text: String,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub analysis_image: Vec<u8>,
    pub title: String,
    pub description: String,
    pub brand_name: String,
    pub brand_display_name: String,
    pub litter_probability: f64,
    pub hazard_probability: f64,
    pub digital_bug_probability: f64,
    pub severity_level: f64,
    pub summary: String,
    pub language: String,
    pub classification: String,
    pub created_at: String,
}

#[derive(Debug, Serialize, Deserialize, utoipa::ToSchema)]
pub struct ReportWithAnalysis {
    pub report: Report,
    pub analysis: Vec<ReportAnalysis>,
}

#[derive(Debug, Serialize, Deserialize, utoipa::ToSchema)]
pub struct ReportBatch {
    pub reports: Vec<ReportWithAnalysis>,
    pub count: usize,
    pub from_seq: i64,
    pub to_seq: i64,
}



