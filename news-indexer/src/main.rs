use std::collections::HashMap;
use std::fs;
use anyhow::{Context, Result};
use clap::Parser;
use chrono::{Duration, Utc};
use log::{error, info};
use mysql_async::prelude::*;
use mysql_async::Row;
use reqwest::header;
use serde::Deserialize;
use serde_json::json;
use tokio::time::sleep;
use std::time::Duration as StdDuration;

#[derive(Deserialize)]
struct Config {
    general: GeneralConfig,
    twitter: TwitterConfig,
}

#[derive(Deserialize)]
struct GeneralConfig {
    dry_run: bool,
    prompt: String,
    timeframe_days: i64,
    poll_secs: u64,
    min_score: u32,
    db_url: String,
    cleanapp_api_url: String,
    bot_user_id: String,
    default_lat: f64,
    default_lon: f64,
}

#[derive(Deserialize)]
struct TwitterConfig {
    bearer_token: String,
    max_results: u32,
}

#[derive(Parser)]
struct Args {
    #[arg(long, default_value = "config.toml")]
    config_path: String,
}

struct Post {
    id: String,
    text: String,
    likes: u32,
    reposts: u32,
    replies: u32,
    timestamp: chrono::DateTime<Utc>,
    url: String,
    media_urls: Vec<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();
    let config_str = fs::read_to_string(&args.config_path).context("Failed to read config file")?;
    let config: Config = toml::from_str(&config_str).context("Failed to parse config")?;
    let opts = mysql_async::Opts::from_url(&config.general.db_url)?;
    let pool = mysql_async::Pool::new(opts);
    let timeframe = Duration::days(config.general.timeframe_days);
    loop {
        if let Err(e) = run_once(&pool, &config).await {
            error!("Error in run_once: {:?}", e);
        }
        sleep(StdDuration::from_secs(config.general.poll_secs)).await;
    }
}

async fn run_once(pool: &mysql_async::Pool, config: &Config) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    conn.query_drop(include_str!("../../db/patches/20250914_news_indexer.sql")).await?; // Assume patch content is inlined or adjust
    let last_time_opt: Option<Row> = conn.exec_first(
        "SELECT last_indexed_time FROM indexing_state WHERE platform = 'twitter'",
        (),
    ).await?;
    let last_time = last_time_opt.and_then(|row| row.get(0)).unwrap_or(Utc::now() - Duration::days(config.general.timeframe_days));
    let from_time = last_time - Duration::hours(1);
    let now = Utc::now();
    info!("Searching twitter from {} to {}", from_time, now);
    let mut posts = search_twitter(&config.twitter, &config.general.prompt, from_time, now).await?;
    posts.sort_by_key(|p| std::cmp::Reverse(p.likes + p.reposts + p.replies));
    posts.retain(|p| (p.likes + p.reposts + p.replies) >= config.general.min_score);
    info!("Found {} qualifying posts", posts.len());
    for post in posts {
        let exists: Option<u64> = conn.exec_first(
            "SELECT COUNT(*) FROM social_posts WHERE post_id = :id AND platform = 'twitter'",
            params! { "id" => &post.id },
        ).await?.map(|row: Row| row.get(0).unwrap());
        if exists.unwrap_or(0) > 0 {
            continue;
        }
        info!("Processing post {}: likes={}, reposts={}, replies={}, score={}", post.id, post.likes, post.reposts, post.replies, post.likes + post.reposts + post.replies);
        let mut image: Vec<u8> = vec![];
        if !post.media_urls.is_empty() {
            let resp = reqwest::get(&post.media_urls[0]).await?;
            if resp.status().is_success() {
                image = resp.bytes().await?.to_vec();
            }
        }
        let mut submitted = false;
        let mut seq: Option<i64> = None;
        if !config.general.dry_run {
            let client = reqwest::Client::new();
            let report = json!({
                "version": "2.0",
                "id": config.general.bot_user_id,
                "latitude": config.general.default_lat,
                "longitude": config.general.default_lon,
                "x": 0.0,
                "y": 0.0,
                "image": image,
                "action_id": "",
                "annotation": format!("Digital report from Twitter: {}\n{}", post.url, post.text),
            });
            let resp = client.post(&config.general.cleanapp_api_url).json(&report).send().await?;
            if resp.status().is_success() {
                let res: serde_json::Value = resp.json().await?;
                seq = res["seq"].as_i64();
                submitted = true;
            } else {
                error!("Failed to submit post {}: {}", post.id, resp.status());
            }
        }
        conn.exec_drop(
            r#"INSERT INTO social_posts (post_id, platform, url, content, likes, reposts, replies, post_timestamp, submitted_to_cleanapp, cleanapp_report_seq)
               VALUES (:post_id, 'twitter', :url, :content, :likes, :reposts, :replies, :post_timestamp, :submitted, :seq)"#,
            params! {
                "post_id" => &post.id,
                "url" => &post.url,
                "content" => &post.text,
                "likes" => post.likes,
                "reposts" => post.reposts,
                "replies" => post.replies,
                "post_timestamp" => post.timestamp,
                "submitted" => submitted,
                "seq" => seq,
            },
        ).await?;
    }
    conn.exec_drop(
        "INSERT INTO indexing_state (platform, last_indexed_time) VALUES ('twitter', :now) ON DUPLICATE KEY UPDATE last_indexed_time = :now",
        params! { "now" => now },
    ).await?;
    Ok(())
}

