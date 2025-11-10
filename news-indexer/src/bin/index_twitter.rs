use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::{Deserialize, Serialize};
use serde_json::Value as JsonValue;
use sha2::{Digest, Sha256};
use std::collections::{HashMap, HashSet};
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_twitter_schema.rs"]
mod indexer_twitter_schema;

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")] config_path: String,
    #[arg(long, env = "DB_URL")] db_url: Option<String>,
    #[arg(long, env = "TWITTER_BEARER_TOKEN")] bearer_token: Option<String>,
    #[arg(long, env = "TWITTER_TAGS", default_value = "cleanapp")] tags: String,
    #[arg(long, env = "TWITTER_MENTIONS", default_value = "CleanApp")] mentions: String,
    #[arg(long, env = "TWITTER_INTERVAL_SECS", default_value_t = 3600)] interval_secs: u64,
    #[arg(long, env = "TWITTER_PAGES_PER_RUN", default_value_t = 3)] pages_per_run: usize,
}

#[derive(Deserialize, Clone, Debug)]
struct Config { general: Option<GeneralConfig> }
#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig { db_url: String }

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    // Disable component if interval is set to 0
    if args.interval_secs == 0 {
        info!("index_twitter disabled by option: TWITTER_INTERVAL_SECS=0; exiting");
        return Ok(());
    }

    // Load optional config
    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args
        .db_url
        .clone()
        .or_else(|| cfg.as_ref().map(|c| c.general.as_ref().map(|g| g.db_url.clone())).flatten())
        .context("db_url must be provided via --db-url or config.general.db_url")?;
    let bearer = args
        .bearer_token
        .clone()
        .context("bearer token must be provided via --bearer-token or TWITTER_BEARER_TOKEN")?;

    info!(
        "index_twitter start tags={} mentions={} pages_per_run={} interval={}s",
        args.tags, args.mentions, args.pages_per_run, args.interval_secs
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_twitter_schema::ensure_twitter_tables(&pool).await?;

    let client = reqwest::Client::builder()
        .timeout(StdDuration::from_secs(30))
        .build()?;

    loop {
        if let Err(e) = run_once(&pool, &client, &bearer, &args).await {
            warn!("run_once error: {e}");
        }
        sleep(StdDuration::from_secs(args.interval_secs)).await;
    }
}

