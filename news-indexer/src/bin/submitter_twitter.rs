use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::json;
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
    #[arg(long)] db_url: Option<String>,
    #[arg(long)] endpoint_url: Option<String>,
    #[arg(long)] token: Option<String>,

    #[arg(long, default_value_t = 300)] batch_size: usize,
    #[arg(long, default_value_t = 0)] limit_total: u64,
    #[arg(long)] since_created: Option<String>,
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
        let rows: Vec<(i64, String, String, String, String, f64, f64, f64, f64, String, String, Option<Vec<u8>>)> = if let Some(ref since) = since_created {
            if let Some(aid) = after_tweet_id {
                info!("selecting tweets with (created_at, tweet_id) > ({}, {}) batch_size={} totals: ins={} upd={} err={}", since, aid, effective_batch_size, total_inserted, total_updated, total_errors);
                conn.exec(
                    r#"SELECT t.tweet_id,
                               COALESCE(t.username,''),
                               COALESCE(t.lang,''),
                               COALESCE(t.text,''),
                               COALESCE(t.url,''),
                               a.severity_level,
                               a.relevance,
                               a.litter_probability,
                               a.hazard_probability,
                               a.classification,
                               DATE_FORMAT(t.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                               (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1)
                        FROM indexer_twitter_tweet t
                        JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                        WHERE a.is_relevant = TRUE
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
                               COALESCE(t.url,''),
                               a.severity_level,
                               a.relevance,
                               a.litter_probability,
                               a.hazard_probability,
                               a.classification,
                               DATE_FORMAT(t.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                               (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1)
                        FROM indexer_twitter_tweet t
                        JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                        WHERE a.is_relevant = TRUE
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
                           COALESCE(t.url,''),
                           a.severity_level,
                           a.relevance,
                           a.litter_probability,
                           a.hazard_probability,
                           a.classification,
                           DATE_FORMAT(t.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                           (SELECT data FROM indexer_media_blob b WHERE b.sha256 = (SELECT m.sha256 FROM indexer_twitter_media m WHERE m.tweet_id=t.tweet_id AND m.type='photo' ORDER BY position ASC LIMIT 1) LIMIT 1)
                    FROM indexer_twitter_tweet t
                    JOIN indexer_twitter_analysis a ON a.tweet_id = t.tweet_id
                    WHERE a.is_relevant = TRUE
                    ORDER BY t.created_at ASC, t.tweet_id ASC
                    LIMIT ?"#,
                (effective_batch_size as u64,),
            )
            .await?
        };

        if rows.is_empty() { info!("no more rows to submit"); break; }

        // Build payload
        let items: Vec<_> = rows
            .iter()
            .map(
                |(
                    tweet_id,
                    username,
                    lang,
                    text,
                    url,
                    severity,
                    relevance,
                    litter,
                    hazard,
                    classification,
                    created_iso,
                    img_opt,
                )| {
                    let title = truncate_chars(text, 120);
                    let score = normalize_score(*severity, *relevance);
                    let image_base64 = img_opt.as_ref().map(|b| base64::encode(b));
                    json!({
                        "external_id": tweet_id.to_string(),
                        "title": title,
                        "content": truncate_chars(text, 4000),
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
                        },
                        "skip_ai": true,
                        "image_base64": image_base64
                    })
                },
            )
            .collect();

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
            (last.0, last.10.clone())
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
        "submitter_twitter finished: total_sent={} totals: inserted={} updated={} skipped={} errors={}",
        total_sent, total_inserted, total_updated, total_skipped, total_errors
    );
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


