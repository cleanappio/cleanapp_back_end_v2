use anyhow::{Context, Result};
use clap::Parser;
use futures_util::{SinkExt, StreamExt};
use log::{debug, error, info, warn};
use mysql_async::prelude::*;
use mysql_async::Pool;
use serde::{Deserialize, Serialize};
use serde_json::Value as JsonValue;
use std::time::Duration;
use tokio::time::sleep;
use tokio_tungstenite::{connect_async, tungstenite::Message};

#[path = "../indexer_bluesky_schema.rs"]
mod indexer_bluesky_schema;

/// BlueskyNow: Real-time Jetstream firehose consumer for CleanApp
#[derive(Parser, Debug, Clone)]
#[command(author, version, about, long_about = None)]
struct Args {
    /// Path to config file
    #[arg(short, long, default_value = "config.toml")]
    config: String,

    /// Run once and exit (for testing)
    #[arg(short, long, default_value_t = false)]
    once: bool,
}

#[derive(Deserialize, Clone, Debug)]
struct Config {
    general: GeneralConfig,
    brands: Option<Vec<BrandConfig>>,
}

#[derive(Deserialize, Clone, Debug)]
struct GeneralConfig {
    db_url: String,
}

#[derive(Deserialize, Clone, Debug, Serialize)]
struct BrandConfig {
    id: String,
    display_name: String,
    aliases: Vec<String>,
    domains: Vec<String>,
    #[serde(default)]
    handle_patterns: Vec<String>,
    #[serde(default)]
    vertical: String,
}

// Jetstream event structures
#[derive(Deserialize, Debug)]
struct JetstreamEvent {
    did: String,
    #[serde(rename = "time_us")]
    time_us: u64,
    kind: String,
    commit: Option<JetstreamCommit>,
}

