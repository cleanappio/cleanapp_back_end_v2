pub mod schema;

use sqlx::{MySql, Pool};
use anyhow::Result;

pub async fn create_pool(config: &crate::config::Config) -> Result<Pool<MySql>> {
    let database_url = format!(
        "mysql://{}:{}@{}:{}/{}?parseTime=true&multiStatements=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
        config.db_user, config.db_password, config.db_host, config.db_port, config.db_name
    );

    let pool = sqlx::MySqlPool::connect(&database_url).await?;
    
    // Test connection
    sqlx::query("SELECT 1").fetch_one(&pool).await?;
    
    tracing::info!("Database connected successfully to {}:{}/{}", config.db_host, config.db_port, config.db_name);
    
    Ok(pool)
}
