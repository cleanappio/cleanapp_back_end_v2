use anyhow::Result;
use clap::Parser;
use log::info;
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use std::sync::{Arc, atomic::{AtomicU64, Ordering}};
use std::time::Duration as StdDuration;
use tokio::sync::Semaphore;
use tokio::time::sleep;
use chrono::Utc;

#[derive(Deserialize)]
struct Config {
    general: GeneralConfig,
    appstore: AppStoreConfig,
}

#[derive(Deserialize)]
struct GeneralConfig {
    keywords: Vec<String>,
    max_rating: u32,
    min_length: usize,
    timeframe_days: i64,
    db_url: String,
}

#[derive(Deserialize)]
struct AppStoreConfig {
    country: String,
    reviews_per_app: u32,
}

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")]
    config_path: String,

    /// Max apps to scan (0 = all)
    #[arg(long, default_value_t = 10)]
    limit_apps: u32,

    /// Number of concurrent app fetch workers
    #[arg(long, default_value_t = 6)]
    concurrency: usize,
}

struct Review {
    id: String,
    title: String,
    content: String,
    rating: u32,
    updated: chrono::DateTime<chrono::Utc>,
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();
    let cfg_str = std::fs::read_to_string(&args.config_path)?;
    let cfg: Config = toml::from_str(&cfg_str)?;

    let pool = Pool::new(mysql_async::Opts::from_url(&cfg.general.db_url)?);
    let mut conn = pool.get_conn().await?;

    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS social_posts (
          post_id VARCHAR(255) NOT NULL,
          platform VARCHAR(50) NOT NULL,
          url VARCHAR(255),
          content TEXT,
          likes INT,
          reposts INT,
          replies INT,
          post_timestamp TIMESTAMP,
          processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
          submitted_to_cleanapp BOOL DEFAULT FALSE,
          cleanapp_report_seq INT,
          PRIMARY KEY (post_id, platform)
        )
    "#).await?;

    let total_apps: u64 = conn.exec_first("SELECT COUNT(*) FROM indexer_appstore_apps", ()).await?.unwrap_or(0);
    let app_rows: Vec<(String, String)> = if args.limit_apps == 0 {
        info!("fetcher: scanning all {} apps", total_apps);
        conn.exec_map("SELECT app_id, name FROM indexer_appstore_apps ORDER BY updated_at DESC", (), |(id, name)| (id, name)).await?
    } else {
        let sel = std::cmp::min(args.limit_apps as u64, total_apps);
        info!("fetcher: scanning {} of {} apps", sel, total_apps);
        conn.exec_map("SELECT app_id, name FROM indexer_appstore_apps ORDER BY updated_at DESC LIMIT ?", (args.limit_apps,), |(id, name)| (id, name)).await?
    };

    drop(conn);

    let total_selected = app_rows.len() as u64;
    let processed = Arc::new(AtomicU64::new(0));
    let matched_apps = Arc::new(AtomicU64::new(0));
    let matched_total = Arc::new(AtomicU64::new(0));
    let written_new = Arc::new(AtomicU64::new(0));

    let sem = Arc::new(Semaphore::new(args.concurrency));
    let pool_arc = Arc::new(pool);
    let cfg_arc = Arc::new(cfg);

