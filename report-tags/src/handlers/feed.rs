use axum::{
    extract::{Query, State},
    response::Json,
    http::StatusCode,
};
use serde::Deserialize;
use crate::app_state::AppState;
use crate::models::{FeedResponse, TagFeedResponse};
use crate::services::feed_service;
use log;

#[derive(Debug, Deserialize)]
pub struct FeedQuery {
    pub lat: f64,
    pub lon: f64,
    pub radius: Option<f64>,
    pub user_id: String,
    pub limit: Option<u64>,
    pub offset: Option<u64>,
}

pub async fn get_location_feed(
    State(state): State<AppState>,
    Query(params): Query<FeedQuery>,
) -> Result<Json<FeedResponse>, (StatusCode, String)> {
    let radius = params.radius.unwrap_or(500.0);
    let limit = params.limit.unwrap_or(20).min(100); // Cap at 100
    let offset = params.offset.unwrap_or(0);
    
    // Get total count
    let total = match feed_service::get_feed_count(&state.pool, params.lat, params.lon, radius, &params.user_id).await {
        Ok(count) => count,
        Err(e) => {
            log::error!("Failed to get feed count: {}", e);
            return Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()));
        }
    };
    
    // Get reports
    let reports = match feed_service::get_location_feed(
        &state.pool, 
        params.lat, 
        params.lon, 
        radius, 
        &params.user_id, 
        limit, 
        offset
    ).await {
        Ok(reports) => reports,
        Err(e) => {
            log::error!("Failed to get location feed: {}", e);
            return Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()));
        }
    };
    
    let response = FeedResponse {
        reports,
        total,
        limit,
        offset,
    };
    
    Ok(Json(response))
}

#[derive(Debug, Deserialize)]
pub struct TagFeedQuery {
    pub tags: Vec<String>,
    pub limit: Option<u64>,
}

pub async fn get_tag_feed(
    State(state): State<AppState>,
    Query(params): Query<TagFeedQuery>,
) -> Result<Json<TagFeedResponse>, (StatusCode, String)> {
    if params.tags.is_empty() {
        return Err((
            StatusCode::BAD_REQUEST,
            "At least one tag is required".to_string(),
        ));
    }
    
    let limit = params.limit.unwrap_or(20).min(100); // Cap at 100
    
    // Get reports
    let reports = match feed_service::get_tag_feed(
        &state.pool,
        params.tags,
        limit,
    ).await {
        Ok(reports) => reports,
        Err(e) => {
            log::error!("Failed to get tag feed: {}", e);
            return Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()));
        }
    };
    
    let response = TagFeedResponse {
        reports,
    };
    
    Ok(Json(response))
}