async fn run_once(pool: &Pool, client: &reqwest::Client, bearer: &str, args: &Args) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    let tag_key = canonical_tag_key(&args.tags, &args.mentions);
    // Load since_id
    let since_id: Option<i64> = conn
        .exec_first(
            "SELECT since_id FROM indexer_twitter_cursor WHERE tag = ?",
            (tag_key.clone(),),
        )
        .await?;

    if let Some(sid) = since_id { info!("using since_id={}", sid); }

    let mut newest_id_seen: Option<i64> = since_id;
    let mut next_token: Option<String> = None;
    let mut pages = 0usize;
    loop {
        if pages >= args.pages_per_run { break; }
        pages += 1;
        let url = build_recent_url(&args, since_id.as_ref().map(|x| x.to_string()), next_token.as_ref());
        let req = client
            .get(url)
            .bearer_auth(bearer)
            .header("User-Agent", "cleanapp-news-indexer/1.0");
        let resp = req.send().await?;
        if resp.status() == reqwest::StatusCode::TOO_MANY_REQUESTS {
            warn!("twitter 429; backing off");
            sleep(StdDuration::from_secs(60)).await;
            break;
        }
        if !resp.status().is_success() {
            let st = resp.status();
            let body = resp.text().await.unwrap_or_default();
            anyhow::bail!("twitter error {}: {}", st, body);
        }
        let v: JsonValue = resp.json().await?;
        // Process
        let data = v.get("data").and_then(|d| d.as_array()).cloned().unwrap_or_default();
        if data.is_empty() {
            info!("no tweets in page");
        } else {
            let mut photos_downloaded: usize = 0;
            info!("tweets in page: {}", data.len());
            // We'll accumulate per-tweet media stats below
            // (counter updated inside the loop)
        }
        let includes = v.get("includes").cloned().unwrap_or(JsonValue::Null);
        let users_by_id = index_users(&includes);
        let media_by_key = index_media(&includes);

        // track newest id
        if let Some(meta) = v.get("meta") {
            if let Some(newest) = meta.get("newest_id").and_then(|x| x.as_str()) {
                if let Ok(id) = newest.parse::<i64>() {
                    if newest_id_seen.map(|cur| id > cur).unwrap_or(true) { newest_id_seen = Some(id); }
                }
            }
            next_token = meta.get("next_token").and_then(|x| x.as_str()).map(|s| s.to_string());
        }

        let mut photos_downloaded_page: usize = 0;
        for (pos, tw) in data.iter().enumerate() {
            if let Some(tid) = tw.get("id").and_then(|x| x.as_str()).and_then(|s| s.parse::<i64>().ok()) {
                let created_at_db = tw
                    .get("created_at")
                    .and_then(|x| x.as_str())
                    .map(|s| s.replace('T', " ").trim_end_matches('Z').to_string());
                let author_id = tw.get("author_id").and_then(|x| x.as_str()).and_then(|s| s.parse::<i64>().ok());
                let username = author_id
                    .and_then(|aid| users_by_id.get(&aid).cloned());
                let lang = tw.get("lang").and_then(|x| x.as_str()).unwrap_or("").to_string();
                let text = tw.get("text").and_then(|x| x.as_str()).unwrap_or("").to_string();
                let url = username
                    .as_ref()
                    .map(|u| format!("https://twitter.com/{}/status/{}", u, tid))
                    .unwrap_or_default();
                let public_metrics = tw.get("public_metrics").cloned().unwrap_or(JsonValue::Null);
                let entities = tw.get("entities").cloned().unwrap_or(JsonValue::Null);
                let media_keys: Vec<String> = tw
                    .get("attachments")
                    .and_then(|a| a.get("media_keys"))
                    .and_then(|mk| mk.as_array())
                    .map(|arr| arr.iter().filter_map(|x| x.as_str().map(|s| s.to_string())).collect())
                    .unwrap_or_default();

                // Upsert tweet
                conn.exec_drop(
                    r#"INSERT INTO indexer_twitter_tweet
                       (tweet_id, created_at, author_id, username, lang, text, url, public_metrics, entities, media_keys, raw)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, JSON_ARRAY(?), ?)
                       ON DUPLICATE KEY UPDATE updated_at = NOW()"#,
                    (
                        tid,
                        created_at_db.clone(),
                        author_id,
                        username.clone().unwrap_or_default(),
                        lang,
                        text.clone(),
                        url.clone(),
                        serde_json::to_string(&public_metrics).unwrap_or("null".into()),
                        serde_json::to_string(&entities).unwrap_or("null".into()),
                        media_keys.join(","),
                        serde_json::to_string(&tw).unwrap_or("null".into()),
                    ),
                )
                .await?;

                // Media handling: photos only; download and store blob deduped
                if !media_keys.is_empty() {
                    let mut used_hashes: HashSet<Vec<u8>> = HashSet::new();
                    for (i, k) in media_keys.iter().enumerate() {
                        if let Some(m) = media_by_key.get(k) {
                            let mtype = m.get("type").and_then(|x| x.as_str()).unwrap_or("");
                            if mtype != "photo" { continue; }
                            let url_opt = m.get("url").and_then(|x| x.as_str());
                            if let Some(murl) = url_opt {
                                match client.get(murl).send().await {
                                    Ok(resp) => {
                                        if resp.status().is_success() {
                                            let bytes = resp.bytes().await.unwrap_or_default();
                                            if !bytes.is_empty() {
                                                let mut hasher = Sha256::new();
                                                hasher.update(&bytes);
                                                let digest = hasher.finalize();
                                                let digest_vec = digest.to_vec();
                                                if used_hashes.insert(digest_vec.clone()) {
                                                    // insert blob if not exists
                                                    conn.exec_drop(
                                                        "INSERT IGNORE INTO indexer_media_blob (sha256, data) VALUES (?, ?)",
                                                        (digest_vec.clone(), bytes.as_ref()),
                                                    ).await?;
                                                }
                                                // upsert mapping
                                                conn.exec_drop(
                                                    r#"INSERT INTO indexer_twitter_media
                                                        (tweet_id, media_key, position, type, sha256, url)
                                                      VALUES (?, ?, ?, 'photo', ?, ?)
                                                      ON DUPLICATE KEY UPDATE sha256=VALUES(sha256), url=VALUES(url)"#,
                                                    (tid, k, i as i32, digest_vec, murl),
                                                ).await?;
                                                photos_downloaded_page += 1;
                                            }
                                        }
                                    }
                                    Err(e) => {
                                        warn!("media download failed {}: {}", murl, e);
                                    }
                                }
                            }
                        }
                    }
                }
            }
            // avoid hammering
            if pos % 20 == 0 { sleep(StdDuration::from_millis(50)).await; }
        }

        if !data.is_empty() {
            info!("processed page: tweets={} photos_saved={} next_token={:?}", data.len(), photos_downloaded_page, next_token);
        }

        if next_token.is_none() { break; }
    }

    if let Some(newest) = newest_id_seen {
        conn.exec_drop(
            r#"INSERT INTO indexer_twitter_cursor (tag, since_id) VALUES (?, ?)
               ON DUPLICATE KEY UPDATE since_id = GREATEST(COALESCE(since_id, 0), VALUES(since_id)), updated_at = NOW()"#,
            (tag_key, newest),
        )
        .await?;
        info!("updated cursor tag={} since_id={}", canonical_tag_key(&args.tags, &args.mentions), newest);
    }

    Ok(())
}

