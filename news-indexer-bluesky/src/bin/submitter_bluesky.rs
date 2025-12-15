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

#[path = "../indexer_bluesky_schema.rs"]
mod indexer_bluesky_schema;

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
    #[arg(long, default_value = "config.toml")]
    config_path: String,
    #[arg(long, env = "DB_URL")]
    db_url: Option<String>,
    #[arg(long, env = "SUBMIT_ENDPOINT_URL")]
    endpoint_url: Option<String>,
    #[arg(long, env = "SUBMIT_TOKEN")]
    token: Option<String>,
    #[arg(long, env = "SUBMIT_BATCH_SIZE", default_value_t = 100)]
    batch_size: usize,
    #[arg(long, env = "SUBMIT_INTERVAL_SECS", default_value_t = 300)]
    interval_secs: u64,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args
        .db_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or DB_URL")?;

    let endpoint_url = args
        .endpoint_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.submit.as_ref().and_then(|s| s.endpoint_url.clone())))
        .context("endpoint_url must be provided via SUBMIT_ENDPOINT_URL")?;

    let token = args
        .token
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.submit.as_ref().and_then(|s| s.token.clone())))
        .context("token must be provided via SUBMIT_TOKEN")?;

    let batch_size = args.batch_size.min(500).max(1);

    info!(
        "submitter_bluesky: start endpoint={} batch_size={} interval={}s",
        endpoint_url, batch_size, args.interval_secs
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_bluesky_schema::ensure_bluesky_tables(&pool).await?;

    let client = reqwest::Client::builder()
        .timeout(StdDuration::from_secs(60))
        .build()?;

    loop {
        if let Err(e) = run_once(&pool, &client, &endpoint_url, &token, batch_size).await {
            warn!("run_once error: {e}");
        }
        sleep(StdDuration::from_secs(args.interval_secs)).await;
    }
}

