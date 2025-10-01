use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::json;
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: Option<GeneralConfig>,
    submit: Option<SubmitConfig>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig { db_url: String }

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

    /// Batch size per HTTP request (max 1000)
    #[arg(long, default_value_t = 500)] batch_size: usize,
    /// Limit total rows to submit (0 = no limit)
    #[arg(long, default_value_t = 0)] limit_total: u64,
    /// Start from created_at >= this date (YYYY-MM-DD). Overrides saved state
    #[arg(long)] since_created: Option<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    // Load config (best-effort)
    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args.db_url.clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or config.general.db_url")?;
    let endpoint_url = args.endpoint_url.clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.submit.as_ref().and_then(|s| s.endpoint_url.clone())))
        .context("endpoint_url must be provided via --endpoint-url or config.submit.endpoint_url")?;
    let token = args.token.clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.submit.as_ref().and_then(|s| s.token.clone())))
        .context("token must be provided via --token or config.submit.token")?;

    let batch_size = args.batch_size.min(1000).max(1);

    info!("submitter_github: start endpoint={} batch_size={} limit_total={} since_created={:?}", endpoint_url, batch_size, args.limit_total, args.since_created);

    // DB and state setup
    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    let mut conn = pool.get_conn().await?;
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_github_issues_submit_state (
            id INT PRIMARY KEY DEFAULT 1,
            last_submitted_created_at DATETIME NULL,
            last_submitted_issue_id BIGINT NULL,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        )
    "#).await?;
    // Ensure a single row exists
    conn.query_drop("INSERT IGNORE INTO indexer_github_issues_submit_state (id) VALUES (1)").await?;

    // HTTP client
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

        // Determine start point
        let (saved_created, saved_issue_id): (Option<String>, Option<i64>) = {
            let row: Option<(Option<String>, Option<i64>)> = conn.exec_first(
                "SELECT DATE_FORMAT(last_submitted_created_at, '%Y-%m-%d %H:%i:%s'), last_submitted_issue_id FROM indexer_github_issues_submit_state WHERE id=1",
                (),
            ).await?;
            row.unwrap_or((None, None))
        };

        // Determine pagination anchors: prefer saved state if present; otherwise use CLI floor
        let since_created = if saved_created.is_some() { saved_created.clone() } else { args.since_created.clone() };
        let after_issue_id = if saved_created.is_some() { saved_issue_id } else { None };

        // Fetch next batch
        // Build SQL and execute with positional params
        let rows: Vec<(i64, i64, String, String, String, String, i32, i32, String, String)> = if let Some(ref since) = since_created {
            if let Some(aid) = after_issue_id {
                info!("selecting issues with (created_at, issue_id) > ({}, {}) batch_size={} totals: ins={} upd={} err={}", since, aid, effective_batch_size, total_inserted, total_updated, total_errors);
                conn.exec(
                    r#"SELECT issue_id, repo_id, repo_full_name, title, url, body, comments, reactions_plus_one,
                           DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'), DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ')
                      FROM indexer_github_issue
                     WHERE (created_at > ? OR (created_at = ? AND issue_id > ?))
                     ORDER BY created_at ASC, issue_id ASC
                     LIMIT ?"#,
                    (since.clone(), since.clone(), aid, effective_batch_size as u64)
                ).await?
            } else {
                info!("selecting issues with created_at >= {} batch_size={} totals: ins={} upd={} err={}", since, effective_batch_size, total_inserted, total_updated, total_errors);
                conn.exec(
                    r#"SELECT issue_id, repo_id, repo_full_name, title, url, body, comments, reactions_plus_one,
                           DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'), DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ')
                      FROM indexer_github_issue
                     WHERE created_at >= ?
                     ORDER BY created_at ASC, issue_id ASC
                     LIMIT ?"#,
                    (since.clone(), effective_batch_size as u64)
                ).await?
            }
        } else {
            info!("selecting issues from start batch_size={} totals: ins={} upd={} err={}", effective_batch_size, total_inserted, total_updated, total_errors);
            conn.exec(
                r#"SELECT issue_id, repo_id, repo_full_name, title, url, body, comments, reactions_plus_one,
                       DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'), DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ')
                  FROM indexer_github_issue
                 ORDER BY created_at ASC, issue_id ASC
                 LIMIT ?"#,
                (effective_batch_size as u64,)
            ).await?
        };

        if rows.is_empty() { info!("no more rows to submit"); break; }

        // Build payload
        let items: Vec<_> = rows.iter().map(|(issue_id, _repo_id, repo_full_name, title, url, body, _comments, plus1, created_iso, updated_iso)| {
            let sev = normalize_severity(*plus1 as i64);
            json!({
                "external_id": issue_id.to_string(),
                "title": title,
                "content": truncate_chars(body, 4000),
                "url": url,
                "created_at": created_iso,
                "updated_at": updated_iso,
                "score": sev,
                "metadata": {
                    "repo_full_name": repo_full_name,
                    "plus_one": plus1,
                },
                "skip_ai": true
            })
        }).collect();

        let payload = json!({
            "source": "github_issue",
            "items": items,
        });

        let resp = client.post(format!("{}/api/v3/reports/bulk_ingest", endpoint_url.trim_end_matches('/')))
            .bearer_auth(&token)
            .json(&payload)
            .send().await;

        match resp {
            Ok(r) => {
                if !r.status().is_success() {
                    let status = r.status();
                    let text = r.text().await.unwrap_or_default();
                    warn!("submit failed http {}: {}", status, text);
                    if status.as_u16() == 413 {
                        // Shrink batch and retry
                        let new_size = std::cmp::max(50, effective_batch_size / 2);
                        if new_size < effective_batch_size {
                            info!("reducing effective_batch_size from {} to {} due to 413", effective_batch_size, new_size);
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
                let errs = v.get("errors").and_then(|x| x.as_array()).map(|a| a.len() as u64).unwrap_or(0);
                total_inserted += inserted;
                total_updated += updated;
                total_skipped += skipped;
                total_errors += errs;
                if errs > 0 {
                    let sample = v.get("errors").and_then(|x| x.as_array()).and_then(|a| a.get(0)).cloned().unwrap_or(json!({}));
                    warn!("batch errors={} sample={}", errs, sample);
                }
                info!("submitted batch: rows={} inserted={} updated={} skipped={} (totals: ins={} upd={} skp={} err={})",
                    rows.len(), inserted, updated, skipped, total_inserted, total_updated, total_skipped, total_errors);
                // Optionally grow batch slowly if small
                if effective_batch_size < batch_size {
                    let grown = std::cmp::min(batch_size, effective_batch_size + 50);
                    if grown > effective_batch_size { effective_batch_size = grown; }
                }
            }
            Err(e) => {
                warn!("http error: {}", e);
                sleep(StdDuration::from_secs(5)).await;
                continue;
            }
        }

        // Update state to last row's created_at/id (restart-friendly, server is idempotent)
        let (last_issue_id, last_created_iso) = {
            let last = rows.last().unwrap();
            (last.0, last.8.clone())
        };
        // Convert ISO8601 to MySQL DATETIME format: "YYYY-MM-DD HH:MM:SS"
        let last_created_db = last_created_iso.replace('T', " ").trim_end_matches('Z').to_string();
        conn.exec_drop(
            "UPDATE indexer_github_issues_submit_state SET last_submitted_created_at = ?, last_submitted_issue_id = ?, updated_at = NOW() WHERE id = 1",
            (last_created_db, last_issue_id),
        ).await?;

        total_sent += rows.len() as u64;
        if args.limit_total > 0 && total_sent >= args.limit_total { break 'outer; }
        sleep(StdDuration::from_millis(250)).await;
    }

    info!("submitter_github finished: total_sent={} totals: inserted={} updated={} skipped={} errors={}",
        total_sent, total_inserted, total_updated, total_skipped, total_errors);
    Ok(())
}

fn normalize_severity(plus_one: i64) -> f64 {
    if plus_one <= 0 { return 0.7; }
    let ratio = (plus_one as f64) / 50.0; // 50+ likes -> cap
    let capped = if ratio > 1.0 { 1.0 } else { ratio };
    0.7 + 0.3 * capped
}

fn truncate_chars(s: &str, max_chars: usize) -> String {
    if s.chars().count() <= max_chars { return s.to_string(); }
    s.chars().take(max_chars).collect()
}


