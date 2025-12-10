use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::Value as JsonValue;
use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_bluesky_schema.rs"]
mod indexer_bluesky_schema;

#[derive(Parser, Debug, Clone)]
struct Args {
    #[arg(long, default_value = "config.toml")]
    config_path: String,
    #[arg(long, env = "DB_URL")]
    db_url: Option<String>,
    #[arg(long, env = "BSKY_IDENTIFIER", default_value = "trashcash.bsky.social")]
    identifier: String,
    #[arg(long, env = "BSKY_APP_PASSWORD")]
    app_password: Option<String>,
    #[arg(long, env = "BSKY_INTERVAL_SECS", default_value_t = 3600)]
    interval_secs: u64,
    #[arg(long, env = "BSKY_PAGES_PER_RUN", default_value_t = 3)]
    pages_per_run: usize,
    #[arg(long, env = "BSKY_SEARCH_QUERIES", default_value = "fatal bug,app crash,horrible UX,broken feature,keeps crashing,feature request,missing dark mode,battery drain,laggy,freezes,login broken,sync fails,unusable,showstopper bug")]
    search_queries: String,
}

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: Option<GeneralConfig>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig {
    db_url: String,
}

// Bluesky session
#[derive(Deserialize, Debug)]
struct CreateSessionResponse {
    #[serde(rename = "accessJwt")]
    access_jwt: String,
    #[serde(rename = "did")]
    _did: String,
}

// Search posts response
#[derive(Deserialize, Debug)]
struct SearchPostsResponse {
    posts: Vec<PostView>,
    cursor: Option<String>,
}

#[derive(Deserialize, serde::Serialize, Debug, Clone)]
struct PostView {
    uri: String,
    cid: String,
    author: Author,
    record: Record,
    #[serde(rename = "indexedAt")]
    indexed_at: Option<String>,
    embed: Option<JsonValue>,
}

#[derive(Deserialize, serde::Serialize, Debug, Clone)]
struct Author {
    did: String,
    handle: String,
}

#[derive(Deserialize, serde::Serialize, Debug, Clone)]
struct Record {
    #[serde(default)]
    text: String,
    #[serde(rename = "createdAt")]
    created_at: Option<String>,
    langs: Option<Vec<String>>,
}

// Negative keywords to filter out noise
const NEGATIVE_KEYWORDS: &[&str] = &[
    "giveaway",
    "promo",
    "nft",
    "crypto airdrop",
    "follow for follow",
    "f4f",
    "follow back",
    "followback",
];

#[tokio::main]
async fn main() -> Result<()> {
    env_logger::init();
    let args = Args::parse();

    if args.interval_secs == 0 {
        info!("index_bluesky disabled by option: BSKY_INTERVAL_SECS=0; exiting");
        return Ok(());
    }

    let cfg: Option<Config> = match std::fs::read_to_string(&args.config_path) {
        Ok(s) => toml::from_str(&s).ok(),
        Err(_) => None,
    };

    let db_url = args
        .db_url
        .clone()
        .or_else(|| cfg.as_ref().and_then(|c| c.general.as_ref().map(|g| g.db_url.clone())))
        .context("db_url must be provided via --db-url or DB_URL")?;

    let app_password = args
        .app_password
        .clone()
        .context("app_password must be provided via BSKY_APP_PASSWORD")?;

    let queries: Vec<String> = args
        .search_queries
        .split(',')
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect();

    info!(
        "index_bluesky start identifier={} queries={} pages_per_run={} interval={}s",
        args.identifier,
        queries.len(),
        args.pages_per_run,
        args.interval_secs
    );

    let pool = Pool::new(mysql_async::Opts::from_url(&db_url)?);
    indexer_bluesky_schema::ensure_bluesky_tables(&pool).await?;

    let client = reqwest::Client::builder()
        .timeout(StdDuration::from_secs(30))
        .build()?;

    loop {
        if let Err(e) = run_once(&pool, &client, &args, &app_password, &queries).await {
            warn!("run_once error: {e}");
        }
        sleep(StdDuration::from_secs(args.interval_secs)).await;
    }
}