async fn run_once(
    pool: &Pool,
    client: &reqwest::Client,
    endpoint_url: &str,
    token: &str,
    batch_size: usize,
) -> Result<()> {
    let mut conn = pool.get_conn().await?;


    // Fetch analyzed posts not yet submitted
    // Uses external_ingest_index exclusion only (not date-based state tracking)
    // to properly handle backlog where posts are analyzed out of order
    let rows: Vec<Row> = conn.exec(
        r#"SELECT p.uri, COALESCE(p.author_handle,''), COALESCE(p.text,''),
                  COALESCE(a.severity_level, 0.0), COALESCE(a.relevance, 0.0),
                  COALESCE(a.classification, 'digital'),
                  DATE_FORMAT(p.created_at, '%Y-%m-%dT%H:%i:%sZ'),
                  (SELECT data FROM indexer_media_blob b WHERE b.sha256 = 
                   (SELECT m.sha256 FROM indexer_bluesky_media m WHERE m.post_uri=p.uri ORDER BY position ASC LIMIT 1) LIMIT 1),
                  COALESCE(a.summary, ''), COALESCE(a.report_title, ''),
                  COALESCE(a.report_description, ''), COALESCE(a.brand_display_name, ''),
                  COALESCE(a.brand_name, ''), COALESCE(a.inferred_contact_emails, '[]')
           FROM indexer_bluesky_post p
           JOIN indexer_bluesky_analysis a ON a.uri = p.uri
           LEFT JOIN external_ingest_index ei 
             ON ei.source = 'bluesky' AND ei.external_id COLLATE utf8mb4_unicode_ci = p.uri
           WHERE a.is_relevant = TRUE
             AND ei.seq IS NULL
           ORDER BY p.created_at ASC, p.uri ASC
           LIMIT ?"#,
        (batch_size as u64,),
    )
    .await?;

    if rows.is_empty() {
        info!("submitter: no posts to submit");
        return Ok(());
    }

    info!("submitter: building payload for {} posts", rows.len());

    // Build payload
    let mut items: Vec<serde_json::Value> = Vec::with_capacity(rows.len());
    for row in rows.iter() {
        let uri: String = row.get::<String, _>(0).unwrap_or_default();
        let author_handle: String = row.get::<String, _>(1).unwrap_or_default();
        let text: String = row.get::<String, _>(2).unwrap_or_default();
        let severity: f64 = row.get::<Option<f64>, _>(3).unwrap_or(None).unwrap_or(0.0);
        let relevance: f64 = row.get::<Option<f64>, _>(4).unwrap_or(None).unwrap_or(0.0);
        let classification: String = row.get::<Option<String>, _>(5).unwrap_or(None).unwrap_or_else(|| "digital".to_string());
        let created_iso: String = row.get::<Option<String>, _>(6).unwrap_or(None).unwrap_or_default();
        let img_opt: Option<Vec<u8>> = row.get::<Option<Vec<u8>>, _>(7).unwrap_or(None);
        let summary: String = row.get::<Option<String>, _>(8).unwrap_or(None).unwrap_or_default();
        let report_title: String = row.get::<Option<String>, _>(9).unwrap_or(None).unwrap_or_default();
        let report_description: String = row.get::<Option<String>, _>(10).unwrap_or(None).unwrap_or_default();
        let brand_display_name: String = row.get::<Option<String>, _>(11).unwrap_or(None).unwrap_or_default();
        let brand_name: String = row.get::<Option<String>, _>(12).unwrap_or(None).unwrap_or_default();
        let inferred_contact_emails: String = row.get::<Option<String>, _>(13).unwrap_or(None).unwrap_or_else(|| "[]".to_string());

        // Build web URL from AT URI
        // at://did:plc:xxx/app.bsky.feed.post/yyy -> https://bsky.app/profile/handle/post/yyy
        // Note: Bluesky URLs work with either handle or DID
        let post_id = uri.rsplit('/').next().unwrap_or("");
        // Use author_handle if available, otherwise extract DID from URI
        let profile_id = if !author_handle.is_empty() {
            author_handle.clone()
        } else {
            // Extract DID from AT URI: at://did:plc:xxx/app.bsky.feed.post/yyy
            uri.strip_prefix("at://")
                .and_then(|s| s.split('/').next())
                .unwrap_or("")
                .to_string()
        };
        let url = format!("https://bsky.app/profile/{}/post/{}", profile_id, post_id);

        let title = if !report_title.is_empty() {
            truncate_chars(&report_title, 120)
        } else {
            truncate_chars(&text, 120)
        };

        let mut content = if !report_description.is_empty() {
            report_description
        } else {
            text.clone()
        };
        content = format!("{} : {}", content, url);

        let score = normalize_score(severity, relevance);
        let image_base64 = img_opt.as_ref().map(|b| STANDARD.encode(b));

        let item = json!({
            "external_id": uri,
            "title": title,
            "content": truncate_chars(&content, 4000),
            "url": url,
            "created_at": created_iso,
            "updated_at": created_iso,
            "score": score,
            "metadata": {
                "author_handle": author_handle,
                "classification": classification,
                "relevance": relevance,
                "severity_level": severity,
                "summary": summary,
                "brand_display_name": brand_display_name,
                "brand_name": brand_name,
                "inferred_contact_emails": serde_json::from_str::<serde_json::Value>(&inferred_contact_emails).unwrap_or(json!([]))
            },
            "tags": ["bluesky"],
            "skip_ai": true,
            "image_base64": image_base64
        });
        items.push(item);
    }

    let payload = json!({
        "source": "bluesky",
        "items": items,
    });

    // Submit
    let resp = client
        .post(format!("{}/api/v3/reports/bulk_ingest", endpoint_url.trim_end_matches('/')))
        .bearer_auth(token)
        .json(&payload)
        .send()
        .await;

    match resp {
        Ok(r) => {
            if !r.status().is_success() {
                let status = r.status();
                let text = r.text().await.unwrap_or_default();
                warn!("submit failed http {}: {}", status, text);
                return Ok(());
            }
            let v: serde_json::Value = r.json().await.unwrap_or_else(|_| json!({}));
            let inserted = v.get("inserted").and_then(|x| x.as_u64()).unwrap_or(0);
            let updated = v.get("updated").and_then(|x| x.as_u64()).unwrap_or(0);
            info!("submitted batch: rows={} inserted={} updated={}", rows.len(), inserted, updated);
        }
        Err(e) => {
            warn!("http error: {}", e);
            return Ok(());
        }
    }



    Ok(())
}

fn normalize_score(severity: f64, relevance: f64) -> f64 {
    let mut s = if severity > 0.0 { severity } else { 0.7 + 0.3 * relevance.clamp(0.0, 1.0) };
    s = s.clamp(0.7, 1.0);
    s
}

fn truncate_chars(s: &str, max_chars: usize) -> String {
    if s.chars().count() <= max_chars {
        return s.to_string();
    }
    s.chars().take(max_chars).collect()
}
