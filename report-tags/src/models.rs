use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Tag {
    pub id: u64,
    pub canonical_name: String,
    pub display_name: String,
    pub usage_count: u32,
    pub last_used_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportTag {
    pub report_seq: i32,
    pub tag_id: u64,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UserTagFollow {
    pub user_id: String,
    pub tag_id: u64,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TagWithFollow {
    pub id: u64,
    pub display_name: String,
    pub canonical_name: String,
    pub usage_count: u32,
    pub followed_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportWithTags {
    pub seq: i32,
    pub id: String,
    pub team: i32,
    pub latitude: f64,
    pub longitude: f64,
    pub ts: DateTime<Utc>,
    pub tags: Vec<Tag>,
    pub analysis: Vec<ReportAnalysis>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportAnalysis {
    pub seq: i32,
    pub source: String,
    pub analysis_text: Option<String>,
    pub title: Option<String>,
    pub description: Option<String>,
    pub brand_name: Option<String>,
    pub brand_display_name: Option<String>,
    pub litter_probability: Option<f64>,
    pub hazard_probability: Option<f64>,
    pub digital_bug_probability: Option<f64>,
    pub severity_level: Option<f64>,
    pub summary: Option<String>,
    pub language: Option<String>,
    pub classification: Option<String>,
    pub is_valid: Option<bool>,
    pub created_at: Option<DateTime<Utc>>,
    pub updated_at: Option<DateTime<Utc>>,
}

// Request/Response DTOs
#[derive(Debug, Deserialize)]
pub struct AddTagsRequest {
    pub tags: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct AddTagsResponse {
    pub report_seq: i32,
    pub tags_added: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct GetTagsResponse {
    pub tags: Vec<Tag>,
}

#[derive(Debug, Serialize)]
pub struct TagSuggestion {
    pub id: u64,
    pub display_name: String,
    pub canonical_name: String,
    pub usage_count: u32,
}

#[derive(Debug, Serialize)]
pub struct SuggestionsResponse {
    pub suggestions: Vec<TagSuggestion>,
}

#[derive(Debug, Deserialize)]
pub struct FollowTagRequest {
    pub tag: String,
}

#[derive(Debug, Serialize)]
pub struct FollowTagResponse {
    pub followed: bool,
    pub tag_id: u64,
}

#[derive(Debug, Serialize)]
pub struct UnfollowTagResponse {
    pub unfollowed: bool,
}

#[derive(Debug, Serialize)]
pub struct GetFollowsResponse {
    pub follows: Vec<TagWithFollow>,
}

#[derive(Debug, Serialize)]
pub struct FeedResponse {
    pub reports: Vec<ReportWithTags>,
    pub total: u64,
    pub limit: u64,
    pub offset: u64,
}

#[derive(Debug, Serialize)]
pub struct TrendingTag {
    pub id: u64,
    pub display_name: String,
    pub usage_count: u32,
}

#[derive(Debug, Serialize)]
pub struct TrendingResponse {
    pub trending: Vec<TrendingTag>,
}

#[derive(Debug, Serialize)]
pub struct HealthResponse {
    pub status: String,
    pub service: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Report {
    pub seq: i32,
    #[serde(rename = "timestamp")]
    pub ts: DateTime<Utc>,
    pub id: String,
    pub team: i32,
    pub latitude: f64,
    pub longitude: f64,
    pub x: Option<f64>,
    pub y: Option<f64>,
    pub action_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportWithAnalysis {
    pub report: Report,
    pub analysis: Vec<ReportAnalysis>,
}

#[derive(Debug, Serialize)]
pub struct TagFeedResponse {
    pub reports: Vec<ReportWithAnalysis>,
    pub count: u64,
}
