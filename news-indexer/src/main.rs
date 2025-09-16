use std::fs;
use anyhow::{Context, Result};
use clap::Parser;
use chrono::{Duration, Utc};
use log::{error, info};
use mysql_async::prelude::*;
use mysql_async::Row;
use serde::Deserialize;
use serde_json::json;
use tokio::time::sleep;
use std::time::Duration as StdDuration;
use reqwest::header;

#[derive(Deserialize)]
struct Config {
    general: GeneralConfig,
    appstore: AppStoreConfig,
}

#[derive(Deserialize)]
struct GeneralConfig {
    dry_run: bool,
    keywords: Vec<String>,
    max_rating: u32,
    min_length: usize,
    timeframe_days: i64,
    poll_secs: u64,
    db_url: String,
    cleanapp_api_url: String,
    bot_user_id: String,
    default_lat: f64,
    default_lon: f64,
    max_submissions_per_run: u32,
    host_header: Option<String>,
}

#[derive(Deserialize)]
struct AppStoreConfig {
    country: String,
    top_apps_limit: u32,
    reviews_per_app: u32,
}

#[derive(Parser)]
struct Args {
    #[arg(long, default_value = "config.toml")]
    config_path: String,
}

struct Review {
    id: String,
    title: String,
    content: String,
    rating: u32,
    updated: chrono::DateTime<Utc>,
    app_id: String,
    app_name: String,
}

fn truncate_utf8_by_bytes(input: &str, max_bytes: usize) -> String {
    if input.len() <= max_bytes { return input.to_string(); }
    let mut acc = String::with_capacity(max_bytes);
    let mut used = 0usize;
    for ch in input.chars() {
        let ch_len = ch.len_utf8();
        if used + ch_len > max_bytes { break; }
        acc.push(ch);
        used += ch_len;
    }
    if acc.len() < input.len() {
        let ell = "…";
        let ell_len = ell.len();
        if used + ell_len <= max_bytes { acc.push_str(ell); }
    }
    acc
}

