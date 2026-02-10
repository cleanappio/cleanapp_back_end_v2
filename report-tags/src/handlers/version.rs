use axum::{http::StatusCode, response::Json};
use serde::Serialize;

fn build_version() -> &'static str {
    option_env!("CLEANAPP_BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"))
}

fn git_sha() -> &'static str {
    option_env!("CLEANAPP_GIT_SHA").unwrap_or("")
}

fn build_time() -> &'static str {
    option_env!("CLEANAPP_BUILD_TIME").unwrap_or("")
}

#[derive(Debug, Serialize)]
pub struct VersionResponse {
    pub service: String,
    pub version: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub git_sha: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub build_time: String,
}

pub async fn version() -> (StatusCode, Json<VersionResponse>) {
    let response = VersionResponse {
        service: "report-tags".to_string(),
        version: build_version().to_string(),
        git_sha: git_sha().to_string(),
        build_time: build_time().to_string(),
    };

    (StatusCode::OK, Json(response))
}
