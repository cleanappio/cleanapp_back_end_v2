use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::{Pool, Row};
use serde::Deserialize;
use serde_json::json;
use base64::{engine::general_purpose::STANDARD, Engine as _};
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_twitter_schema.rs"]
mod indexer_twitter_schema;

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: Option<GeneralConfig>,
    submit: Option<SubmitConfig>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig {
    db_url: String,
}

#[derive(Deserialize, Clone, Debug)]
struct SubmitConfig {
    endpoint_url: Option<String>,
    token: Option<String>,
}

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")] config_path: String,
    #[arg(long, env = "DB_URL")] db_url: Option<String>,
    #[arg(long, env = "SUBMIT_ENDPOINT_URL")] endpoint_url: Option<String>,
    #[arg(long, env = "SUBMIT_TOKEN")] token: Option<String>,

    #[arg(long, env = "SUBMIT_BATCH_SIZE", default_value_t = 300)] batch_size: usize,
    #[arg(long, default_value_t = 0)] limit_total: u64,
    #[arg(long)] since_created: Option<String>,
    /// Interval between submit cycles when limit_total = 0 (seconds)
    #[arg(long, env = "SUBMIT_INTERVAL_SECS", default_value_t = 300)] interval_secs: u64,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    // Load config
    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args
        .db_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or config.general.db_url")?;
    let endpoint_url = args
        .endpoint_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.submit.as_ref().and_then(|s| s.endpoint_url.clone())))
        .context("endpoint_url must be provided via --endpoint-url or config.submit.endpoint_url")?;
    let token = args
        .token
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.submit.as_ref().and_then(|s| s.token.clone())))
        .context("token must be provided via --token or config.submit.token")?;

    let batch_size = args.batch_size.min(1000).max(1);
    info!(
        "submitter_twitter: start endpoint={} batch_size={} limit_total={} since_created={:?}",
        endpoint_url, batch_size, args.limit_total, args.since_created
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_twitter_schema::ensure_twitter_tables(&pool).await?;
    let mut conn = pool.get_conn().await?;

    let client = reqwest::Client::builder()
        .timeout(StdDuration::from_secs(60))
        .build()?;

    loop {
        let mut total_sent: u64 = 0;
        let mut total_inserted: u64 = 0;
        let mut total_updated: u64 = 0;
        let mut total_skipped: u64 = 0;
        let mut total_errors: u64 = 0;
        let mut effective_batch_size: usize = batch_size;

        'outer: loop {
            if args.limit_total > 0 && total_sent >= args.limit_total { break; }

        // Determine start anchors from state table
        let (saved_created, saved_tweet_id): (Option<String>, Option<i64>) = {
            let row: Option<(Option<String>, Option<i64>)> = conn
                .exec_first(
                    "SELECT DATE_FORMAT(last_submitted_created_at, '%Y-%m-%d %H:%i:%s'), last_submitted_tweet_id FROM indexer_twitter_submit_state WHERE id=1",
                    (),
                )
                .await?;
            row.unwrap_or((None, None))
        };
        let since_created = if saved_created.is_some() { saved_created.clone() } else { args.since_created.clone() };
        let after_tweet_id = if saved_created.is_some() { saved_tweet_id } else { None };

        // Fetch batch of candidates
        let rows: Vec<Row> = if let Some(ref since) = since_created {
            if let Some(aid) = after_tweet_id {
                info!("selecting tweets with (created_at, tweet_id) > ({}, {}) batch_size={} totals: ins={} upd={} err={}", since, aid, effective_batch_size, total_inserted, total_updated, total_errors);
                        conn.exec(
                            r#"SELECT t.tweet_id,
                               COALESCE(t.username,''),
                               COALESCE(t.lang,''),
                               COALESCE(t.text,''),
                               COALESCE(a.severity_level, 0.0),
                               COALESCE(a.relevance, 0.0),
                               COALESCE(a.litter_probability, 0.0),
                               COALESCE(a.hazard_probability, 0.0),
                               COALESCE(a.classification, 'unknown'),
                               DATE_FORMAT(t.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                               COALESCE(
                                 (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1),
                                 (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.anchor_tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1)
                               ),
                               COALESCE(a.summary, ''),
                               a.latitude,
                               a.longitude,
                               COALESCE(a.report_title, ''),
                               COALESCE(a.report_description, ''),
                               COALESCE(a.brand_display_name, ''),
                               COALESCE(a.brand_name, '')
                        FROM indexer_twitter_tweet t
                        JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                        LEFT JOIN external_ingest_index ei 
                          ON ei.source COLLATE utf8mb4_general_ci = 'twitter' COLLATE utf8mb4_general_ci
                         AND ei.external_id COLLATE utf8mb4_general_ci = CAST(t.tweet_id AS CHAR) COLLATE utf8mb4_general_ci
                        WHERE a.is_relevant = TRUE
                          AND ei.seq IS NULL
                          AND (t.created_at > ? OR (t.created_at = ? AND t.tweet_id > ?))
                        ORDER BY t.created_at ASC, t.tweet_id ASC
                        LIMIT ?"#,
                    (since.clone(), since.clone(), aid, effective_batch_size as u64),
                )
                .await?
            } else {
                info!("selecting tweets with created_at >= {} batch_size={} totals: ins={} upd={} err={}", since, effective_batch_size, total_inserted, total_updated, total_errors);
                conn.exec(
                    r#"SELECT t.tweet_id,
                               COALESCE(t.username,''),
                               COALESCE(t.lang,''),
                               COALESCE(t.text,''),
                               COALESCE(a.severity_level, 0.0),
                               COALESCE(a.relevance, 0.0),
                               COALESCE(a.litter_probability, 0.0),
                               COALESCE(a.hazard_probability, 0.0),
                               COALESCE(a.classification, 'unknown'),
                               DATE_FORMAT(t.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                           COALESCE(
                             (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1),
                             (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.anchor_tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1)
                           ),
                               COALESCE(a.summary, ''),
                               a.latitude,
                               a.longitude,
                               COALESCE(a.report_title, ''),
                               COALESCE(a.report_description, ''),
                               COALESCE(a.brand_display_name, ''),
                               COALESCE(a.brand_name, '')
                        FROM indexer_twitter_tweet t
                        JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                        LEFT JOIN external_ingest_index ei 
                          ON ei.source COLLATE utf8mb4_general_ci = 'twitter' COLLATE utf8mb4_general_ci
                         AND ei.external_id COLLATE utf8mb4_general_ci = CAST(t.tweet_id AS CHAR) COLLATE utf8mb4_general_ci
                        WHERE a.is_relevant = TRUE
                          AND ei.seq IS NULL
                          AND t.created_at >= ?
                        ORDER BY t.created_at ASC, t.tweet_id ASC
                        LIMIT ?"#,
                    (since.clone(), effective_batch_size as u64),
                )
                .await?
            }
        } else {
            info!("selecting tweets from start batch_size={} totals: ins={} upd={} err={}", effective_batch_size, total_inserted, total_updated, total_errors);
            conn.exec(
                r#"SELECT t.tweet_id,
                           COALESCE(t.username,''),
                           COALESCE(t.lang,''),
                           COALESCE(t.text,''),
                           COALESCE(a.severity_level, 0.0),
                           COALESCE(a.relevance, 0.0),
                           COALESCE(a.litter_probability, 0.0),
                           COALESCE(a.hazard_probability, 0.0),
                           COALESCE(a.classification, 'unknown'),
                           DATE_FORMAT(t.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                           COALESCE(
                             (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1),
                             (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.anchor_tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1)
                           ),
                           COALESCE(a.summary, ''),
                           a.latitude,
                           a.longitude,
                           COALESCE(a.report_title, ''),
                           COALESCE(a.report_description, ''),
                           COALESCE(a.brand_display_name, ''),
                           COALESCE(a.brand_name, '')
                    FROM indexer_twitter_tweet t
                    JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                    LEFT JOIN external_ingest_index ei 
                      ON ei.source COLLATE utf8mb4_general_ci = 'twitter' COLLATE utf8mb4_general_ci
                     AND ei.external_id COLLATE utf8mb4_general_ci = CAST(t.tweet_id AS CHAR) COLLATE utf8mb4_general_ci
                    WHERE a.is_relevant = TRUE
                      AND ei.seq IS NULL
                    ORDER BY t.created_at ASC, t.tweet_id ASC
                    LIMIT ?"#,
                (effective_batch_size as u64,),
            )
            .await?
        };

        if rows.is_empty() { info!("no more rows to submit"); break 'outer; }

        // Build payload (fetch per-tweet tags)
        let mut items: Vec<serde_json::Value> = Vec::with_capacity(rows.len());
        for row in rows.iter() {
            let tweet_id: i64 = row.get::<i64, _>(0).unwrap_or(0);
            let username: String = row.get::<String, _>(1).unwrap_or_default();
            let lang: String = row.get::<String, _>(2).unwrap_or_default();
            let text: String = row.get::<String, _>(3).unwrap_or_default();
            let severity: f64 = row.get::<Option<f64>, _>(4).unwrap_or(None).unwrap_or(0.0);
            let relevance: f64 = row.get::<Option<f64>, _>(5).unwrap_or(None).unwrap_or(0.0);
            let litter: f64 = row.get::<Option<f64>, _>(6).unwrap_or(None).unwrap_or(0.0);
            let hazard: f64 = row.get::<Option<f64>, _>(7).unwrap_or(None).unwrap_or(0.0);
            let classification: String = row.get::<Option<String>, _>(8).unwrap_or(None).unwrap_or_else(|| "unknown".to_string());
            let created_iso: String = row.get::<Option<String>, _>(9).unwrap_or(None).unwrap_or_default();
            let img_opt: Option<Vec<u8>> = row.get::<Option<Vec<u8>>, _>(10).unwrap_or(None);
            let summary: String = row.get::<Option<String>, _>(11).unwrap_or(None).unwrap_or_default();
            let latitude_opt: Option<f64> = row.get::<Option<f64>, _>(12).unwrap_or(None);
            let longitude_opt: Option<f64> = row.get::<Option<f64>, _>(13).unwrap_or(None);
            let report_title: String = row.get::<Option<String>, _>(14).unwrap_or(None).unwrap_or_default();
            let report_description: String = row.get::<Option<String>, _>(15).unwrap_or(None).unwrap_or_default();
            let brand_display_name: String = row.get::<Option<String>, _>(16).unwrap_or(None).unwrap_or_default();
            let brand_name: String = row.get::<Option<String>, _>(17).unwrap_or(None).unwrap_or_default();

            // Fetch display tag names for this tweet, union with anchor tweet tags if present
            let anchor_opt: Option<i64> = conn
                .exec_first::<(Option<i64>,), _, _>(
                    "SELECT anchor_tweet_id FROM indexer_twitter_tweet WHERE tweet_id = ?",
                    (tweet_id,),
                )
                .await
                .ok()
                .flatten()
                .and_then(|t| t.0);
            let tags: Vec<String> = if let Some(anchor_id) = anchor_opt {
                let tag_rows: Vec<(String,)> = conn.exec(
                    r#"SELECT DISTINCT t.display_name
                       FROM indexer_twitter_tweets_tags tt
                       JOIN indexer_twitter_tags t ON t.id = tt.tag_id
                       WHERE tt.tweet_id IN (?, ?)
                       ORDER BY t.display_name ASC"#,
                    (tweet_id, anchor_id),
                ).await.unwrap_or_default();
                tag_rows.into_iter().map(|(name,)| name).collect()
            } else {
                let tag_rows: Vec<(String,)> = conn.exec(
                    r#"SELECT t.display_name
                       FROM indexer_twitter_tweets_tags tt
                       JOIN indexer_twitter_tags t ON t.id = tt.tag_id
                       WHERE tt.tweet_id = ?
                       ORDER BY t.display_name ASC"#,
                    (tweet_id,),
                ).await.unwrap_or_default();
                tag_rows.into_iter().map(|(name,)| name).collect()
            };

            let title_source = if !report_title.is_empty() { report_title.clone() } else { text.clone() };
            let title = truncate_chars(&title_source, 120);
            let score = normalize_score(severity, relevance);
            let image_base64 = img_opt.as_ref().map(|b| STANDARD.encode(b));
            let url = format!("https://twitter.com/{}/status/{}", username, tweet_id);
            let mut content = if !report_description.is_empty() { report_description } else { text.clone() };
            if !url.is_empty() {
                content = format!("{} : {}", content, url);
            }
            let item = json!({
                "external_id": tweet_id.to_string(),
                "title": title,
                "content": truncate_chars(&content, 4000),
                "url": url,
                "created_at": created_iso,
                "updated_at": created_iso,
                "score": score,
                "metadata": {
                    "author_username": username,
                    "lang": lang,
                    "classification": classification,
                    "litter_probability": litter,
                    "hazard_probability": hazard,
                    "relevance": relevance,
                    "severity_level": severity,
                    "summary": summary,
                    "latitude": latitude_opt,
                    "longitude": longitude_opt,
                    "brand_display_name": brand_display_name,
                    "brand_name": brand_name
                },
                "tags": tags,
                "skip_ai": true,
                "image_base64": image_base64
            });
            items.push(item);
        }

        let payload = json!({
            "source": "twitter",
            "items": items,
        });

        let resp = client
            .post(format!(
                "{}/api/v3/reports/bulk_ingest",
                endpoint_url.trim_end_matches('/')
            ))
            .bearer_auth(&token)
            .json(&payload)
            .send()
            .await;

        match resp {
            Ok(r) => {
                if !r.status().is_success() {
                    let status = r.status();
                    let text = r.text().await.unwrap_or_default();
                    warn!("submit failed http {}: {}", status, text);
                    if status.as_u16() == 413 {
                        let new_size = std::cmp::max(50, effective_batch_size / 2);
                        if new_size < effective_batch_size {
                            info!(
                                "reducing effective_batch_size from {} to {} due to 413",
                                effective_batch_size, new_size
                            );
                            effective_batch_size = new_size;
                        }
                    }
                    sleep(StdDuration::from_secs(5)).await;
                    continue;
                }
                let v: serde_json::Value = r.json().await.unwrap_or_else(|_| json!({}));
                let inserted = v.get("inserted").and_then(|x| x.as_u64()).unwrap_or(0);
                let updated = v.get("updated").and_then(|x| x.as_u64()).unwrap_or(0);
                let skipped = v.get("skipped").and_then(|x| x.as_u64()).unwrap_or(0);
                let errs = v
                    .get("errors")
                    .and_then(|x| x.as_array())
                    .map(|a| a.len() as u64)
                    .unwrap_or(0);
                total_inserted += inserted;
                total_updated += updated;
                total_skipped += skipped;
                total_errors += errs;
                if errs > 0 {
                    let sample = v
                        .get("errors")
                        .and_then(|x| x.as_array())
                        .and_then(|a| a.get(0))
                        .cloned()
                        .unwrap_or(json!({}));
                    warn!("batch errors={} sample={}", errs, sample);
                }
                info!(
                    "submitted batch: rows={} inserted={} updated={} skipped={} (totals: ins={} upd={} skp={} err={})",
                    rows.len(),
                    inserted,
                    updated,
                    skipped,
                    total_inserted,
                    total_updated,
                    total_skipped,
                    total_errors
                );
            }
            Err(e) => {
                warn!("http error: {}", e);
                sleep(StdDuration::from_secs(5)).await;
                continue;
            }
        }

        // Update state to last row
        let (last_tweet_id, last_created_iso) = {
            let last = rows.last().unwrap();
            let tid: i64 = last.get::<i64, _>(0).unwrap_or(0);
            let created_iso: String = last.get::<String, _>(9).unwrap_or_default();
            (tid, created_iso)
        };
        let last_created_db = last_created_iso
            .replace('T', " ")
            .trim_end_matches('Z')
            .to_string();
        conn
            .exec_drop(
                "UPDATE indexer_twitter_submit_state SET last_submitted_created_at = ?, last_submitted_tweet_id = ?, updated_at = NOW() WHERE id = 1",
                (last_created_db, last_tweet_id),
            )
            .await?;

        total_sent += rows.len() as u64;
        if args.limit_total > 0 && total_sent >= args.limit_total {
            break 'outer;
        }
        sleep(StdDuration::from_millis(250)).await;
        }

        info!(
            "submitter_twitter finished cycle: total_sent={} totals: inserted={} updated={} skipped={} errors={}",
            total_sent, total_inserted, total_updated, total_skipped, total_errors
        );

        if args.limit_total > 0 { break; }
        sleep(StdDuration::from_secs(args.interval_secs)).await;
    }

    Ok(())
}

fn normalize_score(severity: f64, relevance: f64) -> f64 {
    // Prefer severity if > 0, otherwise use relevance; clamp to [0.7..1.0]
    let mut s = if severity > 0.0 { severity } else { 0.7 + 0.3 * relevance.max(0.0).min(1.0) };
    if s < 0.7 { s = 0.7; }
    if s > 1.0 { s = 1.0; }
    s
}

fn truncate_chars(s: &str, max_chars: usize) -> String {
    if s.chars().count() <= max_chars { return s.to_string(); }
    s.chars().take(max_chars).collect()
}