async fn run_once(
    pool: &Pool,
    client: &reqwest::Client,
    args: &Args,
    app_password: &str,
    queries: &[String],
) -> Result<()> {
    // Authenticate with Bluesky
    let access_token = authenticate(client, &args.identifier, app_password).await?;
    info!("authenticated with Bluesky as {}", args.identifier);

    let mut conn = pool.get_conn().await?;
    let mut total_new = 0usize;

    for query in queries {
        let tag_key = format!("search:{}", query.to_lowercase());
        
        // Load cursor for this query
        let cursor: Option<String> = conn
            .exec_first(
                "SELECT cursor_value FROM indexer_bluesky_cursor WHERE query_tag = ?",
                (tag_key.clone(),),
            )
            .await?;

        let mut next_cursor = cursor;
        let mut pages = 0usize;

        loop {
            if pages >= args.pages_per_run {
                break;
            }
            pages += 1;

            let result = search_posts(client, &access_token, query, next_cursor.as_deref()).await?;
            
            if result.posts.is_empty() {
                info!("query '{}': no posts in page", query);
                break;
            }

            info!("query '{}': {} posts in page {}", query, result.posts.len(), pages);

            for post in result.posts.iter() {
                // Skip if contains negative keywords
                let text_lower = post.record.text.to_lowercase();
                if NEGATIVE_KEYWORDS.iter().any(|kw| text_lower.contains(kw)) {
                    continue;
                }

                // Check language (allow en, es, or unspecified)
                if let Some(ref langs) = post.record.langs {
                    if !langs.is_empty() {
                        let valid_lang = langs.iter().any(|l| l.starts_with("en") || l.starts_with("es"));
                        if !valid_lang {
                            continue;
                        }
                    }
                }

                // Parse created_at
                let created_at_db = post.record.created_at.as_ref().map(|s| {
                    s.replace('T', " ")
                        .chars()
                        .take(19)
                        .collect::<String>()
                });

                let lang = post
                    .record
                    .langs
                    .as_ref()
                    .and_then(|l| l.first())
                    .cloned()
                    .unwrap_or_default();

                // Upsert post
                conn.exec_drop(
                    r#"INSERT INTO indexer_bluesky_post
                       (uri, cid, author_did, author_handle, text, created_at, lang, raw)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                       ON DUPLICATE KEY UPDATE updated_at = NOW()"#,
                    (
                        post.uri.clone(),
                        post.cid.clone(),
                        post.author.did.clone(),
                        post.author.handle.clone(),
                        post.record.text.clone(),
                        created_at_db,
                        lang,
                        serde_json::to_string(&post).unwrap_or("{}".into()),
                    ),
                )
                .await?;

                total_new += 1;

                // Handle embedded images
                if let Some(ref embed) = post.embed {
                    if let Err(e) = handle_embed(client, &mut conn, &post.uri, embed).await {
                        warn!("embed handling error for {}: {}", post.uri, e);
                    }
                }
            }

            // Update cursor
            next_cursor = result.cursor.clone();
            if let Some(ref c) = result.cursor {
                conn.exec_drop(
                    r#"INSERT INTO indexer_bluesky_cursor (query_tag, cursor_value)
                       VALUES (?, ?)
                       ON DUPLICATE KEY UPDATE cursor_value = VALUES(cursor_value), updated_at = NOW()"#,
                    (tag_key.clone(), c.clone()),
                )
                .await?;
            }

            if result.cursor.is_none() {
                break;
            }

            // Rate limiting
            sleep(StdDuration::from_millis(500)).await;
        }
    }

    info!("index_bluesky completed: {} new posts indexed", total_new);
    Ok(())
}

async fn authenticate(
    client: &reqwest::Client,
    identifier: &str,
    app_password: &str,
) -> Result<String> {
    let url = "https://bsky.social/xrpc/com.atproto.server.createSession";
    let body = serde_json::json!({
        "identifier": identifier,
        "password": app_password
    });

    let resp = client.post(url).json(&body).send().await?;
    
    if !resp.status().is_success() {
        let status = resp.status();
        let text = resp.text().await.unwrap_or_default();
        anyhow::bail!("Bluesky auth failed {}: {}", status, text);
    }

    let session: CreateSessionResponse = resp.json().await?;
    Ok(session.access_jwt)
}

async fn search_posts(
    client: &reqwest::Client,
    access_token: &str,
    query: &str,
    cursor: Option<&str>,
) -> Result<SearchPostsResponse> {
    let mut url = format!(
        "https://bsky.social/xrpc/app.bsky.feed.searchPosts?q={}&limit=50",
        urlencoding::encode(query)
    );
    
    if let Some(c) = cursor {
        url.push_str(&format!("&cursor={}", urlencoding::encode(c)));
    }

    let resp = client
        .get(&url)
        .header("Authorization", format!("Bearer {}", access_token))
        .send()
        .await?;

    if resp.status() == reqwest::StatusCode::TOO_MANY_REQUESTS {
        warn!("Bluesky rate limited; will retry next cycle");
        return Ok(SearchPostsResponse {
            posts: vec![],
            cursor: None,
        });
    }

    if !resp.status().is_success() {
        let status = resp.status();
        let text = resp.text().await.unwrap_or_default();
        anyhow::bail!("Bluesky search failed {}: {}", status, text);
    }

    let result: SearchPostsResponse = resp.json().await?;
    Ok(result)
}

async fn handle_embed(
    client: &reqwest::Client,
    conn: &mut mysql_async::Conn,
    post_uri: &str,
    embed: &JsonValue,
) -> Result<()> {
    // Handle images embed
    let images = embed
        .get("images")
        .or_else(|| embed.get("$type").and_then(|t| t.as_str()).filter(|t| *t == "app.bsky.embed.images#view").and_then(|_| embed.get("images")))
        .and_then(|i| i.as_array());

    if let Some(images) = images {
        let mut position = 0;
        for img in images {
            // Get fullsize URL
            let url = img
                .get("fullsize")
                .or_else(|| img.get("thumb"))
                .and_then(|u| u.as_str());

            if let Some(img_url) = url {
                // Download image
                match client.get(img_url).send().await {
                    Ok(resp) if resp.status().is_success() => {
                        let bytes = resp.bytes().await?;
                        if !bytes.is_empty() {
                            let mut hasher = Sha256::new();
                            hasher.update(&bytes);
                            let digest = hasher.finalize().to_vec();

                            // Insert blob (shared table with Twitter)
                            conn.exec_drop(
                                "INSERT IGNORE INTO indexer_media_blob (sha256, data) VALUES (?, ?)",
                                (digest.clone(), bytes.as_ref()),
                            )
                            .await?;

                            // Insert media reference
                            conn.exec_drop(
                                r#"INSERT INTO indexer_bluesky_media (post_uri, position, sha256, url)
                                   VALUES (?, ?, ?, ?)
                                   ON DUPLICATE KEY UPDATE sha256=VALUES(sha256), url=VALUES(url)"#,
                                (post_uri, position, digest, img_url),
                            )
                            .await?;

                            position += 1;
                        }
                    }
                    Ok(resp) => {
                        warn!("image download failed {}: {}", img_url, resp.status());
                    }
                    Err(e) => {
                        warn!("image download error {}: {}", img_url, e);
                    }
                }
            }
        }
    }

    Ok(())
}
