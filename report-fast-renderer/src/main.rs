use axum::{
    extract::Path,
    http::StatusCode,
    response::Json,
    routing::{get, post},
    Router,
};
use serde::{Deserialize, Serialize};
use tower::ServiceBuilder;
use tower_http::{
    cors::{Any, CorsLayer},
    trace::TraceLayer,
};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

mod config;
mod subscriber;

use config::{init_config, get_config};
use subscriber::FastRendererSubscriber;

#[derive(Serialize, Deserialize)]
struct HealthResponse {
    status: String,
    service: String,
    version: String,
}

#[derive(Serialize, Deserialize)]
struct RenderRequest {
    content: String,
    format: Option<String>,
}

#[derive(Serialize, Deserialize)]
struct RenderResponse {
    rendered_content: String,
    format: String,
    success: bool,
}

async fn health_check() -> Json<HealthResponse> {
    Json(HealthResponse {
        status: "healthy".to_string(),
        service: "report-fast-renderer".to_string(),
        version: "0.1.0".to_string(),
    })
}

async fn render_report(
    Json(payload): Json<RenderRequest>,
) -> Result<Json<RenderResponse>, StatusCode> {
    // TODO: Implement actual rendering logic
    let format = payload.format.unwrap_or_else(|| "html".to_string());
    
    Ok(Json(RenderResponse {
        rendered_content: format!("Rendered: {}", payload.content),
        format,
        success: true,
    }))
}

async fn get_render_status(Path(id): Path<String>) -> Result<Json<serde_json::Value>, StatusCode> {
    // TODO: Implement status checking logic
    let status = serde_json::json!({
        "id": id,
        "status": "completed",
        "progress": 100
    });
    
    Ok(Json(status))
}

async fn list_formats() -> Json<Vec<String>> {
    Json(vec![
        "html".to_string(),
        "pdf".to_string(),
        "markdown".to_string(),
        "json".to_string(),
    ])
}

async fn get_config_info() -> Json<serde_json::Value> {
    let config = get_config();
    Json(serde_json::json!({
        "amqp_host": config.amqp_host,
        "amqp_port": config.amqp_port,
        "amqp_user": config.amqp_user,
        "exchange": config.exchange,
        "queue_name": config.queue_name,
        "routing_key": config.routing_key,
        "amqp_url": config.amqp_url()
    }))
}

async fn get_subscriber_status() -> Json<serde_json::Value> {
    // Note: In a real implementation, you'd want to store the subscriber
    // in a way that allows checking its status from handlers
    Json(serde_json::json!({
        "status": "initialized",
        "message": "Subscriber status endpoint - implementation needed"
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
    let mut subscriber = FastRendererSubscriber::new().await
        .map_err(|e| anyhow::anyhow!("Failed to initialize subscriber: {}", e))?;
    
    tracing::info!("✅ RabbitMQ subscriber initialized successfully");

    // Start subscriber in background task
    tokio::spawn(async move {
        tracing::info!("🚀 Starting subscriber listener in background...");
        if let Err(e) = subscriber.start_listening().await {
            tracing::error!("Failed to start subscriber: {}", e);
        }
    });

    // Build our application with routes
    let app = Router::new()
        .route("/health", get(health_check))
        .route("/config", get(get_config_info))
        .route("/subscriber/status", get(get_subscriber_status))
        .route("/render", post(render_report))
        .route("/render/:id/status", get(get_render_status))
        .route("/formats", get(list_formats))
        .layer(
            ServiceBuilder::new()
                .layer(TraceLayer::new_for_http())
                .layer(
                    CorsLayer::new()
                        .allow_origin(Any)
                        .allow_methods(Any)
                        .allow_headers(Any),
                ),
        );

    // Run the server
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await?;
    tracing::info!("🚀 Report Fast Renderer server starting on http://0.0.0.0:3000");
    
    axum::serve(listener, app).await?;

    Ok(())
}
