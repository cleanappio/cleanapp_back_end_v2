use sqlx::{MySql, Pool, Row};
use anyhow::Result;
use crate::models::ReportWithTags;

pub async fn get_location_feed(
    pool: &Pool<MySql>,
    lat: f64,
    lon: f64,
    radius_meters: f64,
    user_id: &str,
    limit: u64,
    offset: u64,
) -> Result<Vec<ReportWithTags>> {
    // 1. Get user's followed tag IDs
    let followed_tags: Vec<u64> = sqlx::query_scalar(
        "SELECT tag_id FROM user_tag_follows WHERE user_id = ?"
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    
    if followed_tags.is_empty() {
        return Ok(vec![]);
    }
    
    // 2. Query reports within radius with any of the followed tags
    // Use ST_Distance_Sphere on reports_geometry.geom
    let placeholders = followed_tags.iter().map(|_| "?").collect::<Vec<_>>().join(",");
    let query = format!(
        "SELECT DISTINCT r.seq, r.latitude, r.longitude, r.ts, r.id, r.team 
         FROM reports r
         INNER JOIN reports_geometry rg ON r.seq = rg.seq
         INNER JOIN report_tags rt ON r.seq = rt.report_seq
         WHERE ST_Distance_Sphere(rg.geom, POINT(?, ?)) <= ?
         AND rt.tag_id IN ({})
         ORDER BY r.seq DESC
         LIMIT ? OFFSET ?",
        placeholders
    );
    
    let mut query_builder = sqlx::query(&query);
    query_builder = query_builder.bind(lon).bind(lat).bind(radius_meters);
    for tag_id in &followed_tags {
        query_builder = query_builder.bind(tag_id);
    }
    query_builder = query_builder.bind(limit as i64).bind(offset as i64);
    
    let reports = query_builder
        .fetch_all(pool)
        .await?;
    
    if reports.is_empty() {
        return Ok(vec![]);
    }
    
    // 3. Get report sequences for detailed queries (unused for now)
    let _report_seqs: Vec<i32> = reports.iter().map(|row| row.get("seq")).collect();
    
    // 4. Get tags for each report
    let mut reports_with_tags = Vec::new();
    
    for report in reports {
        let seq: i32 = report.get("seq");
        let id: String = report.get("id");
        let team: i32 = report.get("team");
        let latitude: f64 = report.get("latitude");
        let longitude: f64 = report.get("longitude");
        let ts: chrono::DateTime<chrono::Utc> = report.get("ts");
        
        // Get tags for this report
        let tag_rows = sqlx::query(
            "SELECT t.id, t.canonical_name, t.display_name, t.usage_count, t.last_used_at, t.created_at
             FROM tags t
             INNER JOIN report_tags rt ON t.id = rt.tag_id
             WHERE rt.report_seq = ?"
        )
        .bind(seq)
        .fetch_all(pool)
        .await?;
        
        let mut tags = Vec::new();
        for tag_row in tag_rows {
            tags.push(crate::models::Tag {
                id: tag_row.get("id"),
                canonical_name: tag_row.get("canonical_name"),
                display_name: tag_row.get("display_name"),
                usage_count: tag_row.get("usage_count"),
                last_used_at: tag_row.get("last_used_at"),
                created_at: tag_row.get("created_at"),
            });
        }
        
        // Get analysis for this report
        let analysis_row = sqlx::query(
            "SELECT seq, source, analysis_text, title, description, brand_name, brand_display_name,
                    litter_probability, hazard_probability, digital_bug_probability, severity_level,
                    summary, language, classification, is_valid, created_at, updated_at
             FROM report_analysis 
             WHERE seq = ?"
        )
        .bind(seq)
        .fetch_optional(pool)
        .await?;
        
        let analysis = if let Some(row) = analysis_row {
            Some(crate::models::ReportAnalysis {
                seq: row.get("seq"),
                source: row.get("source"),
                analysis_text: row.get("analysis_text"),
                title: row.get("title"),
                description: row.get("description"),
                brand_name: row.get("brand_name"),
                brand_display_name: row.get("brand_display_name"),
                litter_probability: row.get("litter_probability"),
                hazard_probability: row.get("hazard_probability"),
                digital_bug_probability: row.get("digital_bug_probability"),
                severity_level: row.get("severity_level"),
                summary: row.get("summary"),
                language: row.get("language"),
                classification: row.get("classification"),
                is_valid: row.get("is_valid"),
                created_at: row.get("created_at"),
                updated_at: row.get("updated_at"),
            })
        } else {
            None
        };
        
        reports_with_tags.push(ReportWithTags {
            seq,
            id,
            team,
            latitude,
            longitude,
            ts,
            tags,
            analysis,
        });
    }
    
    Ok(reports_with_tags)
}

pub async fn get_feed_count(
    pool: &Pool<MySql>,
    lat: f64,
    lon: f64,
    radius_meters: f64,
    user_id: &str,
) -> Result<u64> {
    // Get user's followed tag IDs
    let followed_tags: Vec<u64> = sqlx::query_scalar(
        "SELECT tag_id FROM user_tag_follows WHERE user_id = ?"
    )
    .bind(user_id)
    .fetch_all(pool)
    .await?;
    
    if followed_tags.is_empty() {
        return Ok(0);
    }
    
    // Count reports within radius with any of the followed tags
    let placeholders = followed_tags.iter().map(|_| "?").collect::<Vec<_>>().join(",");
    let query = format!(
        "SELECT COUNT(DISTINCT r.seq)
         FROM reports r
         INNER JOIN reports_geometry rg ON r.seq = rg.seq
         INNER JOIN report_tags rt ON r.seq = rt.report_seq
         WHERE ST_Distance_Sphere(rg.geom, POINT(?, ?)) <= ?
         AND rt.tag_id IN ({})",
        placeholders
    );
    
    let mut query_builder = sqlx::query_scalar::<_, i64>(&query);
    query_builder = query_builder.bind(lon).bind(lat).bind(radius_meters);
    for tag_id in &followed_tags {
        query_builder = query_builder.bind(tag_id);
    }
    
    let count = query_builder.fetch_one(pool).await?;
    Ok(count as u64)
}

pub async fn get_tag_feed(
    pool: &Pool<MySql>,
    tag_names: Vec<String>,
    limit: u64,
) -> Result<Vec<ReportWithTags>> {
    if tag_names.is_empty() {
        return Ok(vec![]);
    }
    
    // 1. Look up tag IDs from tag names using canonical_name matching
    let placeholders = tag_names.iter().map(|_| "?").collect::<Vec<_>>().join(",");
    let tag_query = format!(
        "SELECT id FROM tags WHERE canonical_name IN ({})",
        placeholders
    );
    
    let mut tag_query_builder = sqlx::query_scalar::<_, u64>(&tag_query);
    for tag_name in &tag_names {
        tag_query_builder = tag_query_builder.bind(tag_name);
    }
    
    let tag_ids: Vec<u64> = tag_query_builder
        .fetch_all(pool)
        .await?;
    
    if tag_ids.is_empty() {
        return Ok(vec![]);
    }
    
    // 2. Query reports with any of the tag IDs
    let tag_placeholders = tag_ids.iter().map(|_| "?").collect::<Vec<_>>().join(",");
    let query = format!(
        "SELECT DISTINCT r.seq, r.latitude, r.longitude, r.ts, r.id, r.team 
         FROM reports r
         INNER JOIN report_tags rt ON r.seq = rt.report_seq
         WHERE rt.tag_id IN ({})
         ORDER BY r.seq DESC
         LIMIT ?",
        tag_placeholders
    );
    
    let mut query_builder = sqlx::query(&query);
    for tag_id in &tag_ids {
        query_builder = query_builder.bind(tag_id);
    }
    query_builder = query_builder.bind(limit as i64);
    
    let reports = query_builder
        .fetch_all(pool)
        .await?;
    
    if reports.is_empty() {
        return Ok(vec![]);
    }
    
    // 3. Get tags and analysis for each report (reusing logic from get_location_feed)
    let mut reports_with_tags = Vec::new();
    
    for report in reports {
        let seq: i32 = report.get("seq");
        let id: String = report.get("id");
        let team: i32 = report.get("team");
        let latitude: f64 = report.get("latitude");
        let longitude: f64 = report.get("longitude");
        let ts: chrono::DateTime<chrono::Utc> = report.get("ts");
        
        // Get tags for this report
        let tag_rows = sqlx::query(
            "SELECT t.id, t.canonical_name, t.display_name, t.usage_count, t.last_used_at, t.created_at
             FROM tags t
             INNER JOIN report_tags rt ON t.id = rt.tag_id
             WHERE rt.report_seq = ?"
        )
        .bind(seq)
        .fetch_all(pool)
        .await?;
        
        let mut tags = Vec::new();
        for tag_row in tag_rows {
            tags.push(crate::models::Tag {
                id: tag_row.get("id"),
                canonical_name: tag_row.get("canonical_name"),
                display_name: tag_row.get("display_name"),
                usage_count: tag_row.get("usage_count"),
                last_used_at: tag_row.get("last_used_at"),
                created_at: tag_row.get("created_at"),
            });
        }
        
        // Get analysis for this report
        let analysis_row = sqlx::query(
            "SELECT seq, source, analysis_text, title, description, brand_name, brand_display_name,
                    litter_probability, hazard_probability, digital_bug_probability, severity_level,
                    summary, language, classification, is_valid, created_at, updated_at
             FROM report_analysis 
             WHERE seq = ?"
        )
        .bind(seq)
        .fetch_optional(pool)
        .await?;
        
        let analysis = if let Some(row) = analysis_row {
            Some(crate::models::ReportAnalysis {
                seq: row.get("seq"),
                source: row.get("source"),
                analysis_text: row.get("analysis_text"),
                title: row.get("title"),
                description: row.get("description"),
                brand_name: row.get("brand_name"),
                brand_display_name: row.get("brand_display_name"),
                litter_probability: row.get("litter_probability"),
                hazard_probability: row.get("hazard_probability"),
                digital_bug_probability: row.get("digital_bug_probability"),
                severity_level: row.get("severity_level"),
                summary: row.get("summary"),
                language: row.get("language"),
                classification: row.get("classification"),
                is_valid: row.get("is_valid"),
                created_at: row.get("created_at"),
                updated_at: row.get("updated_at"),
            })
        } else {
            None
        };
        
        reports_with_tags.push(ReportWithTags {
            seq,
            id,
            team,
            latitude,
            longitude,
            ts,
            tags,
            analysis,
        });
    }
    
    Ok(reports_with_tags)
}