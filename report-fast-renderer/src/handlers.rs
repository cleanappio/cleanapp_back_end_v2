use std::sync::Arc;

use axum::{
    extract::{Path, Query, State},
    http::{header, StatusCode},
    response::{IntoResponse, Response},
    Json,
};
use serde::Deserialize;

use crate::{
    model::{BrandSummaryItem, ReportPoint},
    reports_memory::InMemoryReports,
    tiles::{self, Hotspot},
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

/// GET /api/v4/reports/tiles/{z}/{x}/{y}
///
/// One map tile of physical reports as a Mapbox Vector Tile; see `tiles.rs`.
/// 204 means the tile has no reports.
#[utoipa::path(
    get,
    path = "/api/v4/reports/tiles/{z}/{x}/{y}",
    params(
        ("z" = u8, Path, description = "Zoom level, 0 to 22"),
        ("x" = u32, Path, description = "Tile column"),
        ("y" = u32, Path, description = "Tile row"),
    ),
    responses(
        (status = 200, description = "Vector tile", content_type = "application/vnd.mapbox-vector-tile"),
        (status = 204, description = "No reports in this tile"),
        (status = 400, description = "Tile coordinates out of range"),
    )
)]
pub async fn get_report_tile(
    State(reports_memory): State<Arc<InMemoryReports>>,
    Path((z, x, y)): Path<(u8, u32, u32)>,
) -> Result<Response, (StatusCode, String)> {
    if z > tiles::MAX_ZOOM || x >= 1 << z || y >= 1 << z {
        return Err((
            StatusCode::BAD_REQUEST,
            "Tile coordinates out of range".to_string(),
        ));
    }

    let physical_map = reports_memory.get_physical_content();
    let tile = {
        let guard = physical_map.read().map_err(|_| {
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                "Failed to access reports memory".to_string(),
            )
        })?;
        tiles::build_tile(guard.values(), z, x, y)
    };

    // New reports arrive continuously, so let caches hold a tile only briefly.
    let cache = (header::CACHE_CONTROL, "public, max-age=30");
    Ok(match tile {
        Some(bytes) => (
            [
                (header::CONTENT_TYPE, "application/vnd.mapbox-vector-tile"),
                cache,
            ],
            bytes,
        )
            .into_response(),
        None => (StatusCode::NO_CONTENT, [cache]).into_response(),
    })
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
pub struct HotspotParams {
    /// How many places to return; 60 by default, 500 at most.
    limit: Option<usize>,
}

/// GET /api/v4/reports/hotspots
///
/// The places with the most physical reports, busiest first.
#[utoipa::path(
    get,
    path = "/api/v4/reports/hotspots",
    params(HotspotParams),
    responses((status = 200, description = "Busiest places", body = [Hotspot]))
)]
pub async fn get_report_hotspots(
    State(reports_memory): State<Arc<InMemoryReports>>,
    Query(params): Query<HotspotParams>,
) -> Result<Json<Vec<Hotspot>>, (StatusCode, String)> {
    let limit = params.limit.unwrap_or(60).min(500);
    let physical_map = reports_memory.get_physical_content();
    let guard = physical_map.read().map_err(|_| {
        (
            StatusCode::INTERNAL_SERVER_ERROR,
            "Failed to access reports memory".to_string(),
        )
    })?;
    Ok(Json(tiles::hotspots(guard.values(), limit)))
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
        "physical_map_refresh": reports_memory.refresh_status.read().map_err(|_| (StatusCode::INTERNAL_SERVER_ERROR, "Failed to access refresh status".to_string()))?.clone(),
        "total_physical_reports": {
            "count": physical_map.read().map_err(|_| (StatusCode::INTERNAL_SERVER_ERROR, "Failed to access reports memory".to_string()))?.len(),
        },
        "total_digital_bouquets": {
            "count": digital_map.read().map_err(|_| (StatusCode::INTERNAL_SERVER_ERROR, "Failed to access reports memory".to_string()))?.len(),
        },
    });
    Ok(Json(stats))
}
