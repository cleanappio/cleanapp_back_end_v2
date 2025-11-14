use axum::{
    extract::{Query, State, Request},
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
    pub limit: Option<u64>,
}

pub async fn get_tag_feed(
    State(state): State<AppState>,
    request: Request,
) -> Result<Json<TagFeedResponse>, (StatusCode, String)> {
    // Extract query string manually to handle repeated 'tags' parameters
    let query_string = request.uri().query().unwrap_or("");
    
    // Parse query string manually - handles both ?tags=a&tags=b and ?tags=a,b
    let mut tags = Vec::new();
    let mut limit = None;
    
    for pair in query_string.split('&') {
        if let Some((key, value)) = pair.split_once('=') {
            // Simple URL decoding - replace %20 with space, %2C with comma, etc.
            let decoded_key = key.replace("%20", " ").replace("+", " ");
            let decoded_value = value.replace("%20", " ").replace("+", " ").replace("%2C", ",");
            
            if decoded_key == "tags" {
                // Handle comma-separated values in a single tags parameter
                for tag in decoded_value.split(',').map(|s| s.trim()).filter(|s| !s.is_empty()) {
                    tags.push(tag.to_string());
                }
            } else if decoded_key == "limit" {
                if let Ok(parsed_limit) = decoded_value.parse::<u64>() {
                    limit = Some(parsed_limit);
                }
            }
        }
    }
    
    let limit = limit.unwrap_or(20).min(100);
    
    if tags.is_empty() {
        return Err((
            StatusCode::BAD_REQUEST,
            "At least one tag is required".to_string(),
        ));
    }
    
    // Get reports
    let reports = match feed_service::get_tag_feed(
        &state.pool,
        tags,
        limit,
    ).await {
        Ok(reports) => reports,
        Err(e) => {
            log::error!("Failed to get tag feed: {}", e);
            return Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()));
        }
    };
    
    let count = reports.len() as u64;
    
    let response = TagFeedResponse {
        reports,
        count,
    };
    
    Ok(Json(response))
}
