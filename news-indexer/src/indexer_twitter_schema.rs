use mysql_async::prelude::*;
use mysql_async::Pool;

pub async fn ensure_twitter_tables(pool: &Pool) -> anyhow::Result<()> {
    let mut conn = pool.get_conn().await?;

    // Cursor state per logical query/tag set
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_twitter_cursor (
            tag VARCHAR(128) NOT NULL PRIMARY KEY,
            since_id BIGINT NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Raw tweets
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_twitter_tweet (
            tweet_id BIGINT NOT NULL,
            created_at DATETIME NULL,
            author_id BIGINT NULL,
            username VARCHAR(64) DEFAULT '',
            lang VARCHAR(8) DEFAULT '',
            text TEXT,
            url VARCHAR(512) DEFAULT '',
            public_metrics JSON NULL,
            entities JSON NULL,
            media_keys JSON NULL,
            raw JSON NULL,
            ingested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (tweet_id),
            INDEX idx_created_at (created_at),
            INDEX idx_username (username),
            INDEX idx_lang (lang)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Media blob store with dedup by sha256
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_media_blob (
            sha256 VARBINARY(32) NOT NULL,
            mime VARCHAR(64) DEFAULT 'image/jpeg',
            width INT NULL,
            height INT NULL,
            data LONGBLOB NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (sha256)
        ) ENGINE=InnoDB
    "#).await?;

    // Mapping tweet -> media
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_twitter_media (
            tweet_id BIGINT NOT NULL,
            media_key VARCHAR(64) NOT NULL,
            position INT NOT NULL,
            type ENUM('photo','video','animated_gif') NOT NULL,
            alt_text TEXT,
            width INT NULL,
            height INT NULL,
            sha256 VARBINARY(32) NULL,
            url VARCHAR(1024) DEFAULT '',
            PRIMARY KEY (tweet_id, position),
            INDEX idx_tweet (tweet_id),
            CONSTRAINT fk_media_blob_sha FOREIGN KEY (sha256) REFERENCES indexer_media_blob(sha256)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // LLM analysis per tweet
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_twitter_analysis (
            tweet_id BIGINT NOT NULL PRIMARY KEY,
            is_relevant BOOL DEFAULT FALSE,
            relevance FLOAT DEFAULT 0.0,
            classification ENUM('physical','digital','unknown') DEFAULT 'unknown',
            litter_probability FLOAT DEFAULT 0.0,
            hazard_probability FLOAT DEFAULT 0.0,
            digital_bug_probability FLOAT DEFAULT 0.0,
            severity_level FLOAT DEFAULT 0.0,
            brand_name VARCHAR(255) DEFAULT '',
            brand_display_name VARCHAR(255) DEFAULT '',
            summary TEXT,
            language VARCHAR(8) DEFAULT 'en',
            inferred_contact_emails JSON NULL,
            raw_llm JSON NULL,
            analyzed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            error TEXT NULL
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Submit state
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_twitter_submit_state (
            id INT PRIMARY KEY DEFAULT 1,
            last_submitted_created_at DATETIME NULL,
            last_submitted_tweet_id BIGINT NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB
    "#).await?;
    conn.query_drop("INSERT IGNORE INTO indexer_twitter_submit_state (id) VALUES (1)").await?;

    Ok(())
}