async fn submit_with_retries(client: &reqwest::Client, url: &str, host_header: Option<&String>, payload: serde_json::Value) -> Result<Option<i64>> {
    let mut attempt: u32 = 0;
    let max_attempts: u32 = 6; // ~1+2+4+8+16+32s backoff worst-case
    loop {
        let mut req = client.post(url).json(&payload);
        if let Some(host) = host_header { req = req.header(header::HOST, host); }
        match req.send().await {
            Ok(resp) => {
                if resp.status().is_success() {
                    let res: serde_json::Value = resp.json().await.unwrap_or_else(|_| json!({"seq": null}));
                    return Ok(res["seq"].as_i64());
                }
                let status = resp.status();
                // Retry on 5xx
                if status.is_server_error() && attempt + 1 < max_attempts {
                    attempt += 1;
                    let delay = StdDuration::from_secs(1u64 << (attempt - 1).min(5));
                    error!("submission failed with {}. retrying in {:?} (attempt {}/{})", status, delay, attempt, max_attempts);
                    sleep(delay).await;
                    continue;
                } else {
                    error!("submission failed with status {} and will not retry", status);
                    return Ok(None);
                }
            }
            Err(e) => {
                // Retry on connect/dns/timeout
                let retryable = e.is_connect() || e.is_timeout() || e.is_request();
                if retryable && attempt + 1 < max_attempts {
                    attempt += 1;
                    let delay = StdDuration::from_secs(1u64 << (attempt - 1).min(5));
                    error!("submission transport error: {}. retrying in {:?} (attempt {}/{})", e, delay, attempt, max_attempts);
                    sleep(delay).await;
                    continue;
                } else {
                    error!("submission error (not retrying): {}", e);
                    return Ok(None);
                }
            }
        }
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();
    let config_str = fs::read_to_string(&args.config_path).context("Failed to read config file")?;
    let config: Config = toml::from_str(&config_str).context("Failed to parse config")?;
    let opts = mysql_async::Opts::from_url(&config.general.db_url)?;
    let pool = mysql_async::Pool::new(opts);
    loop {
        if let Err(e) = run_once(&pool, &config).await {
            error!("Error in run_once: {:?}", e);
        }
        sleep(StdDuration::from_secs(config.general.poll_secs)).await;
    }
}

async fn run_once(pool: &mysql_async::Pool, config: &Config) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    conn.query_drop(include_str!("../../db/patches/20250914_news_indexer.sql")).await?;

    // Compute timeframe window start; ignore last_indexed_time for filtering to allow backfills
    let window_start = Utc::now() - Duration::days(config.general.timeframe_days);

    info!("Fetching top {} apps for {}", config.appstore.top_apps_limit, config.appstore.country);
    let app_ids = fetch_top_apps(&config.appstore).await?;

    let mut all_reviews = vec![];
    for (app_id, app_name) in app_ids {
        info!("Fetching reviews for app {} ({})", app_id, app_name);
        let reviews = fetch_app_reviews(&config.appstore, &app_id, config.appstore.reviews_per_app).await?;
        let before = reviews.len();
        let filtered: Vec<Review> = reviews.into_iter().filter(|r| {
            let text = format!("{} {}", r.title, r.content).to_lowercase();
            let has_keyword = config.general.keywords.iter().any(|k| text.contains(&k.to_lowercase()));
            let is_low_rating = r.rating <= config.general.max_rating;
            let is_substantial = text.trim().len() > config.general.min_length;
            has_keyword && is_low_rating && is_substantial && r.updated >= window_start
        }).collect();
        if !filtered.is_empty() {
            info!("App {}: {} -> {} matched", app_id, before, filtered.len());
        }
        all_reviews.extend(filtered.into_iter().map(|mut r| { r.app_id = app_id.clone(); r.app_name = app_name.clone(); r }));
    }

    // Sort by recency
    all_reviews.sort_by_key(|r| std::cmp::Reverse(r.updated));

    info!("Found {} qualifying reviews", all_reviews.len());

    let mut submissions_done: u32 = 0;
    let http_client = reqwest::Client::builder()
        .user_agent("news-indexer/0.1 (+https://cleanapp.io)")
        .timeout(StdDuration::from_secs(30))
        .build()?;

    for review in all_reviews {
        let exists: Option<u64> = conn.exec_first(
            "SELECT COUNT(*) FROM social_posts WHERE post_id = :id AND platform = 'appstore'",
            params! { "id" => &review.id },
        ).await?.map(|row: Row| row.get(0).unwrap());
        if exists.unwrap_or(0) > 0 {
            continue;
        }

        info!("Processing review {} for app {}: rating={}, title='{}'", review.id, review.app_name, review.rating, review.title);

        let annotation_full = format!(
            "Digital UX complaint from App Store - {} (rating {}): {}\n{}",
            review.app_name, review.rating, review.title, review.content
        );
        let annotation = truncate_utf8_by_bytes(&annotation_full, 250);

        let mut submitted = false;
        let mut seq: Option<i64> = None;
        let can_submit = !config.general.dry_run && submissions_done < config.general.max_submissions_per_run;
        if can_submit {
            let payload = json!({
                "version": "2.0",
                "id": config.general.bot_user_id,
                "latitude": config.general.default_lat,
                "longitude": config.general.default_lon,
                "x": 0.0,
                "y": 0.0,
                "image": "",
                "action_id": "",
                "annotation": annotation,
            });
            match submit_with_retries(&http_client, &config.general.cleanapp_api_url, config.general.host_header.as_ref(), payload).await {
                Ok(maybe_seq) => {
                    seq = maybe_seq;
                    submitted = seq.is_some() || true; // consider submitted even if seq missing
                    submissions_done += 1;
                }
                Err(e) => {
                    error!("Failed to submit after retries: {}", e);
                }
            }
        } else if !config.general.dry_run {
            info!("Submission cap reached ({}), skipping submission for {}", config.general.max_submissions_per_run, review.id);
        } else {
            info!("Dry run: Would submit - {}", annotation);
        }

        let ts_str = review.updated.format("%Y-%m-%d %H:%M:%S").to_string();
        conn.exec_drop(
            r#"INSERT INTO social_posts (post_id, platform, url, content, likes, reposts, replies, post_timestamp, submitted_to_cleanapp, cleanapp_report_seq)
               VALUES (:post_id, 'appstore', :url, :content, :rating, 0, 0, :post_timestamp, :submitted, :seq)"#,
            params! {
                "post_id" => &review.id,
                "url" => format!("https://apps.apple.com/{}/app/id{}", config.appstore.country, review.app_id),
                "content" => format!("{}: {}", review.title, review.content),
                "rating" => review.rating,
                "post_timestamp" => ts_str,
                "submitted" => submitted,
                "seq" => seq,
            },
        ).await?;

        if submissions_done >= config.general.max_submissions_per_run {
            info!("Reached submission cap for this run: {}", submissions_done);
        }
    }

    conn.query_drop(
        "INSERT INTO indexing_state (platform, last_indexed_time) VALUES ('appstore', NOW()) ON DUPLICATE KEY UPDATE last_indexed_time = NOW()",
    ).await?;

    Ok(())
}

