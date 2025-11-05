use sqlx::{MySql, Pool, Row};
use anyhow::Result;
use crate::models::Tag;
use crate::utils::normalization::normalize_tag;
use crate::rabbitmq::TagEventPublisher;
use std::sync::Arc;
use log;

pub async fn upsert_tag(pool: &Pool<MySql>, canonical: &str, display: &str) -> Result<u64> {
    let result = sqlx::query(
        "INSERT INTO tags (canonical_name, display_name, usage_count, last_used_at) 
         VALUES (?, ?, 0, NULL)
         ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)"
    )
    .bind(canonical)
    .bind(display)
    .execute(pool)
    .await?;
    
    Ok(result.last_insert_id())
}

pub async fn increment_tag_usage(pool: &Pool<MySql>, tag_id: u64) -> Result<()> {
    sqlx::query(
        "UPDATE tags SET usage_count = usage_count + 1, last_used_at = NOW() WHERE id = ?"
    )
    .bind(tag_id)
    .execute(pool)
    .await?;
    Ok(())
}

pub async fn get_tag_by_canonical(pool: &Pool<MySql>, canonical: &str) -> Result<Option<Tag>> {
    let row = sqlx::query(
        "SELECT id, canonical_name, display_name, usage_count, last_used_at, created_at 
         FROM tags WHERE canonical_name = ?"
    )
    .bind(canonical)
    .fetch_optional(pool)
    .await?;
    
    if let Some(row) = row {
        Ok(Some(Tag {
            id: row.get("id"),
            canonical_name: row.get("canonical_name"),
            display_name: row.get("display_name"),
            usage_count: row.get("usage_count"),
            last_used_at: row.get("last_used_at"),
            created_at: row.get("created_at"),
        }))
    } else {
        Ok(None)
    }
}

pub async fn get_tag_by_id(pool: &Pool<MySql>, tag_id: u64) -> Result<Option<Tag>> {
    let row = sqlx::query(
        "SELECT id, canonical_name, display_name, usage_count, last_used_at, created_at 
         FROM tags WHERE id = ?"
    )
    .bind(tag_id)
    .fetch_optional(pool)
    .await?;
    
    if let Some(row) = row {
        Ok(Some(Tag {
            id: row.get("id"),
            canonical_name: row.get("canonical_name"),
            display_name: row.get("display_name"),
            usage_count: row.get("usage_count"),
            last_used_at: row.get("last_used_at"),
            created_at: row.get("created_at"),
        }))
    } else {
        Ok(None)
    }
}

pub async fn get_tags_for_report(pool: &Pool<MySql>, report_seq: i32) -> Result<Vec<Tag>> {
    let rows = sqlx::query(
        "SELECT t.id, t.canonical_name, t.display_name, t.usage_count, t.last_used_at, t.created_at
         FROM tags t
         INNER JOIN report_tags rt ON t.id = rt.tag_id
         WHERE rt.report_seq = ?
         ORDER BY t.usage_count DESC"
    )
    .bind(report_seq)
    .fetch_all(pool)
    .await?;
    
    let mut tags = Vec::new();
    for row in rows {
        tags.push(Tag {
            id: row.get("id"),
            canonical_name: row.get("canonical_name"),
            display_name: row.get("display_name"),
            usage_count: row.get("usage_count"),
            last_used_at: row.get("last_used_at"),
            created_at: row.get("created_at"),
        });
    }
    
    Ok(tags)
}

pub async fn add_tags_to_report(
    pool: &Pool<MySql>, 
    report_seq: i32, 
    tag_strings: Vec<String>,
    publisher: Option<Arc<TagEventPublisher>>
) -> Result<Vec<String>> {
    let mut added_tags = Vec::new();
    
    for tag_string in tag_strings {
        // Normalize the tag
        let (canonical, display) = normalize_tag(&tag_string)?;
        
        // Upsert the tag
        let tag_id = upsert_tag(pool, &canonical, &display).await?;
        
        // Add to report_tags (ignore if already exists)
        sqlx::query(
            "INSERT IGNORE INTO report_tags (report_seq, tag_id) VALUES (?, ?)"
        )
        .bind(report_seq)
        .bind(tag_id)
        .execute(pool)
        .await?;
        
        // Increment usage count
        increment_tag_usage(pool, tag_id).await?;
        
        added_tags.push(canonical);
    }
    
    // Publish tag added event if publisher is available
    if let Some(pub_) = publisher {
        if let Err(e) = pub_.publish_tag_added(report_seq, added_tags.clone()).await {
            log::error!("Failed to publish tag added event for report {}: {}", report_seq, e);
            // Don't fail the request if publishing fails
        }
    }
    
    Ok(added_tags)
}

