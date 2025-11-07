mod config;
mod models;
mod database;
mod services;
mod handlers;
mod utils;
mod rabbitmq;
mod app_state;

use axum::{
    routing::{get, post, delete},
    Router,
};
use std::net::SocketAddr;
// TODO: Re-enable when we have consumers for tag.added events
// use std::sync::Arc;
use tokio::signal;
use tower_http::{
    cors::{Any, CorsLayer},
    trace::TraceLayer,
};
use stderrlog::{self, Timestamp};
use log;
// TODO: Re-enable when we have consumers for tag.added events
// use crate::rabbitmq::TagEventPublisher;
use crate::rabbitmq::ReportTagsSubscriber;
use crate::app_state::AppState;

#[tokio::main]
async fn main() {
    if let Err(e) = run().await {
        eprintln!("FATAL ERROR: {}", e);
        eprintln!("Error details: {:?}", e);
        std::process::exit(1);
    }
}

async fn run() -> anyhow::Result<()> {
    // Initialize stderrlog FIRST - before anything else
    stderrlog::new()
        .verbosity(log::Level::Info)
        .timestamp(Timestamp::Millisecond)
        .show_module_names(true)
        .init()
        .unwrap();
    
    log::info!("=== Report Tags Service Starting ===");
    log::info!("Process ID: {}", std::process::id());
    log::info!("Current working directory: {:?}", std::env::current_dir());
    
    // Load environment variables
    log::info!("Loading environment variables...");
    let env_result = dotenvy::dotenv();
    match env_result {
        Ok(_) => log::info!("Environment variables loaded from .env file"),
        Err(_) => log::info!("No .env file found, using system environment variables"),
    }
    
    // Log key environment variables (without sensitive data)
    log::info!("Environment check:");
    log::info!("  DB_HOST: {}", std::env::var("DB_HOST").unwrap_or_else(|_| "not set".to_string()));
    log::info!("  DB_PORT: {}", std::env::var("DB_PORT").unwrap_or_else(|_| "not set".to_string()));
    log::info!("  DB_NAME: {}", std::env::var("DB_NAME").unwrap_or_else(|_| "not set".to_string()));
    log::info!("  PORT: {}", std::env::var("PORT").unwrap_or_else(|_| "not set".to_string()));
    log::info!("  RUST_LOG: {}", std::env::var("RUST_LOG").unwrap_or_else(|_| "not set".to_string()));
    
    // Load configuration
    log::info!("Loading configuration...");
    let config = config::Config::load();
    log::info!("Configuration loaded successfully");
    log::info!("Database host: {}", config.db_host);
    log::info!("Database port: {}", config.db_port);
    log::info!("Database name: {}", config.db_name);
    log::info!("Server port: {}", config.port);
    
    log::info!("Starting report-tags service on port {}", config.port);
    
    // Create database pool
    log::info!("Creating database connection pool...");
    let pool = database::create_pool(&config).await?;
    log::info!("Database connection pool created successfully");
    
    // Initialize database schema
    log::info!("Initializing database schema...");
    database::schema::initialize_schema(&pool).await?;
    log::info!("Database schema initialized successfully");
    
    // Initialize RabbitMQ subscriber for processing report tags (optional, graceful degradation)
    let report_subscriber = match ReportTagsSubscriber::new(&config).await {
        Ok(sub) => {
            log::info!("RabbitMQ subscriber initialized successfully");
            Some(sub)
        }
        Err(e) => {
            log::warn!("Failed to initialize RabbitMQ subscriber: {}. Continuing without RabbitMQ. Tag processing via HTTP API will still work.", e);
            None
        }
    };
    
    // Start the subscriber if it was initialized (in a background task so it doesn't block HTTP server)
    // Use a separate thread with LocalSet because Callback trait is not Send
    if let Some(mut subscriber) = report_subscriber {
        let pool_clone = pool.clone();
        let routing_key = config.rabbitmq_raw_report_routing_key.clone();
        
        // Spawn a thread with its own LocalSet to run the non-Send subscriber
        std::thread::spawn(move || {
            let rt = tokio::runtime::Runtime::new().unwrap();
            rt.block_on(async {
                let local_set = tokio::task::LocalSet::new();
                local_set.spawn_local(async move {
                    match subscriber.start(pool_clone, &routing_key).await {
                        Ok(_) => {
                            log::info!("RabbitMQ subscriber started successfully for routing key: {}", routing_key);
                        }
                        Err(e) => {
                            log::error!("Failed to start RabbitMQ subscriber: {}. Continuing without RabbitMQ.", e);
                        }
                    }
                });
                local_set.await;
            });
        });
        
        // Note: subscriber is moved into the spawned thread, so we can't use it for shutdown
        // We'll need to handle shutdown differently if needed
    }
    
    // TODO: Re-enable RabbitMQ tag event publisher when we have consumers for tag.added events
    // Initialize RabbitMQ publisher (optional, graceful degradation)
    // let publisher = match TagEventPublisher::new(&config).await {
    //     Ok(pub_) => {
    //         log::info!("RabbitMQ publisher initialized successfully");
    //         Some(Arc::new(pub_))
    //     }
    //     Err(e) => {
    //         log::warn!("Failed to initialize RabbitMQ publisher: {}. Continuing without RabbitMQ.", e);
    //         None
    //     }
    // };
    // 
    // // Clone publisher for shutdown handler before moving into state
    // let shutdown_publisher = publisher.clone();
    
    // Create application state
    let app_state = AppState {
        pool,
        // publisher,
    };
    
    // Create router
    log::info!("Creating HTTP router...");
    let app = create_router(app_state);
    log::info!("HTTP router created successfully");
    
    // Create server
    let addr = SocketAddr::from(([0, 0, 0, 0], config.port));
    log::info!("Binding to address: {}", addr);
    
    // Start server with graceful shutdown
    log::info!("Starting TCP listener...");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    log::info!("TCP listener bound successfully");
    log::info!("Server listening on {}", addr);
    log::info!("=== Report Tags Service Ready ===");
    
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await?;
    
    // Note: Subscriber is running in a background task and will be cleaned up
    // when the process exits. If graceful shutdown of subscriber is needed,
    // we would need to use a channel or other synchronization mechanism.
    log::info!("HTTP server shutdown, background tasks will be cleaned up");
    
    // TODO: Re-enable publisher shutdown when we have consumers for tag.added events
    // Close publisher on shutdown
    // Note: Publisher close consumes self, so we can't close through Arc
    // The connection will be closed when Arc is dropped
    // if shutdown_publisher.is_some() {
    //     log::info!("RabbitMQ publisher will be closed on drop");
    // }
    
    log::info!("Server shutdown complete");
    Ok(())
}

fn create_router(state: AppState) -> Router {
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
        .with_state(state)
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
            log::info!("Received Ctrl+C, shutting down gracefully...");
        },
        _ = terminate => {
            log::info!("Received terminate signal, shutting down gracefully...");
        },
    }
}