async fn search_twitter(config: &TwitterConfig, query: &str, from: chrono::DateTime<Utc>, to: chrono::DateTime<Utc>) -> Result<Vec<Post>> {
    let client = reqwest::Client::new();
    let url = "https://api.twitter.com/2/tweets/search/recent";
    let mut params = HashMap::new();
    params.insert("query", query.to_string());
    params.insert("start_time", from.to_rfc3339_opts(chrono::SecondsFormat::Secs, true));
    params.insert("end_time", to.to_rfc3339_opts(chrono::SecondsFormat::Secs, true));
    params.insert("max_results", config.max_results.to_string());
    params.insert("tweet.fields", "public_metrics,created_at".to_string());
    params.insert("expansions", "attachments.media_keys".to_string());
    params.insert("media.fields", "url".to_string());
    params.insert("sort_order", "recency".to_string());
    let resp = client.get(url)
        .header(header::AUTHORIZATION, format!("Bearer {}", config.bearer_token))
        .query(&params)
        .send().await?
        .json::<serde_json::Value>().await?;
    let mut posts = vec![];
    let data = resp["data"].as_array().unwrap_or(&vec![]);
    let includes = resp["includes"].as_object();
    let media = includes.and_then(|i| i.get("media")).and_then(|m| m.as_array()).unwrap_or(&vec![]);
    let media_map: HashMap<String, String> = media.iter().filter_map(|m| {
        if m["type"] == "photo" {
            Some((m["media_key"].as_str()?.to_string(), m["url"].as_str()?.to_string()))
        } else { None }
    }).collect();
    for tweet in data {
        let metrics = &tweet["public_metrics"];
        let likes = metrics["like_count"].as_u64().unwrap_or(0) as u32;
        let reposts = metrics["retweet_count"].as_u64().unwrap_or(0) as u32;
        let replies = metrics["reply_count"].as_u64().unwrap_or(0) as u32;
        let id = tweet["id"].as_str().unwrap_or("").to_string();
        let text = tweet["text"].as_str().unwrap_or("").to_string();
        let timestamp_str = tweet["created_at"].as_str().unwrap_or("");
        let timestamp = chrono::DateTime::parse_from_rfc3339(timestamp_str).map(|dt| dt.with_timezone(&Utc)).unwrap_or(Utc::now());
        let url = format!("https://x.com/i/web/status/{}", id);
        let mut media_urls = vec![];
        if let Some(attachments) = tweet.get("attachments") {
            if let Some(keys) = attachments.get("media_keys").and_then(|k| k.as_array()) {
                for key in keys {
                    if let Some(u) = media_map.get(key.as_str()?.to_string()) {
                        media_urls.push(u.clone());
                    }
                }
            }
        }
        posts.push(Post { id, text, likes, reposts, replies, timestamp, url, media_urls });
    }
    Ok(posts)
}
