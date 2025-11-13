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
            conversation_id BIGINT NULL,
            author_id BIGINT NULL,
            username VARCHAR(64) DEFAULT '',
            lang VARCHAR(8) DEFAULT '',
            text TEXT,
            url VARCHAR(512) DEFAULT '',
            public_metrics JSON NULL,
            entities JSON NULL,
            media_keys JSON NULL,
            anchor_tweet_id BIGINT NULL,
            relation ENUM('original','reply','quote','retweet','other') DEFAULT 'original',
            matched_by_filter BOOL DEFAULT FALSE,
            raw JSON NULL,
            ingested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
            PRIMARY KEY (tweet_id),
            INDEX idx_created_at (created_at),
            INDEX idx_conversation (conversation_id),
            INDEX idx_anchor (anchor_tweet_id),
            INDEX idx_username (username),
            INDEX idx_lang (lang)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;

    // Best-effort migration in case table exists without updated_at
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet
            ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            ON UPDATE CURRENT_TIMESTAMP"#).await {
        // ignore if column already exists or lack of privileges
    }
    // Best-effort migrations for new tweet relationship columns
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet ADD COLUMN conversation_id BIGINT NULL"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet ADD COLUMN anchor_tweet_id BIGINT NULL"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet ADD COLUMN relation ENUM('original','reply','quote','retweet','other') DEFAULT 'original'"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet ADD COLUMN matched_by_filter BOOL DEFAULT FALSE"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet ADD INDEX idx_conversation (conversation_id)"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_tweet ADD INDEX idx_anchor (anchor_tweet_id)"#).await {
        // ignore
    }

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
            latitude DOUBLE NULL,
            longitude DOUBLE NULL,
            report_title VARCHAR(512) DEFAULT '',
            report_description TEXT NULL,
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

    // Best-effort migrations for new columns
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_analysis ADD COLUMN latitude DOUBLE NULL"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_analysis ADD COLUMN longitude DOUBLE NULL"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_analysis ADD COLUMN report_title VARCHAR(512) DEFAULT ''"#).await {
        // ignore
    }
    if let Err(_e) = conn.query_drop(
        r#"ALTER TABLE indexer_twitter_analysis ADD COLUMN report_description TEXT NULL"#).await {
        // ignore
    }

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


