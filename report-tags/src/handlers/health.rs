use crate::models::HealthResponse;
use axum::{http::StatusCode, response::Json};

pub async fn health_check() -> (StatusCode, Json<HealthResponse>) {
    let response = HealthResponse {
        status: "healthy".to_string(),
        service: "report-tags".to_string(),
    };

    (StatusCode::OK, Json(response))
}
