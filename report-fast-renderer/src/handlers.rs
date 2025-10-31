use std::sync::Arc;

use axum::{
    extract::{Query, State},
    http::StatusCode,
    Json,
};
use serde::Deserialize;

use crate::{
    model::{BrandSummaryItem, ReportPoint},
    reports_memory::InMemoryReports,
};

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
pub struct PointsParams {
    classification: Option<String>,
}

/// GET /api/v4/reports/points
#[utoipa::path(
    get,
    path = "/api/v4/reports/points",
    params(PointsParams),
    responses((status = 200, description = "Report points", body = [ReportPoint]))
)]
pub async fn get_report_points(
    State(reports_memory): State<Arc<InMemoryReports>>,
    Query(params): Query<PointsParams>,
) -> Result<Json<Vec<ReportPoint>>, (StatusCode, String)> {
    if params.classification.as_deref() == Some("digital") {
        return Err((
            StatusCode::BAD_REQUEST,
            "Digital classification not supported".to_string(),
        ));
    }

    let physical_map = reports_memory.get_physical_content();
    let items: Vec<ReportPoint> = {
        let guard = physical_map.read().map_err(|_| {
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                "Failed to access reports memory".to_string(),
            )
        })?;
        guard.values().cloned().collect()
    };

    Ok(Json(items))
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
pub struct BrandSummaryParams {
    classification: String,
    lang: String,
}

/// GET /api/v4/brands/summary
#[utoipa::path(
    get,
    path = "/api/v4/brands/summary",
    params(BrandSummaryParams),
    responses(
        (status = 200, description = "Brand counts", body = [BrandSummaryItem])
    )
)]
pub async fn get_brands_summary(
    State(reports_memory): State<Arc<InMemoryReports>>,
    Query(params): Query<BrandSummaryParams>,
) -> Result<Json<Vec<BrandSummaryItem>>, (StatusCode, String)> {
    if params.classification == "physical" {
        return Err((
            StatusCode::BAD_REQUEST,
            "Physical classification not supported".to_string(),
        ));
    }
    if params.lang.is_empty() {
        return Err((
            StatusCode::BAD_REQUEST,
            "Language parameter is required".to_string(),
        ));
    }

    let digital_map = reports_memory.get_digital_content();
    let items: Vec<BrandSummaryItem> = {
        let guard = digital_map.read().map_err(|_| {
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                "Failed to access reports memory".to_string(),
            )
        })?;
        guard.values().cloned().collect()
    };

    Ok(Json(items))
}

pub async fn get_stats_info(
    State(reports_memory): State<Arc<InMemoryReports>>,
) -> Result<Json<serde_json::Value>, (StatusCode, String)> {
    let physical_map = reports_memory.get_physical_content();
    let digital_map = reports_memory.get_digital_content();
    let stats = serde_json::json!({
        "total_physical_reports": {
            "count": physical_map.read().map_err(|_| (StatusCode::INTERNAL_SERVER_ERROR, "Failed to access reports memory".to_string()))?.len(),
        },
        "total_digital_bouquets": {
            "count": digital_map.read().map_err(|_| (StatusCode::INTERNAL_SERVER_ERROR, "Failed to access reports memory".to_string()))?.len(),
        },
    });
    Ok(Json(stats))
}