    let mut handles = Vec::with_capacity(app_rows.len());
    for (app_id, _app_name) in app_rows.into_iter() {
        let permit = sem.clone().acquire_owned().await?;
        let p = pool_arc.clone();
        let cfgc = cfg_arc.clone();
        let processed_c = processed.clone();
        let matched_apps_c = matched_apps.clone();
        let matched_total_c = matched_total.clone();
        let written_new_c = written_new.clone();
        let total_selected_c = total_selected;
        let handle = tokio::spawn(async move {
            let _perm = permit;
            // Fetch and filter
            let reviews = match fetch_app_reviews_paged(&cfgc.appstore, &app_id, cfgc.appstore.reviews_per_app).await {
                Ok(v) => v,
                Err(_) => vec![],
            };
            let win_start = Utc::now() - chrono::Duration::days(cfgc.general.timeframe_days);
            let mut app_matched = 0usize;
            for r in reviews.into_iter() {
                let text = format!("{} {}", r.title, r.content).to_lowercase();
                let has_keyword = cfgc.general.keywords.iter().any(|k| text.contains(&k.to_lowercase()));
                let is_low_rating = r.rating <= cfgc.general.max_rating;
                let is_substantial = text.trim().len() > cfgc.general.min_length;
                if !(has_keyword && is_low_rating && is_substantial && r.updated >= win_start) { continue; }
                app_matched += 1;
                let content = format!("{}: {}", r.title, r.content);
                let url = format!("https://apps.apple.com/{}/app/id{}", cfgc.appstore.country, app_id);
                if let Ok(mut c) = p.get_conn().await {
                    // Insert if new
                    if c.exec_drop(
                        r#"INSERT IGNORE INTO social_posts (post_id, platform, url, content, likes, reposts, replies, post_timestamp, submitted_to_cleanapp)
                           VALUES (:post_id, 'appstore', :url, :content, :rating, 0, 0, :post_timestamp, false)"#,
                        params!{
                            "post_id" => &r.id,
                            "url" => &url,
                            "content" => &content,
                            "rating" => r.rating as i32,
                            "post_timestamp" => r.updated.format("%Y-%m-%d %H:%M:%S").to_string(),
                        }
                    ).await.is_ok() {
                        if let Ok(row_count_opt) = c.exec_first::<i64, _, _>("SELECT ROW_COUNT()", ()).await {
                            if row_count_opt.unwrap_or(0) > 0 { written_new_c.fetch_add(1, Ordering::Relaxed); }
                            else {
                                // Update only if not yet submitted
                                let _ = c.exec_drop(
                                    "UPDATE social_posts SET content=:content, url=:url WHERE post_id=:post_id AND platform='appstore' AND submitted_to_cleanapp=false",
                                    params!{"content"=>&content, "url"=>&url, "post_id"=>&r.id}
                                ).await;
                            }
                        }
                    }
                }
            }
            if app_matched > 0 {
                matched_apps_c.fetch_add(1, Ordering::Relaxed);
                matched_total_c.fetch_add(app_matched as u64, Ordering::Relaxed);
                info!("App {}: {} -> {} matched", app_id, cfgc.appstore.reviews_per_app, app_matched);
            }
            let done = processed_c.fetch_add(1, Ordering::Relaxed) + 1;
            if done % 20 == 0 || done == total_selected_c {
                let rem = total_selected_c.saturating_sub(done);
                info!(
                    "progress(fetch): processed={}/{} remaining={} matched_apps={} matched_total={}",
                    done,
                    total_selected_c,
                    rem,
                    matched_apps_c.load(Ordering::Relaxed),
                    matched_total_c.load(Ordering::Relaxed)
                );
            }
        });
        handles.push(handle);
    }

    for h in handles { let _ = h.await; }

    info!(
        "fetcher done: processed={} matched_apps={} matched_total={} new_rows={}",
        processed.load(Ordering::Relaxed),
        matched_apps.load(Ordering::Relaxed),
        matched_total.load(Ordering::Relaxed),
        written_new.load(Ordering::Relaxed)
    );

    Ok(())
}

async fn fetch_app_reviews_paged(config: &AppStoreConfig, app_id: &str, limit: u32) -> Result<Vec<Review>> {
    let client = reqwest::Client::builder().user_agent("news-indexer/0.1").timeout(StdDuration::from_secs(20)).build()?;
    let mut out = Vec::new();
    let mut page = 1u32;
    let max_pages = 10u32;
    while (out.len() as u32) < limit && page <= max_pages {
        sleep(StdDuration::from_millis(150)).await;
        let url = format!("https://itunes.apple.com/{}/rss/customerreviews/page={}/id={}/sortBy=mostRecent/json", config.country, page, app_id);
        let resp = client.get(&url).send().await?;
        if !resp.status().is_success() { break; }
        let body = resp.text().await.unwrap_or_default();
        let parsed: serde_json::Value = match serde_json::from_str(&body) { Ok(v) => v, Err(_) => break };
        let entries = parsed["feed"]["entry"].as_array().cloned().unwrap_or_default();
        let mut added = 0usize;
        for e in entries {
            if e.get("im:rating").is_none() { continue; }
            let id = e["id"]["label"].as_str().unwrap_or("").to_string();
            let title = e["title"]["label"].as_str().unwrap_or("").to_string();
            let content = e["content"]["label"].as_str().unwrap_or("").to_string();
            let rating = e["im:rating"]["label"].as_str().unwrap_or("0").parse::<u32>().unwrap_or(0);
            let updated = e["updated"]["label"].as_str().unwrap_or("");
            let updated = chrono::DateTime::parse_from_rfc3339(updated).map(|dt| dt.with_timezone(&Utc)).unwrap_or(Utc::now());
            out.push(Review{ id, title, content, rating, updated });
            added += 1;
            if (out.len() as u32) >= limit { break; }
        }
        if added == 0 { break; }
        page += 1;
    }
    Ok(out)
}