fn canonical_tag_key(tags: &str, mentions: &str) -> String {
    format!("tags:{}|mentions:{}", tags.trim().to_lowercase(), mentions.trim().to_lowercase())
}

fn build_recent_url(args: &Args, since_id: Option<String>, next_token: Option<&String>) -> String {
    // mentions: operator is not available on our plan; match literal @username instead
    let query = format!(
        "(#{tag} OR \"{tag}\" OR @{mention}) -is:retweet -is:quote -is:reply",
        tag = args.tags,
        mention = args.mentions
    );
    let mut url = format!(
        "https://api.twitter.com/2/tweets/search/recent?query={}&max_results=100&tweet.fields=created_at,lang,public_metrics,entities,attachments,author_id,possibly_sensitive&expansions=attachments.media_keys,author_id&user.fields=username,verified&media.fields=url,preview_image_url,alt_text,width,height,type",
        urlencoding::encode(&query)
    );
    if let Some(sid) = since_id { url.push_str(&format!("&since_id={}", sid)); }
    if let Some(nt) = next_token { url.push_str(&format!("&next_token={}", nt)); }
    url
}

fn index_users(includes: &JsonValue) -> HashMap<i64, String> {
    let mut map = HashMap::new();
    if let Some(users) = includes.get("users").and_then(|x| x.as_array()) {
        for u in users {
            if let (Some(id), Some(username)) = (
                u.get("id").and_then(|x| x.as_str()).and_then(|s| s.parse::<i64>().ok()),
                u.get("username").and_then(|x| x.as_str()),
            ) {
                map.insert(id, username.to_string());
            }
        }
    }
    map
}

fn index_media(includes: &JsonValue) -> HashMap<String, JsonValue> {
    let mut map = HashMap::new();
    if let Some(media) = includes.get("media").and_then(|x| x.as_array()) {
        for m in media {
            if let Some(k) = m.get("media_key").and_then(|x| x.as_str()) {
                map.insert(k.to_string(), m.clone());
            }
        }
    }
    map
}


