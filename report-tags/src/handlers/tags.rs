use axum::{
    extract::{Path, State},
    response::Json,
    http::StatusCode,
};
use sqlx::MySqlPool;
use crate::models::{AddTagsRequest, AddTagsResponse, GetTagsResponse};
use crate::services::tag_service;
use log;

pub async fn add_tags_to_report(
    State(pool): State<MySqlPool>,
    Path(report_seq): Path<i32>,
    Json(request): Json<AddTagsRequest>,
) -> Result<Json<AddTagsResponse>, (StatusCode, String)> {
    match tag_service::add_tags_to_report(&pool, report_seq, request.tags).await {
        Ok(tags_added) => {
            let response = AddTagsResponse {
                report_seq,
                tags_added,
            };
            Ok(Json(response))
        }
        Err(e) => {
            log::error!("Failed to add tags to report {}: {}", report_seq, e);
            Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
        }
    }
}

pub async fn get_report_tags(
    State(pool): State<MySqlPool>,
    Path(report_seq): Path<i32>,
) -> Result<Json<GetTagsResponse>, (StatusCode, String)> {
    match tag_service::get_tags_for_report(&pool, report_seq).await {
        Ok(tags) => {
            let response = GetTagsResponse { tags };
            Ok(Json(response))
        }
        Err(e) => {
            log::error!("Failed to get tags for report {}: {}", report_seq, e);
            Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
        }
    }
}
