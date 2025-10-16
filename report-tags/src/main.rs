mod config;
mod models;
mod database;
mod services;
mod handlers;
mod utils;

use axum::{
    routing::{get, post, delete},
    Router,
};
use sqlx::MySqlPool;
use std::net::SocketAddr;
use tokio::signal;
use tower_http::{
    cors::{Any, CorsLayer},
    trace::TraceLayer,
};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Load environment variables
    dotenvy::dotenv().ok();
    
    // Load configuration
    let config = config::Config::load();
    
    // Initialize tracing
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| config.rust_log.clone().into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .init();
    
    tracing::info!("Starting report-tags service on port {}", config.port);
    
    // Create database pool
    let pool = database::create_pool(&config).await?;
    
    // Initialize database schema
    database::schema::initialize_schema(&pool).await?;
    
    // Create router
    let app = create_router(pool);
    
    // Create server
    let addr = SocketAddr::from(([0, 0, 0, 0], config.port));
    let listener = tokio::net::TcpListener::bind(addr).await?;
    
    tracing::info!("Server listening on {}", addr);
    
    // Start server with graceful shutdown
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;
    
    Ok(())
}

fn create_router(pool: MySqlPool) -> Router {
    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);
    
    Router::new()
        .route("/health", get(handlers::health::health_check))
        .route("/api/v3/reports/:report_seq/tags", post(handlers::tags::add_tags_to_report))
        .route("/api/v3/reports/:report_seq/tags", get(handlers::tags::get_report_tags))
        .route("/api/v3/tags/suggest", get(handlers::suggestions::get_tag_suggestions))
        .route("/api/v3/tags/trending", get(handlers::suggestions::get_trending_tags))
        .route("/api/v3/users/:user_id/tags/follow", post(handlers::follows::follow_tag))
        .route("/api/v3/users/:user_id/tags/follow/:tag_id", delete(handlers::follows::unfollow_tag))
        .route("/api/v3/users/:user_id/tags/follows", get(handlers::follows::get_user_follows))
        .route("/api/v3/feed", get(handlers::feed::get_location_feed))
        .layer(cors)
        .layer(TraceLayer::new_for_http())
        .with_state(pool)
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("Failed to install Ctrl+C handler");
    };
    
    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("Failed to install signal handler")
            .recv()
            .await;
    };
    
    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();
    
    tokio::select! {
        _ = ctrl_c => {
            tracing::info!("Received Ctrl+C, shutting down gracefully...");
        },
        _ = terminate => {
            tracing::info!("Received terminate signal, shutting down gracefully...");
        },
    }
}