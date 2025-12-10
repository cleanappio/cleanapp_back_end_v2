use mysql_async::prelude::*;
use mysql_async::Pool;

pub async fn ensure_bluesky_tables(pool: &Pool) -> anyhow::Result<()> {
    let mut conn = pool.get_conn().await?;

    // Cursor state for search queries
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_bluesky_cursor (
            query_tag VARCHAR(255) NOT NULL PRIMARY KEY,
            cursor_value VARCHAR(512) NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Raw posts
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_bluesky_post (
            uri VARCHAR(512) NOT NULL PRIMARY KEY,
            cid VARCHAR(128) NOT NULL,
            author_did VARCHAR(128) NOT NULL,
            author_handle VARCHAR(255) NOT NULL,
            text TEXT,
            created_at DATETIME,
            indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            lang VARCHAR(10) DEFAULT '',
            raw JSON,
            INDEX idx_created_at (created_at),
            INDEX idx_author_handle (author_handle)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Media/images linked to posts
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_bluesky_media (
            id INT AUTO_INCREMENT PRIMARY KEY,
            post_uri VARCHAR(512) NOT NULL,
            position INT NOT NULL DEFAULT 0,
            sha256 BINARY(32),
            url VARCHAR(1024),
            UNIQUE KEY uq_post_position (post_uri, position),
            INDEX idx_sha256 (sha256)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Analysis results
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_bluesky_analysis (
            uri VARCHAR(512) NOT NULL PRIMARY KEY,
            is_relevant BOOL DEFAULT FALSE,
            relevance FLOAT DEFAULT 0.0,
            classification ENUM('physical','digital','unknown') DEFAULT 'digital',
            litter_probability FLOAT DEFAULT 0.0,
            hazard_probability FLOAT DEFAULT 0.0,
            digital_bug_probability FLOAT DEFAULT 0.0,
            severity_level FLOAT DEFAULT 0.0,
            latitude DOUBLE NULL,
            longitude DOUBLE NULL,
            report_title VARCHAR(512) DEFAULT '',
            report_description TEXT,
            brand_name VARCHAR(255) DEFAULT '',
            brand_display_name VARCHAR(255) DEFAULT '',
            summary TEXT,
            language VARCHAR(10) DEFAULT 'en',
            inferred_contact_emails JSON,
            raw_llm JSON,
            error VARCHAR(255) NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Submission state tracking
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_bluesky_submit_state (
            id INT NOT NULL PRIMARY KEY DEFAULT 1,
            last_submitted_created_at DATETIME NULL,
            last_submitted_uri VARCHAR(512) NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Initialize submit state if not exists
    conn.query_drop(r#"
        INSERT IGNORE INTO indexer_bluesky_submit_state (id) VALUES (1)
    "#).await?;

    Ok(())
}
