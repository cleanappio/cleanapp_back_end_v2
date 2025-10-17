use axum::{
    extract::{Path, State},
    response::Json,
    http::StatusCode,
};
use sqlx::MySqlPool;
use crate::models::{FollowTagRequest, FollowTagResponse, UnfollowTagResponse, GetFollowsResponse};
use crate::services::tag_service;
use crate::utils::normalization::normalize_tag;
use log;

pub async fn follow_tag(
    State(pool): State<MySqlPool>,
    Path(user_id): Path<String>,
    Json(request): Json<FollowTagRequest>,
) -> Result<Json<FollowTagResponse>, (StatusCode, String)> {
    // Normalize the tag
    let (canonical, _) = normalize_tag(&request.tag)
        .map_err(|e| (StatusCode::BAD_REQUEST, e.to_string()))?;
    
    match tag_service::follow_tag(&pool, &user_id, &canonical, 200).await {
        Ok(tag_id) => {
            let response = FollowTagResponse {
                followed: true,
                tag_id,
            };
            Ok(Json(response))
        }
        Err(e) => {
            if e.to_string().contains("Follow limit exceeded") {
                Err((StatusCode::TOO_MANY_REQUESTS, e.to_string()))
            } else {
                log::error!("Failed to follow tag '{}' for user '{}': {}", request.tag, user_id, e);
                Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
            }
        }
    }
}

pub async fn unfollow_tag(
    State(pool): State<MySqlPool>,
    Path((user_id, tag_id)): Path<(String, u64)>,
) -> Result<Json<UnfollowTagResponse>, (StatusCode, String)> {
    match tag_service::unfollow_tag(&pool, &user_id, tag_id).await {
        Ok(unfollowed) => {
            let response = UnfollowTagResponse { unfollowed };
            Ok(Json(response))
        }
        Err(e) => {
            log::error!("Failed to unfollow tag {} for user '{}': {}", tag_id, user_id, e);
            Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
        }
    }
}

pub async fn get_user_follows(
    State(pool): State<MySqlPool>,
    Path(user_id): Path<String>,
) -> Result<Json<GetFollowsResponse>, (StatusCode, String)> {
    match tag_service::get_user_follows(&pool, &user_id).await {
        Ok(follows) => {
            let response = GetFollowsResponse { follows };
            Ok(Json(response))
        }
        Err(e) => {
            log::error!("Failed to get follows for user '{}': {}", user_id, e);
            Err((StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))
        }
    }
}
