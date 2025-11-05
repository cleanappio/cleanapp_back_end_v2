pub mod schema;

use sqlx::{MySql, Pool, pool::PoolOptions};
use anyhow::{Result, Context};
use log;
use std::time::Duration;

const MAX_RETRIES: u32 = 10;
const INITIAL_RETRY_DELAY_SECS: u64 = 2;
const MAX_RETRY_DELAY_SECS: u64 = 30;

pub async fn create_pool(config: &crate::config::Config) -> Result<Pool<MySql>> {
    log::info!("Building database connection string...");
    let database_url = format!(
        "mysql://{}:{}@{}:{}/{}?parseTime=true&multiStatements=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
        config.db_user, config.db_password, config.db_host, config.db_port, config.db_name
    );
    log::info!("Database URL constructed (password hidden): mysql://{}:***@{}:{}/{}", 
               config.db_user, config.db_host, config.db_port, config.db_name);

    log::info!("Attempting to connect to MySQL database with retries...");

    let mut last_error = None;
    
    for attempt in 1..=MAX_RETRIES {
        log::info!("Connection attempt {} of {}", attempt, MAX_RETRIES);
        
        // Configure pool options with longer timeouts (create new instance for each retry)
        let pool_options = PoolOptions::<MySql>::new()
            .max_connections(10)
            .acquire_timeout(Duration::from_secs(30))
            .idle_timeout(Duration::from_secs(600))
            .max_lifetime(Duration::from_secs(1800));
        
        match pool_options.connect(&database_url).await {
            Ok(pool) => {
                log::info!("MySQL connection pool established on attempt {}", attempt);
                
                // Test connection with retries
                match test_connection_with_retries(&pool, 3).await {
                    Ok(_) => {
                        log::info!("Database connection test successful");
                        log::info!("Database connected successfully to {}:{}/{}", config.db_host, config.db_port, config.db_name);
                        return Ok(pool);
                    }
                    Err(e) => {
                        log::warn!("Connection pool created but test query failed: {}. Retrying...", e);
                        last_error = Some(e);
                    }
                }
            }
            Err(e) => {
                log::warn!("Connection attempt {} failed: {}", attempt, e);
                last_error = Some(anyhow::anyhow!("{}", e));
            }
        }
        
        if attempt < MAX_RETRIES {
            // Exponential backoff: 2s, 4s, 8s, 16s, 30s (capped), 30s, ...
            let delay_secs = std::cmp::min(
                INITIAL_RETRY_DELAY_SECS * (1u64 << (attempt - 1)),
                MAX_RETRY_DELAY_SECS
            );
            log::info!("Waiting {} seconds before next attempt...", delay_secs);
            tokio::time::sleep(Duration::from_secs(delay_secs)).await;
        }
    }
    
    Err(last_error.unwrap_or_else(|| anyhow::anyhow!("Failed to connect after {} attempts", MAX_RETRIES)))
        .context("Failed to establish database connection after all retries")
}

async fn test_connection_with_retries(pool: &Pool<MySql>, max_retries: u32) -> Result<()> {
    for attempt in 1..=max_retries {
        log::info!("Testing database connection (test attempt {} of {})...", attempt, max_retries);
        match sqlx::query("SELECT 1").fetch_one(pool).await {
            Ok(_) => {
                log::info!("Database connection test successful");
                return Ok(());
            }
            Err(e) => {
                if attempt < max_retries {
                    log::warn!("Test query failed on attempt {}: {}. Retrying...", attempt, e);
                    tokio::time::sleep(Duration::from_secs(1)).await;
                } else {
                    return Err(e).context("Test query failed after all retries");
                }
            }
        }
    }
    Err(anyhow::anyhow!("Test query failed after {} attempts", max_retries))
}
