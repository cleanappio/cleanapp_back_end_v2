use std::sync::Arc;

use axum::{response::Json, routing::get, Router};
use serde::{Deserialize, Serialize};
use tower::ServiceBuilder;
use tower_http::{
    compression::CompressionLayer,
    cors::{Any, CorsLayer},
    trace::TraceLayer,
};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

mod config;
mod db;
mod handlers;
mod model;
mod reports_memory;
mod subscriber;

use config::{get_config, init_config};
use subscriber::FastRendererSubscriber;

use crate::{
    handlers::{get_brands_summary, get_report_points, get_stats_info},
    reports_memory::InMemoryReports,
};

#[derive(Serialize, Deserialize)]
struct HealthResponse {
    status: String,
    service: String,
    version: String,
}

async fn health_check() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "healthy".to_string(),
        service: "report-fast-renderer".to_string(),
        version: "0.1.0".to_string(),
    })
}

async fn get_config_info() -> Json<serde_json::Value> {
    let config = get_config();
    Json(serde_json::json!({
        "db_host": config.db_host,
        "db_port": config.db_port,
        "db_user": config.db_user,
        "db_name": config.db_name,
        "amqp_host": config.amqp_host,
        "amqp_port": config.amqp_port,
        "amqp_user": config.amqp_user,
        "exchange": config.exchange,
        "queue_name": config.queue_name,
        "routing_key": config.routing_key,
    }))
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Initialize tracing
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "report_fast_renderer=debug,tower_http=debug".into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .init();

    // Initialize configuration
    tracing::info!("🔧 Initializing configuration from environment variables...");
    init_config().map_err(|e| anyhow::anyhow!("Failed to initialize config: {}", e))?;

    let config = get_config();
    tracing::info!("✅ Configuration loaded successfully");
    tracing::debug!("AMQP URL: {}", config.amqp_url());
    tracing::debug!("Exchange: {}", config.exchange);
    tracing::debug!("Queue: {}", config.queue_name);
    tracing::debug!("Routing key: {}", config.routing_key);

    // Initialize RabbitMQ subscriber
    tracing::info!("🐰 Initializing RabbitMQ subscriber...");
    let mut subscriber = FastRendererSubscriber::new()
        .await
        .map_err(|e| anyhow::anyhow!("Failed to initialize subscriber: {}", e))?;

    tracing::info!("✅ RabbitMQ subscriber initialized successfully");

    let reports_memory = Arc::new(InMemoryReports::new().await);

    // Load reports into memory from the database
    tracing::info!("📥 Loading reports into in-memory storage...");
    let physical_reports = db::fetch_report_points(&db::connect_pool()?, "physical")?;
    {
        let physical_map = reports_memory.get_physical_content();
        let mut guard = physical_map
            .write()
            .map_err(|e| anyhow::anyhow!("Failed to lock physical reports map: {}", e))?;
        for report in physical_reports {
            guard.insert(report.seq, report);
        }
        tracing::info!("✅ Loaded {} physical reports into memory", guard.len());
    }
    let digital_reports = db::fetch_brand_summaries(&db::connect_pool()?, "digital", "en")?;
    {
        let digital_map = reports_memory.get_digital_content();
        let mut guard = digital_map
            .write()
            .map_err(|e| anyhow::anyhow!("Failed to lock digital reports map: {}", e))?;
        for report in digital_reports {
            guard.insert(report.brand_name.clone(), report);
        }
        tracing::info!("✅ Loaded {} digital reports into memory", guard.len());
    }

    // Start listening to messages
    let reports_memory_for_subscriber = reports_memory.clone();
    subscriber
        .start_listening(reports_memory_for_subscriber)
        .await
        .map_err(|e| tracing::error!("Failed to start subscriber listening: {}", e))
        .ok();

    // Build our application with routes
    let app = Router::new()
        .route("/health", get(health_check))
        .route("/config", get(get_config_info))
        .route("/stats", get(get_stats_info))
        .route("/api/v4/brands/summary", get(get_brands_summary))
        .route("/api/v4/reports/points", get(get_report_points))
        .layer(
            ServiceBuilder::new()
                .layer(TraceLayer::new_for_http())
                .layer(CompressionLayer::new())
                .layer(
                    CorsLayer::new()
                        .allow_origin(Any)
                        .allow_methods(Any)
                        .allow_headers(Any),
                ),
        )
        .with_state(reports_memory.clone());

    // Run the server
    let port = get_config().server_port.clone();
    let listener = tokio::net::TcpListener::bind(format!("0.0.0.0:{}", port)).await?;
    tracing::info!("🚀 Report Fast Renderer server starting on http://0.0.0.0:{}", port);

    axum::serve(listener, app).await?;

    Ok(())
}
