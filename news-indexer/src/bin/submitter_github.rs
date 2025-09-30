use anyhow::{Context, Result};
use clap::Parser;
use log::{error, info, warn};
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

        let since_created = args.since_created.clone().or(saved_created);

        // Fetch next batch
        let query = if let Some(ref since) = since_created {
            info!("selecting issues with created_at >= {}", since);
            r#"SELECT issue_id, repo_id, repo_full_name, title, url, body, comments, reactions_plus_one,
                       DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'), DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ')
                FROM indexer_github_issue
                WHERE created_at >= ?
                ORDER BY created_at ASC, issue_id ASC
                LIMIT ?"#
        } else {
            "SELECT issue_id, repo_id, repo_full_name, title, url, body, comments, reactions_plus_one,
                     DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ'), DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ')
             FROM indexer_github_issue
             ORDER BY created_at ASC, issue_id ASC
             LIMIT ?"
        };

        let rows: Vec<(i64, i64, String, String, String, String, i32, i32, String, String)> = if since_created.is_some() {
            conn.exec(query, (since_created.clone().unwrap(), batch_size as u64)).await?
        } else {
            conn.exec(query, (batch_size as u64,)).await?
        };

        if rows.is_empty() { info!("no more rows to submit"); break; }

        // Build payload
        let items: Vec<_> = rows.iter().map(|(issue_id, _repo_id, repo_full_name, title, url, body, _comments, plus1, created_iso, updated_iso)| {
            let sev = normalize_severity(*plus1 as i64);
            json!({
                "external_id": issue_id.to_string(),
                "title": title,
                "content": body,
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
                    sleep(StdDuration::from_secs(5)).await;
                    continue;
                }
                let v: serde_json::Value = r.json().await.unwrap_or_else(|_| json!({}));
                let inserted = v.get("inserted").and_then(|x| x.as_u64()).unwrap_or(0);
                let updated = v.get("updated").and_then(|x| x.as_u64()).unwrap_or(0);
                let skipped = v.get("skipped").and_then(|x| x.as_u64()).unwrap_or(0);
                info!("submitted batch: rows={} inserted={} updated={} skipped={}", rows.len(), inserted, updated, skipped);
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
        conn.exec_drop(
            "UPDATE indexer_github_issues_submit_state SET last_submitted_created_at = ?, last_submitted_issue_id = ?, updated_at = NOW() WHERE id = 1",
            (last_created_iso, last_issue_id),
        ).await?;

        total_sent += rows.len() as u64;
        if args.limit_total > 0 && total_sent >= args.limit_total { break 'outer; }
        sleep(StdDuration::from_millis(250)).await;
    }

    info!("submitter_github finished: total_sent={}", total_sent);
    Ok(())
}

fn normalize_severity(plus_one: i64) -> f64 {
    if plus_one <= 0 { return 0.7; }
    let ratio = (plus_one as f64) / 50.0; // 50+ likes -> cap
    let capped = if ratio > 1.0 { 1.0 } else { ratio };
    0.7 + 0.3 * capped
}