pub async fn follow_tag(pool: &Pool<MySql>, user_id: &str, tag_canonical: &str, max_follows: u32) -> Result<u64> {
    // Check follow count
    let count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM user_tag_follows WHERE user_id = ?"
    )
    .bind(user_id)
    .fetch_one(pool)
    .await?;
    
    if count >= max_follows as i64 {
        return Err(anyhow::anyhow!("Follow limit exceeded"));
    }
    
    // Get tag ID
    let tag_id: u64 = sqlx::query_scalar(
        "SELECT id FROM tags WHERE canonical_name = ?"
    )
    .bind(tag_canonical)
    .fetch_one(pool)
    .await?;
    
    // Insert follow (ignore if exists)
    sqlx::query(
        "INSERT IGNORE INTO user_tag_follows (user_id, tag_id) VALUES (?, ?)"
    )
    .bind(user_id)
    .bind(tag_id)
    .execute(pool)
    .await?;
    
    Ok(tag_id)
}

pub async fn unfollow_tag(pool: &Pool<MySql>, user_id: &str, tag_id: u64) -> Result<bool> {
    let result = sqlx::query(
        "DELETE FROM user_tag_follows WHERE user_id = ? AND tag_id = ?"
    )
    .bind(user_id)
    .bind(tag_id)
    .execute(pool)
    .await?;
    
    Ok(result.rows_affected() > 0)
}

pub async fn get_user_follows(pool: &Pool<MySql>, user_id: &str) -> Result<Vec<crate::models::TagWithFollow>> {
    let rows = sqlx::query(
        "SELECT t.id, t.display_name, t.canonical_name, t.usage_count, utf.created_at as followed_at
         FROM tags t
         INNER JOIN user_tag_follows utf ON t.id = utf.tag_id
         WHERE utf.user_id = ?
         ORDER BY t.usage_count DESC"
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    
    let mut follows = Vec::new();
    for row in rows {
        follows.push(crate::models::TagWithFollow {
            id: row.get("id"),
            display_name: row.get("display_name"),
            canonical_name: row.get("canonical_name"),
            usage_count: row.get("usage_count"),
            followed_at: row.get("followed_at"),
        });
    }
    
    Ok(follows)
}

pub async fn get_tag_suggestions(pool: &Pool<MySql>, query: &str, limit: u32) -> Result<Vec<crate::models::TagSuggestion>> {
    let rows = sqlx::query(
        "SELECT id, display_name, canonical_name, usage_count
         FROM tags 
         WHERE canonical_name LIKE ?
         ORDER BY usage_count DESC, last_used_at DESC
         LIMIT ?"
    )
    .bind(format!("{}%", query))
    .bind(limit)
    .fetch_all(pool)
    .await?;
    
    let mut suggestions = Vec::new();
    for row in rows {
        suggestions.push(crate::models::TagSuggestion {
            id: row.get("id"),
            display_name: row.get("display_name"),
            canonical_name: row.get("canonical_name"),
            usage_count: row.get("usage_count"),
        });
    }
    
    Ok(suggestions)
}

pub async fn get_trending_tags(pool: &Pool<MySql>, limit: u32) -> Result<Vec<crate::models::TrendingTag>> {
    let rows = sqlx::query(
        "SELECT id, display_name, usage_count
         FROM tags 
         WHERE usage_count > 0
         ORDER BY usage_count DESC, last_used_at DESC
         LIMIT ?"
    )
    .bind(limit)
    .fetch_all(pool)
    .await?;
    
    let mut trending = Vec::new();
    for row in rows {
        trending.push(crate::models::TrendingTag {
            id: row.get("id"),
            display_name: row.get("display_name"),
            usage_count: row.get("usage_count"),
        });
    }
    
    Ok(trending)
}