async fn fetch_top_apps(config: &AppStoreConfig) -> Result<Vec<(String, String)>> {
    let client = reqwest::Client::builder()
        .user_agent("news-indexer/0.1 (+https://cleanapp.io)")
        .timeout(StdDuration::from_secs(20))
        .build()?;
    let limit = std::cmp::min(config.top_apps_limit, 200);
    let url = format!(
        "https://itunes.apple.com/{}/rss/topfreeapplications/limit={}/json",
        config.country, limit
    );
    let resp = client.get(&url).send().await?;
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        error!("top apps fetch failed: {} body_head={}", status, &body.chars().take(200).collect::<String>());
        return Ok(vec![]);
    }
    let body = resp.text().await.unwrap_or_default();
    let parsed: serde_json::Value = match serde_json::from_str(&body) {
        Ok(v) => v,
        Err(e) => {
            error!("failed to parse top apps JSON: {} body_head={}", e, &body.chars().take(200).collect::<String>());
            return Ok(vec![]);
        }
    };
    let entries_vec = parsed["feed"]["entry"].as_array().cloned().unwrap_or_default();
    let mut apps = vec![];
    for entry in entries_vec {
        let id = entry["id"]["attributes"]["im:id"].as_str().unwrap_or("").to_string();
        let name = entry["im:name"]["label"].as_str().unwrap_or("").to_string();
        if !id.is_empty() {
            apps.push((id, name));
        }
    }
    Ok(apps)
}

async fn fetch_app_reviews(config: &AppStoreConfig, app_id: &str, limit: u32) -> Result<Vec<Review>> {
    let client = reqwest::Client::builder()
        .user_agent("news-indexer/0.1 (+https://cleanapp.io)")
        .timeout(StdDuration::from_secs(20))
        .build()?;
    // small pause to be polite and reduce rate limiting
    sleep(StdDuration::from_millis(200)).await;
    let url = format!(
        "https://itunes.apple.com/{}/rss/customerreviews/page=1/id={}/sortBy=mostRecent/json",
        config.country, app_id
    );
    let resp = client.get(&url).send().await?;
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        error!("reviews fetch failed for app {}: {} body_head={}", app_id, status, &body.chars().take(200).collect::<String>());
        return Ok(vec![]);
    }
    let body = resp.text().await.unwrap_or_default();
    let parsed: serde_json::Value = match serde_json::from_str(&body) {
        Ok(v) => v,
        Err(e) => {
            error!("failed to parse reviews JSON for app {}: {} body_head={}", app_id, e, &body.chars().take(200).collect::<String>());
            return Ok(vec![]);
        }
    };
    let entries_vec = parsed["feed"]["entry"].as_array().cloned().unwrap_or_default();
    let mut reviews = vec![];
    for entry in entries_vec {
        if entry.get("im:rating").is_none() { continue; }
        let id = entry["id"]["label"].as_str().unwrap_or("").to_string();
        let title = entry["title"]["label"].as_str().unwrap_or("").to_string();
        let content = entry["content"]["label"].as_str().unwrap_or("").to_string();
        let rating_str = entry["im:rating"]["label"].as_str().unwrap_or("0");
        let rating = rating_str.parse::<u32>().unwrap_or(0);
        let updated_str = entry["updated"]["label"].as_str().unwrap_or("");
        let updated = chrono::DateTime::parse_from_rfc3339(updated_str)
            .map(|dt| dt.with_timezone(&Utc))
            .unwrap_or_else(|_| Utc::now());
        reviews.push(Review { id, title, content, rating, updated, app_id: app_id.to_string(), app_name: String::new() });
    }
    reviews.truncate(limit as usize);
    Ok(reviews)
}

