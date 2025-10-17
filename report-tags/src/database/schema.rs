use sqlx::{MySql, Pool};
use anyhow::Result;
use tracing::info;

pub async fn initialize_schema(pool: &Pool<MySql>) -> Result<()> {
    info!("Initializing database schema...");

    // Create tags table
    sqlx::query(
        r#"
        CREATE TABLE IF NOT EXISTS tags (
            id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
            canonical_name VARCHAR(255) NOT NULL UNIQUE,
            display_name VARCHAR(255) NOT NULL,
            usage_count INT UNSIGNED DEFAULT 0,
            last_used_at TIMESTAMP NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_canonical_name (canonical_name),
            INDEX idx_usage_count (usage_count DESC),
            INDEX idx_last_used (last_used_at)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
        "#
    )
    .execute(pool)
    .await?;

    // Create report_tags table
    sqlx::query(
        r#"
        CREATE TABLE IF NOT EXISTS report_tags (
            report_seq INT NOT NULL,
            tag_id INT UNSIGNED NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (report_seq, tag_id),
            INDEX idx_tag_id (tag_id),
            INDEX idx_report_seq (report_seq),
            FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
        ) ENGINE=InnoDB
        "#
    )
    .execute(pool)
    .await?;

    // Create user_tag_follows table
    sqlx::query(
        r#"
        CREATE TABLE IF NOT EXISTS user_tag_follows (
            user_id VARCHAR(256) NOT NULL,
            tag_id INT UNSIGNED NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (user_id, tag_id),
            INDEX idx_user_id (user_id),
            INDEX idx_tag_id (tag_id),
            FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
        ) ENGINE=InnoDB
        "#
    )
    .execute(pool)
    .await?;

    info!("Database schema initialized successfully");
    Ok(())
}