#[derive(Deserialize, Debug)]
struct JetstreamCommit {
    rev: String,
    operation: String,
    collection: String,
    rkey: String,
    record: Option<JsonValue>,
    cid: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
struct BlueskyPost {
    uri: String,
    cid: String,
    author_did: String,
    author_handle: Option<String>,
    text: String,
    links: Vec<String>,
    hashtags: Vec<String>,
    created_at: Option<String>,
    is_reply: bool,
    detected_brands: Vec<BrandMatch>,
    raw: JsonValue,
}

#[derive(Debug, Clone, Serialize)]
struct BrandMatch {
    brand_id: String,
    confidence: f32,
    match_type: String, // "alias", "domain", "handle"
}

const JETSTREAM_URL: &str = "wss://jetstream2.us-east.bsky.network/subscribe";
const WANTED_COLLECTIONS: &str = "app.bsky.feed.post";

// Default brand list - comprehensive list of major brands
fn default_brands() -> Vec<BrandConfig> {
    vec![
        // Transportation
        BrandConfig {
            id: "uber".into(),
            display_name: "Uber".into(),
            aliases: vec!["uber".into(), "uber eats".into(), "ubereats".into()],
            domains: vec!["uber.com".into(), "ubereats.com".into()],
            handle_patterns: vec!["uber".into()],
            vertical: "transportation".into(),
        },
        BrandConfig {
            id: "lyft".into(),
            display_name: "Lyft".into(),
            aliases: vec!["lyft".into()],
            domains: vec!["lyft.com".into()],
            handle_patterns: vec!["lyft".into()],
            vertical: "transportation".into(),
        },
        // Hospitality
        BrandConfig {
            id: "airbnb".into(),
            display_name: "Airbnb".into(),
            aliases: vec!["airbnb".into(), "air bnb".into()],
            domains: vec!["airbnb.com".into()],
            handle_patterns: vec!["airbnb".into()],
            vertical: "hospitality".into(),
        },
        BrandConfig {
            id: "booking".into(),
            display_name: "Booking.com".into(),
            aliases: vec!["booking.com".into(), "booking".into()],
            domains: vec!["booking.com".into()],
            handle_patterns: vec!["booking".into()],
            vertical: "hospitality".into(),
        },
        // Streaming
        BrandConfig {
            id: "spotify".into(),
            display_name: "Spotify".into(),
            aliases: vec!["spotify".into()],
            domains: vec!["spotify.com".into(), "open.spotify.com".into()],
            handle_patterns: vec!["spotify".into()],
            vertical: "entertainment".into(),
        },
        BrandConfig {
            id: "netflix".into(),
            display_name: "Netflix".into(),
            aliases: vec!["netflix".into()],
            domains: vec!["netflix.com".into()],
            handle_patterns: vec!["netflix".into()],
            vertical: "entertainment".into(),
        },
        BrandConfig {
            id: "youtube".into(),
            display_name: "YouTube".into(),
            aliases: vec!["youtube".into(), "yt ".into()],
            domains: vec!["youtube.com".into(), "youtu.be".into()],
            handle_patterns: vec!["youtube".into()],
            vertical: "entertainment".into(),
        },
        // E-commerce
        BrandConfig {
            id: "amazon".into(),
            display_name: "Amazon".into(),
            aliases: vec!["amazon".into(), "aws ".into(), "prime video".into()],
            domains: vec!["amazon.com".into(), "amzn.to".into(), "aws.amazon.com".into()],
            handle_patterns: vec!["amazon".into()],
            vertical: "ecommerce".into(),
        },
        BrandConfig {
            id: "ebay".into(),
            display_name: "eBay".into(),
            aliases: vec!["ebay".into()],
            domains: vec!["ebay.com".into()],
            handle_patterns: vec!["ebay".into()],
            vertical: "ecommerce".into(),
        },
        // Social / Tech
        BrandConfig {
            id: "discord".into(),
            display_name: "Discord".into(),
            aliases: vec!["discord".into()],
            domains: vec!["discord.com".into(), "discord.gg".into()],
            handle_patterns: vec!["discord".into()],
            vertical: "social".into(),
        },
        BrandConfig {
            id: "twitter".into(),
            display_name: "Twitter/X".into(),
            aliases: vec!["twitter".into(), " x ".into(), "x.com".into()],
            domains: vec!["twitter.com".into(), "x.com".into()],
            handle_patterns: vec!["twitter".into()],
            vertical: "social".into(),
        },
        BrandConfig {
            id: "meta".into(),
            display_name: "Meta".into(),
            aliases: vec!["facebook".into(), "instagram".into(), "meta ".into(), "whatsapp".into()],
            domains: vec!["facebook.com".into(), "instagram.com".into(), "meta.com".into(), "whatsapp.com".into()],
            handle_patterns: vec!["facebook".into(), "instagram".into(), "meta".into()],
            vertical: "social".into(),
        },
        BrandConfig {
            id: "tiktok".into(),
            display_name: "TikTok".into(),
            aliases: vec!["tiktok".into(), "tik tok".into()],
            domains: vec!["tiktok.com".into()],
            handle_patterns: vec!["tiktok".into()],
            vertical: "social".into(),
        },
        // Finance
        BrandConfig {
            id: "paypal".into(),
            display_name: "PayPal".into(),
            aliases: vec!["paypal".into(), "venmo".into()],
            domains: vec!["paypal.com".into(), "venmo.com".into()],
            handle_patterns: vec!["paypal".into(), "venmo".into()],
            vertical: "finance".into(),
        },
        BrandConfig {
            id: "stripe".into(),
            display_name: "Stripe".into(),
            aliases: vec!["stripe".into()],
            domains: vec!["stripe.com".into()],
            handle_patterns: vec!["stripe".into()],
            vertical: "finance".into(),
        },
        // Gaming
        BrandConfig {
            id: "steam".into(),
            display_name: "Steam".into(),
            aliases: vec!["steam".into(), "valve ".into()],
            domains: vec!["steampowered.com".into(), "store.steampowered.com".into()],
            handle_patterns: vec!["steam".into()],
            vertical: "gaming".into(),
        },
        BrandConfig {
            id: "playstation".into(),
            display_name: "PlayStation".into(),
            aliases: vec!["playstation".into(), "psn ".into(), "ps5 ".into(), "ps4 ".into()],
            domains: vec!["playstation.com".into()],
            handle_patterns: vec!["playstation".into()],
            vertical: "gaming".into(),
        },
        BrandConfig {
            id: "xbox".into(),
            display_name: "Xbox".into(),
            aliases: vec!["xbox".into(), "microsoft gaming".into()],
            domains: vec!["xbox.com".into()],
            handle_patterns: vec!["xbox".into()],
            vertical: "gaming".into(),
        },
        // Food Delivery
        BrandConfig {
            id: "doordash".into(),
            display_name: "DoorDash".into(),
            aliases: vec!["doordash".into(), "door dash".into()],
            domains: vec!["doordash.com".into()],
            handle_patterns: vec!["doordash".into()],
            vertical: "food_delivery".into(),
        },
        BrandConfig {
            id: "grubhub".into(),
            display_name: "Grubhub".into(),
            aliases: vec!["grubhub".into()],
            domains: vec!["grubhub.com".into()],
            handle_patterns: vec!["grubhub".into()],
            vertical: "food_delivery".into(),
        },
    ]
}

// Negative keywords to filter noise and spam
const NEGATIVE_KEYWORDS: &[&str] = &[
    // Spam/Promo
    "giveaway", "promo", "nft", "crypto airdrop",
    "follow for follow", "f4f", "follow back", "followback",
    // YouTube/Video spam (common pattern: "Watch the latest")
    "watch the latest", "watch latest", "don't miss this",
    "breaking news:", "latest updates:",
    "youtu.be", "youtube.com/watch",
    // Stock/Crypto spam
    "nse/bse", "stock updates", "shares updates", "trading signal",
    "buy now", "pump", "to the moon",
    // Political/News (not product-related)
    "vote for", "election", "politician",
    // Other noise
    "astrology", "horoscope", "zodiac",
    "subscribe to my", "check out my",
];

// Product/Service/Tech complaint keywords - focused but comprehensive
// These are designed to catch CleanApp-relevant issues about apps, services, products, and brands
const COMPLAINT_KEYWORDS: &[&str] = &[
    // === HIGH-SIGNAL SINGLE WORDS (catch more, filter later) ===
    "crash", "crashed", "crashes", "crashing",
    "bug", "bugs", "buggy",
    "broken", "broke",
    "glitch", "glitchy", "glitches",
    "sucks", "suck",
    "terrible", "awful", "horrible", "worst",
    "scam", "scammed", "scammer",
    "refund", "refunds",
    "hacked", "hack",
    "banned", "suspended",
    "outage", "down",
    "unusable",
    "overcharged", "charged",
    "unsubscribe",
    
    // === App/Software Issues ===
    "app crash", "app crashed", "app crashes", "app crashing",
    "keeps crashing", "constantly crashes", "always crashes",
    "app freeze", "app froze", "app freezes", "app frozen",
    "app bug", "buggy app", "so buggy", "full of bugs",
    "glitch", "glitchy", "glitches",
    "app broken", "broken app", "feature broken",
    "doesn't work", "doesnt work", "not working", "stopped working",
    "won't open", "wont open", "won't load", "wont load",
    "can't open", "cant open", "won't start", "wont start",
    "app down", "site down", "service down", "server down", "servers down",
    "outage", "down for", "been down",
    
    // === Login/Account Issues ===
    "can't login", "cant login", "can't log in", "cant log in",
    "login failed", "login error", "login issue", "login problem",
    "can't sign in", "cant sign in", "won't let me log",
    "locked out", "account locked", "got locked out",
    "account suspended", "account banned", "account disabled",
    "account hacked", "got hacked", "been hacked", "someone hacked",
    "password reset", "reset my password", "forgot password",
    "2fa issue", "verification code", "verification failed",
    
    // === Billing/Payment Issues ===
    "charged me", "double charged", "overcharged", "wrongly charged",
    "unauthorized charge", "mystery charge", "random charge",
    "want a refund", "need a refund", "give me a refund", "refund please",
    "where's my refund", "still waiting for refund", "no refund",
    "billing issue", "billing problem", "billing error",
    "subscription", "unsubscribe", "cancel subscription", "cancelled but still",
    "charged after cancel", "still being charged",
    "payment failed", "payment declined", "payment error",
    "won't process", "transaction failed",
    
    // === Customer Service ===
    "customer service", "customer support", "support team",
    "no response", "no reply", "won't respond", "wont respond",
    "support ticket", "waiting for support", "support is useless",
    "help desk", "contact support", "reached out to",
    "hours on hold", "on hold for", "waiting on hold",
    "chat support", "email support", "phone support",
    
    // === Product/Service Quality ===
    "terrible service", "awful service", "worst service",
    "terrible app", "awful app", "worst app",
    "terrible experience", "awful experience", "worst experience",
    "horrible ux", "bad ux", "terrible ui", "horrible design",
    "unusable", "barely usable", "completely unusable",
    "waste of money", "waste of time", "total waste",
    "scam", "scammed", "feels like a scam", "such a scam",
    "ripoff", "rip off", "rip-off",
    "false advertising", "misleading", "bait and switch",
    
    // === Delivery/Shipping Issues ===
    "never arrived", "didn't arrive", "hasnt arrived", "hasn't arrived",
    "wrong item", "wrong order", "wrong product",
    "missing item", "missing order", "order missing",
    "damaged", "arrived damaged", "came damaged",
    "late delivery", "delivery late", "still waiting for delivery",
    "lost package", "package lost", "lost my order",
    "tracking not working", "tracking shows",
    
    // === Booking/Reservation Issues ===
    "reservation cancelled", "reservation canceled", "booking cancelled",
    "booking canceled", "cancelled my booking", "canceled my reservation",
    "double booked", "overbooked", "no record of",
    "couldn't check in", "check in issue", "check-in problem",
    "room wasn't", "room wasnt", "not as advertised", "not as described",
    
    // === Feature Requests/Missing Functionality ===
    "feature request", "please add", "wish you had", "would be nice if",
    "missing feature", "need a feature", "no option to",
    "can't find", "cant find", "where is the", "how do i",
    "used to work", "worked before", "stopped working after update",
    "update broke", "after update", "since the update", "latest update",
    "bring back", "removed feature", "why did you remove",
    
    // === Safety/Trust Issues ===
    "data breach", "privacy concern", "privacy issue",
    "leaked my", "exposed my", "shared my data",
    "unsafe", "security issue", "security concern", "security flaw",
    "vulnerability", "not secure",
    "spam", "getting spam", "spam emails", "spam calls",
    "phishing", "fake email", "suspicious email",
    
    // === Driver/Rider/Host Issues (Rideshare/Delivery/Hospitality) ===
    "driver was", "driver didn't", "driver didnt", "driver never",
    "rider was", "passenger was",
    "host was", "host didn't", "host didnt", "host never",
    "rude driver", "rude host", "unprofessional",
    "unsafe driver", "dangerous driving", "felt unsafe",
    "wrong address", "went to wrong", "picked up wrong",
    "didn't show up", "didnt show up", "no show",
    "cancelled on me", "canceled on me",
];


#[tokio::main]
async fn main() -> Result<()> {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();

    let args = Args::parse();
    info!("BlueskyNow starting...");

    // Load config
    let config_str = std::fs::read_to_string(&args.config)
        .with_context(|| format!("Failed to read config file: {}", args.config))?;
    let config: Config = toml::from_str(&config_str)
        .with_context(|| "Failed to parse config file")?;

    // Connect to database
    let pool = Pool::new(config.general.db_url.as_str());
    info!("Database pool created");

    // Ensure tables exist
    indexer_bluesky_schema::ensure_bluesky_tables(&pool).await?;
    ensure_jetstream_cursor_table(&pool).await?;
    info!("Database tables verified");

    info!("BlueskyNow running in COMPREHENSIVE mode - all complaints will be ingested");

    if args.once {
        run_once(&pool).await?;
    } else {
        run_continuous(&pool).await?;
    }

    Ok(())
}

async fn ensure_jetstream_cursor_table(pool: &Pool) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    conn.query_drop(r#"
        CREATE TABLE IF NOT EXISTS indexer_bluesky_jetstream_cursor (
            id INT NOT NULL PRIMARY KEY DEFAULT 1,
            time_us BIGINT NOT NULL DEFAULT 0,
            updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    "#).await?;
    conn.query_drop(r#"
        INSERT IGNORE INTO indexer_bluesky_jetstream_cursor (id, time_us) VALUES (1, 0)
    "#).await?;
    Ok(())
}

async fn get_cursor(pool: &Pool) -> Result<u64> {
    let mut conn = pool.get_conn().await?;
    let result: Option<u64> = conn
        .query_first("SELECT time_us FROM indexer_bluesky_jetstream_cursor WHERE id = 1")
        .await?;
    Ok(result.unwrap_or(0))
}

async fn update_cursor(pool: &Pool, time_us: u64) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    conn.exec_drop(
        "UPDATE indexer_bluesky_jetstream_cursor SET time_us = ? WHERE id = 1",
        (time_us,)
    ).await?;
    Ok(())
}

async fn run_once(pool: &Pool) -> Result<()> {
    info!("Running once for testing...");
    let cursor = get_cursor(pool).await?;
    
    let url = if cursor > 0 {
        format!("{}?wantedCollections={}&cursor={}", JETSTREAM_URL, WANTED_COLLECTIONS, cursor)
    } else {
        format!("{}?wantedCollections={}", JETSTREAM_URL, WANTED_COLLECTIONS)
    };

    info!("Connecting to Jetstream: {}", url);
    let (ws_stream, _) = connect_async(&url).await?;
    let (_, mut read) = ws_stream.split();

    let mut count = 0;
    while let Some(msg) = read.next().await {
        match msg {
            Ok(Message::Text(text)) => {
                if let Err(e) = process_message(&text, pool).await {
                    warn!("Error processing message: {}", e);
                }
                count += 1;
                if count >= 100 {
                    info!("Processed 100 messages, exiting");
                    break;
                }
            }
            Ok(_) => {}
            Err(e) => {
                error!("WebSocket error: {}", e);
                break;
            }
        }
    }

    Ok(())
}

async fn run_continuous(pool: &Pool) -> Result<()> {
    let mut backoff_secs = 1u64;
    
    loop {
        let cursor = get_cursor(pool).await.unwrap_or(0);
        
        let url = if cursor > 0 {
            format!("{}?wantedCollections={}&cursor={}", JETSTREAM_URL, WANTED_COLLECTIONS, cursor)
        } else {
            format!("{}?wantedCollections={}", JETSTREAM_URL, WANTED_COLLECTIONS)
        };

        info!("Connecting to Jetstream (cursor: {})...", cursor);
        
        match connect_async(&url).await {
            Ok((ws_stream, _)) => {
                backoff_secs = 1; // Reset backoff on success
                let (_, mut read) = ws_stream.split();
                
                info!("Connected to Jetstream firehose (COMPREHENSIVE mode)");
                
                let mut message_count = 0u64;
                let mut match_count = 0u64;
                
                while let Some(msg) = read.next().await {
                    match msg {
                        Ok(Message::Text(text)) => {
                            match process_message(&text, pool).await {
                                Ok(matched) => {
                                    message_count += 1;
                                    if matched {
                                        match_count += 1;
                                    }
                                    if message_count % 10000 == 0 {
                                        info!(
                                            "Processed {} messages, {} complaints stored ({:.2}%)",
                                            message_count,
                                            match_count,
                                            (match_count as f64 / message_count as f64) * 100.0
                                        );
                                    }
                                }
                                Err(e) => {
                                    debug!("Error processing message: {}", e);
                                }
                            }
                        }
                        Ok(Message::Ping(_)) => {
                            debug!("Received ping");
                            // Pong is handled automatically by tungstenite
                        }
                        Ok(Message::Close(_)) => {
                            warn!("WebSocket closed by server");
                            break;
                        }
                        Ok(_) => {}
                        Err(e) => {
                            error!("WebSocket error: {}", e);
                            break;
                        }
                    }
                }
            }
            Err(e) => {
                error!("Failed to connect: {}", e);
            }
        }

        // Reconnect with exponential backoff
        warn!("Connection lost, reconnecting in {} seconds...", backoff_secs);
        sleep(Duration::from_secs(backoff_secs)).await;
        backoff_secs = (backoff_secs * 2).min(60);
    }
}

async fn process_message(raw: &str, pool: &Pool) -> Result<bool> {
    let event: JetstreamEvent = serde_json::from_str(raw)?;
    
    // Update cursor
    if event.time_us > 0 {
        // Only update cursor periodically to reduce DB writes
        static CURSOR_UPDATE_INTERVAL: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
        let last = CURSOR_UPDATE_INTERVAL.load(std::sync::atomic::Ordering::Relaxed);
        if event.time_us - last > 1_000_000 { // Update every ~1 second
            CURSOR_UPDATE_INTERVAL.store(event.time_us, std::sync::atomic::Ordering::Relaxed);
            update_cursor(pool, event.time_us).await?;
        }
    }

    // Only process commit events
    if event.kind != "commit" {
        return Ok(false);
    }

    let commit = match &event.commit {
        Some(c) => c,
        None => return Ok(false),
    };

    // Only process creates for posts
    if commit.operation != "create" || commit.collection != "app.bsky.feed.post" {
        return Ok(false);
    }

    let record = match &commit.record {
        Some(r) => r,
        None => return Ok(false),
    };

    // Normalize to BlueskyPost
    let post = normalize_post(&event.did, commit, record)?;

    // Skip very short posts (likely not useful)
    if post.text.len() < 10 {
        return Ok(false);
    }

    // Check negative keywords (spam filter)
    let text_lower = post.text.to_lowercase();
    for kw in NEGATIVE_KEYWORDS {
        if text_lower.contains(kw) {
            return Ok(false);
        }
    }

    // Check for complaint/issue indicators - this is our main filter
    // The analyzer will determine the brand later
    let has_complaint_indicator = COMPLAINT_KEYWORDS.iter().any(|kw| text_lower.contains(kw));
    if !has_complaint_indicator {
        return Ok(false);
    }

    // Store the post - analyzer_bluesky will determine brand
    store_post(pool, &post).await?;
    
    info!(
        "📥 Complaint found: {}",
        truncate_text(&post.text, 80)
    );

    Ok(true)
}

fn normalize_post(did: &str, commit: &JetstreamCommit, record: &JsonValue) -> Result<BlueskyPost> {
    let uri = format!("at://{}/{}/{}", did, commit.collection, commit.rkey);
    let cid = commit.cid.clone().unwrap_or_default();

    let text = record.get("text")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();

    let created_at = record.get("createdAt")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string());

    // Extract links and hashtags from facets
    let mut links = Vec::new();
    let mut hashtags = Vec::new();

    if let Some(facets) = record.get("facets").and_then(|v| v.as_array()) {
        for facet in facets {
            if let Some(features) = facet.get("features").and_then(|v| v.as_array()) {
                for feature in features {
                    let ftype = feature.get("$type").and_then(|v| v.as_str()).unwrap_or("");
                    
                    if ftype == "app.bsky.richtext.facet#link" {
                        if let Some(uri) = feature.get("uri").and_then(|v| v.as_str()) {
                            links.push(uri.to_string());
                        }
                    } else if ftype == "app.bsky.richtext.facet#tag" {
                        if let Some(tag) = feature.get("tag").and_then(|v| v.as_str()) {
                            hashtags.push(tag.to_lowercase());
                        }
                    }
                }
            }
        }
    }

    // Check if this is a reply
    let is_reply = record.get("reply").is_some();

    Ok(BlueskyPost {
        uri,
        cid,
        author_did: did.to_string(),
        author_handle: None, // Would need identity resolution
        text,
        links,
        hashtags,
        created_at,
        is_reply,
        detected_brands: Vec::new(),
        raw: record.clone(),
    })
}



async fn store_post(pool: &Pool, post: &BlueskyPost) -> Result<()> {
    let mut conn = pool.get_conn().await?;
    let raw_json = serde_json::to_string(&post.raw)?;

    conn.exec_drop(
        r#"
        INSERT INTO indexer_bluesky_post 
            (uri, cid, author_did, author_handle, text, created_at, raw, lang)
        VALUES 
            (?, ?, ?, ?, ?, ?, ?, 'en')
        ON DUPLICATE KEY UPDATE
            text = VALUES(text),
            raw = VALUES(raw)
        "#,
        (
            &post.uri,
            &post.cid,
            &post.author_did,
            post.author_handle.as_deref().unwrap_or(""),
            &post.text,
            post.created_at.as_deref(),
            &raw_json,
        )
    ).await?;

    debug!("Stored post {}", post.uri);

    Ok(())
}

fn truncate_text(text: &str, max_len: usize) -> String {
    if text.len() <= max_len {
        text.replace('\n', " ")
    } else {
        format!("{}...", text.chars().take(max_len).collect::<String>().replace('\n', " "))
    }
}
