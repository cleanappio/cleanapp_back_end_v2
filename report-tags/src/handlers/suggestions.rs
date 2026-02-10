use crate::app_state::AppState;
use crate::models::SuggestionsResponse;
use crate::services::tag_service;
use axum::{
    extract::{Query, State},
    http::StatusCode,
    response::Json,
};
use log;
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct SuggestionQuery {
    pub q: String,
    pub limit: Option<u32>,
}

pub async fn get_tag_suggestions(
    State(state): State<AppState>,
    Query(params): Query<SuggestionQuery>,
) -> Result<Json<SuggestionsResponse>, (StatusCode, String)> {
    let limit = params.limit.unwrap_or(10).min(50); // Cap at 50

    match tag_service::get_tag_suggestions(&state.pool, &params.q, limit).await {
        Ok(suggestions) => {
            let response = SuggestionsResponse { suggestions };
            Ok(Json(response))
        }
        Err(e) => {
            log::error!(
                "Failed to get tag suggestions for query '{}': {}",
                params.q,
                e
            );
            Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
        }
    }
}

pub async fn get_trending_tags(
    State(state): State<AppState>,
    Query(params): Query<TrendingQuery>,
) -> Result<Json<crate::models::TrendingResponse>, (StatusCode, String)> {
    let limit = params.limit.unwrap_or(20).min(100); // Cap at 100

    match tag_service::get_trending_tags(&state.pool, limit).await {
        Ok(trending) => {
            let response = crate::models::TrendingResponse { trending };
            Ok(Json(response))
        }
        Err(e) => {
            log::error!("Failed to get trending tags: {}", e);
            Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
        }
    }
}

#[derive(Debug, Deserialize)]
pub struct TrendingQuery {
    pub limit: Option<u32>,
}
