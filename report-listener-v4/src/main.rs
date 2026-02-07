use std::net::SocketAddr;

use anyhow::Result;
use axum::{
    extract::Query,
    http::StatusCode,
    response::IntoResponse,
    routing::get,
    Json, Router,
};
use mysql as my;
use serde::Deserialize;
use tower::ServiceBuilder;
use tower_http::{cors::{Any, CorsLayer}, trace::TraceLayer};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

mod cfg;
mod db;
mod models;
mod openapi;

use cfg::Config;
use models::{BrandSummaryItem, ReportBatch, ReportPoint, ReportWithAnalysis};

fn build_version() -> &'static str {
    option_env!("CLEANAPP_BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"))
}

fn git_sha() -> &'static str {
    option_env!("CLEANAPP_GIT_SHA").unwrap_or("")
}

fn build_time() -> &'static str {
    option_env!("CLEANAPP_BUILD_TIME").unwrap_or("")
}

#[tokio::main]
async fn main() {
    if let Err(e) = run().await {
        eprintln!("fatal error: {:#}", e);
        std::process::exit(1);
    }
}

async fn run() -> Result<()> {
    println!("boot: report-listener-v4 starting");
    use std::io::Write as _;
    let _ = std::io::stdout().flush();
    dotenvy::dotenv().ok();
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "report_listener_v4=info,tower_http=info".into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .init();

    tracing::info!("starting report-listener-v4");
    let cfg = Config::from_env()?;
    let pool = db::connect_pool(&cfg)?;

    let app = Router::new()
        .route("/api/v4/health", get(health))
        .route("/api/v4/version", get(version))
        .route("/version", get(version))
        .route("/api/v4/brands/summary", get(get_brands_summary))
        .route("/api/v4/reports/by-brand", get(get_reports_by_brand))
        .route("/api/v4/reports/points", get(get_report_points))
        .route("/api/v4/reports/by-seq", get(get_report_by_seq))
        .merge(openapi::routes())
        .with_state(pool.clone())
        .layer(
            ServiceBuilder::new()
                .layer(TraceLayer::new_for_http())
                .layer(CorsLayer::new().allow_origin(Any).allow_methods(Any).allow_headers(Any)),
        );

    let addr: SocketAddr = format!("0.0.0.0:{}", cfg.http_port).parse().unwrap();
    tracing::info!("report-listener-v4 binding on {}", addr);
    let listener = tokio::net::TcpListener::bind(addr).await?;
    if let Err(e) = axum::serve(listener, app).await {
        eprintln!("server error: {:#}", e);
        std::process::exit(1);
    }
    eprintln!("server exited unexpectedly");
    std::process::exit(2)
}

async fn health() -> impl IntoResponse {
    Json(serde_json::json!({
        "status": "healthy",
        "service": "report-listener-v4",
        "version": build_version(),
        "time": chrono::Utc::now().to_rfc3339(),
    }))
}

async fn version() -> impl IntoResponse {
    Json(serde_json::json!({
        "service": "report-listener-v4",
        "version": build_version(),
        "git_sha": git_sha(),
        "build_time": build_time(),
    }))
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
struct BrandSummaryParams {
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
async fn get_brands_summary(
    axum::extract::State(pool): axum::extract::State<my::Pool>,
    Query(params): Query<BrandSummaryParams>,
) -> Result<Json<Vec<BrandSummaryItem>>, (StatusCode, String)> {
    let items = db::fetch_brand_summaries(&pool, &params.classification, &params.lang)
        .map_err(internal_error)?;
    Ok(Json(items))
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
struct ReportsByBrandParams {
    brand_name: String,
    n: Option<u64>,
}

/// GET /api/v4/reports/by-brand
#[utoipa::path(
    get,
    path = "/api/v4/reports/by-brand",
    params(ReportsByBrandParams),
    responses((status = 200, description = "Reports by brand", body = ReportBatch))
)]
async fn get_reports_by_brand(
    axum::extract::State(pool): axum::extract::State<my::Pool>,
    Query(params): Query<ReportsByBrandParams>,
) -> Result<Json<ReportBatch>, (StatusCode, String)> {
    let limit = params.n.unwrap_or(1000) as usize;
    let batch = db::fetch_reports_by_brand(&pool, &params.brand_name, limit).map_err(internal_error)?;
    Ok(Json(batch))
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
struct PointsParams {
    classification: Option<String>,
}

/// GET /api/v4/reports/points
#[utoipa::path(
    get,
    path = "/api/v4/reports/points",
    params(PointsParams),
    responses((status = 200, description = "Report points", body = [ReportPoint]))
)]
async fn get_report_points(
    axum::extract::State(pool): axum::extract::State<my::Pool>,
    Query(params): Query<PointsParams>,
) -> Result<Json<Vec<ReportPoint>>, (StatusCode, String)> {
    let classification = params.classification.unwrap_or_else(|| "all".to_string());
    let items = db::fetch_report_points(&pool, &classification).map_err(internal_error)?;
    Ok(Json(items))
}

#[derive(Deserialize, utoipa::IntoParams)]
#[into_params(parameter_in = Query)]
struct BySeqParams { seq: i64 }

/// GET /api/v4/reports/by-seq
#[utoipa::path(
    get,
    path = "/api/v4/reports/by-seq",
    params(BySeqParams),
    responses((status = 200, description = "Report by seq", body = ReportWithAnalysis))
)]
async fn get_report_by_seq(
    axum::extract::State(pool): axum::extract::State<my::Pool>,
    Query(params): Query<BySeqParams>,
) -> Result<Json<ReportWithAnalysis>, (StatusCode, String)> {
    let item = db::fetch_report_by_seq(&pool, params.seq).map_err(internal_error)?;
    Ok(Json(item))
}

fn internal_error<E: std::fmt::Display>(e: E) -> (StatusCode, String) {
    tracing::error!("internal error: {}", e);
    (StatusCode::INTERNAL_SERVER_ERROR, e.to_string())
}

