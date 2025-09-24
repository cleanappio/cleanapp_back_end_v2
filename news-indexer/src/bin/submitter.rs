use anyhow::Result;
use clap::Parser;
use log::{error, info};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::json;
use std::sync::Arc;
use std::time::Duration as StdDuration;
use tokio::sync::Semaphore;

#[derive(Deserialize)]
struct Config {
    general: GeneralConfig,
}

#[derive(Deserialize)]
struct GeneralConfig {
    db_url: String,
    cleanapp_api_url: String,
    bot_user_id: String,
}

#[derive(Parser, Debug, Clone)]
struct Args {
    /// Optional config path (shares with fetcher)
    #[arg(long, default_value = "config.toml")]
    config_path: String,

    /// Override DB URL; if not set, read from config
    #[arg(long)]
    db_url: Option<String>,

    /// Override CleanApp API URL; if not set, read from config
    #[arg(long)]
    cleanapp_api_url: Option<String>,

    /// Override bot user id; if not set, read from config
    #[arg(long)]
    bot_user_id: Option<String>,

    #[arg(long, default_value_t = 5)]
    concurrency: usize,

    /// Max rows to submit this run (0 = unlimited)
    #[arg(long, default_value_t = 10)]
    limit_rows: u32,

    /// Country code for link format (kept for future use)
    #[arg(long, default_value = "us")]
    _country: String,
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
    acc
}

fn extract_app_id_from_link(link: &str) -> Option<String> {
    // Expect .../id<digits>[?query]
    if let Some(idx) = link.rfind("/id") {
        let mut s = &link[idx + 3..];
        if let Some(q) = s.find('?') { s = &s[..q]; }
        let digits: String = s.chars().take_while(|c| c.is_ascii_digit()).collect();
        if !digits.is_empty() { return Some(digits); }
    }
    None
}

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    // Read config
    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args.db_url.clone().or_else(|| cfg.as_ref().map(|c| c.general.db_url.clone()))
        .expect("db_url must be provided via --db-url or config.general.db_url");
    let cleanapp_api_url = args.cleanapp_api_url.clone().or_else(|| cfg.as_ref().map(|c| c.general.cleanapp_api_url.clone()))
        .expect("cleanapp_api_url must be provided via --cleanapp_api_url or config.general.cleanapp_api_url");
    let bot_user_id = args.bot_user_id.clone().or_else(|| cfg.as_ref().map(|c| c.general.bot_user_id.clone()))
        .expect("bot_user_id must be provided via --bot_user_id or config.general.bot_user_id");

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    let mut conn = pool.get_conn().await?;

    let rows: Vec<(String, String, String, String, String)> = if args.limit_rows == 0 {
        conn.exec("SELECT post_id, platform, url, content, DATE_FORMAT(post_timestamp, '%Y-%m-%d %H:%i:%s') FROM social_posts WHERE submitted_to_cleanapp=false ORDER BY post_timestamp ASC", ()).await?
    } else {
        conn.exec("SELECT post_id, platform, url, content, DATE_FORMAT(post_timestamp, '%Y-%m-%d %H:%i:%s') FROM social_posts WHERE submitted_to_cleanapp=false ORDER BY post_timestamp ASC LIMIT ?", (args.limit_rows,)).await?
    };

    drop(conn);

    info!("submitter: picked {} rows", rows.len());

    let total = rows.len();
    let client = reqwest::Client::builder().user_agent("news-indexer/0.1").timeout(StdDuration::from_secs(30)).build()?;
    let sem = Arc::new(Semaphore::new(args.concurrency));
    let pool_arc = Arc::new(pool);

    let mut started = 0usize;
    let mut handles = Vec::with_capacity(total);

    for (post_id, platform, url, content, _ts) in rows {
        let permit = sem.clone().acquire_owned().await?;
        let http = client.clone();
        let api = cleanapp_api_url.clone();
        let bot = bot_user_id.clone();
        let pool_clone = pool_arc.clone();
        let link = url.clone();
        let handle = tokio::spawn(async move {
            let _p = permit;
            // Extract app id and lookup app name
            let mut app_name = String::new();
            if let Some(app_id) = extract_app_id_from_link(&link) {
                if let Ok(mut c) = pool_clone.get_conn().await {
                    if let Ok(Some(name)) = c.exec_first::<String, _, _>(
                        "SELECT name FROM indexer_appstore_apps WHERE app_id = ?",
                        (app_id,),
                    ).await { app_name = name; }
                }
            }
            // content format is "title: body" as saved by fetcher
            let mut parts = content.splitn(2, ": ");
            let title = parts.next().unwrap_or("");
            let body = parts.next().unwrap_or("");
            let desc256 = truncate_utf8_by_bytes(body, 256);
            // Dig:AppStore:<appname>:<link>:<title>:<desc256>
            let annotation = format!("Dig:AppStore:{}:{}:{}:{}", app_name, link, title, desc256);
            let payload = json!({
                "version": "2.0",
                "id": bot,
                "latitude": 0.0,
                "longitude": 0.0,
                "x": 0.0,
                "y": 0.0,
                "image": "",
                "action_id": "",
                "annotation": annotation,
            });
            let res = http.post(&api).json(&payload).send().await;
            match res {
                Ok(resp) if resp.status().is_success() => {
                    let js: serde_json::Value = resp.json().await.unwrap_or_else(|_| json!({"seq": null}));
                    let seq = js["seq"].as_i64();
                    if let Ok(mut c) = pool_clone.get_conn().await {
                        let _ = c.exec_drop(
                            "UPDATE social_posts SET submitted_to_cleanapp=true, cleanapp_report_seq=:seq WHERE post_id=:post_id AND platform=:platform",
                            params!{"seq" => seq, "post_id" => &post_id, "platform" => &platform}
                        ).await;
                    }
                    info!("submitter: submitted {}:{}", platform, post_id);
                }
                Ok(resp) => {
                    error!("submitter: http {} for {}:{}", resp.status(), platform, post_id);
                }
                Err(e) => {
                    error!("submitter: error {} for {}:{}", e, platform, post_id);
                }
            }
        });
        handles.push(handle);
        started += 1;
        if started % 20 == 0 || started == total { info!("submitter progress: submitted_started={}/{} remaining={}", started, total, total - started); }
    }

    for h in handles { let _ = h.await; }

    Ok(())
}
