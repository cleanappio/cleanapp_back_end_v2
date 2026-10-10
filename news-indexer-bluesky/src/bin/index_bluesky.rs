use anyhow::{Context, Result};
use clap::Parser;
use log::{info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::Deserialize;
use serde_json::Value as JsonValue;
use std::collections::HashMap;
use std::time::Duration as StdDuration;
use tokio::time::sleep;

#[path = "../indexer_bluesky_schema.rs"]
mod indexer_bluesky_schema;
#[path = "../media.rs"]
mod media;
#[path = "../selection.rs"]
mod selection;

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
    #[arg(
        long,
        env = "BSKY_SEARCH_QUERIES",
        default_value = selection::SEARCH_QUERIES
    )]
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
        .or_else(|| {
            cfg.as_ref()
                .and_then(|c| c.general.as_ref().map(|g| g.db_url.clone()))
        })
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
        // Search cursors page backwards; never carry them into the next poll.
        // Each query receives the same page budget, always newest first.
        let mut next_cursor: Option<String> = None;
        let mut author_counts: HashMap<String, usize> = HashMap::new();
        let since =
            (chrono::Utc::now() - chrono::Duration::hours(selection::MAX_AGE_HOURS)).to_rfc3339();
        let mut pages = 0usize;

        loop {
            if pages >= args.pages_per_run {
                break;
            }
            pages += 1;

            let result =
                match search_posts(client, &access_token, query, next_cursor.as_deref(), &since)
                    .await
                {
                    Ok(result) => result,
                    Err(err) => {
                        warn!("query '{}': {}; continuing with other queries", query, err);
                        break;
                    }
                };

            if result.posts.is_empty() {
                info!("query '{}': no posts in page", query);
                break;
            }

            info!(
                "query '{}': {} posts in page {}",
                query,
                result.posts.len(),
                pages
            );

            for post in result.posts.iter() {
                if !selection::fresh(post.record.created_at.as_deref(), chrono::Utc::now()) {
                    continue;
                }
                let has_images = post
                    .embed
                    .as_ref()
                    .is_some_and(|e| !selection::images(e).is_empty());
                if selection::kind(&post.record.text, has_images).is_none() {
                    continue;
                }
                let count = author_counts.entry(post.author.did.clone()).or_default();
                if *count >= 3 {
                    continue;
                }
                *count += 1;
                let exists: Option<u8> = conn
                    .exec_first(
                        "SELECT 1 FROM indexer_bluesky_post WHERE uri=?",
                        (&post.uri,),
                    )
                    .await?;
                if exists.is_some() {
                    continue;
                }
                // Check language (allow en, es, or unspecified)
                if let Some(ref langs) = post.record.langs {
                    if !langs.is_empty() {
                        let valid_lang = langs
                            .iter()
                            .any(|l| l.starts_with("en") || l.starts_with("es"));
                        if !valid_lang {
                            continue;
                        }
                    }
                }

                // Parse created_at
                let created_at_db = post
                    .record
                    .created_at
                    .as_ref()
                    .map(|s| s.replace('T', " ").chars().take(19).collect::<String>());

                let lang = post
                    .record
                    .langs
                    .as_ref()
                    .and_then(|l| l.first())
                    .cloned()
                    .unwrap_or_default();

                let stored_images = if let Some(ref embed) = post.embed {
                    match media::store_images(client, &mut conn, &post.uri, &post.author.did, embed)
                        .await
                    {
                        Ok(count) => count,
                        Err(e) => {
                            warn!("embed handling error for {}: {}", post.uri, e);
                            0
                        }
                    }
                } else {
                    0
                };
                if selection::kind(&post.record.text, has_images)
                    == Some(selection::IssueKind::Physical)
                    && stored_images == 0
                {
                    continue;
                }
                // Upsert post
                conn.exec_drop(
                    r#"INSERT INTO indexer_bluesky_post
                       (uri, cid, author_did, author_handle, text, created_at, lang, raw)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                       ON DUPLICATE KEY UPDATE indexed_at = NOW()"#,
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
            }

            next_cursor = result.cursor.clone();
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
    since: &str,
) -> Result<SearchPostsResponse> {
    let mut url = format!(
        "https://bsky.social/xrpc/app.bsky.feed.searchPosts?q={}&limit=50&sort=latest&since={}",
        urlencoding::encode(query),
        urlencoding::encode(since)
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
