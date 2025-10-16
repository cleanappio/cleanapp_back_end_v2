use axum::{
    extract::{Query, State},
    response::Json,
    http::StatusCode,
};
use sqlx::MySqlPool;
use serde::Deserialize;
use crate::models::FeedResponse;
use crate::services::feed_service;

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
    State(pool): State<MySqlPool>,
    Query(params): Query<FeedQuery>,
) -> Result<Json<FeedResponse>, (StatusCode, String)> {
    let radius = params.radius.unwrap_or(500.0);
    let limit = params.limit.unwrap_or(20).min(100); // Cap at 100
    let offset = params.offset.unwrap_or(0);
    
    // Get total count
    let total = match feed_service::get_feed_count(&pool, params.lat, params.lon, radius, &params.user_id).await {
        Ok(count) => count,
        Err(e) => {
            tracing::error!("Failed to get feed count: {}", e);
            return Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()));
        }
    };
    
    // Get reports
    let reports = match feed_service::get_location_feed(
        &pool, 
        params.lat, 
        params.lon, 
        radius, 
        &params.user_id, 
        limit, 
        offset
    ).await {
        Ok(reports) => reports,
        Err(e) => {
            tracing::error!("Failed to get location feed: {}", e);
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
