pub mod schema;

use sqlx::{MySql, Pool};
use anyhow::Result;
use log;

pub async fn create_pool(config: &crate::config::Config) -> Result<Pool<MySql>> {
    log::info!("Building database connection string...");
    let database_url = format!(
        "mysql://{}:{}@{}:{}/{}?parseTime=true&multiStatements=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
        config.db_user, config.db_password, config.db_host, config.db_port, config.db_name
    );
    log::info!("Database URL constructed (password hidden): mysql://{}:***@{}:{}/{}", 
               config.db_user, config.db_host, config.db_port, config.db_name);

    log::info!("Attempting to connect to MySQL database...");
    let pool = sqlx::MySqlPool::connect(&database_url).await?;
    log::info!("MySQL connection established");
    
    // Test connection
    log::info!("Testing database connection with SELECT 1...");
    sqlx::query("SELECT 1").fetch_one(&pool).await?;
    log::info!("Database connection test successful");
    
    log::info!("Database connected successfully to {}:{}/{}", config.db_host, config.db_port, config.db_name);
    
    Ok(pool)
}
